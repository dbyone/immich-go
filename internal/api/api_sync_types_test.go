package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"immich-go/internal/config"
)

func streamTypes(t *testing.T, h http.Handler, token, types string) []map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/sync/stream",
		strings.NewReader(`{"reset":true,"types":[`+types+`]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("stream: %d %s", rec.Code, rec.Body.String())
	}
	var lines []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(rec.Body.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("bad ndjson line %q: %v", l, err)
		}
		lines = append(lines, m)
	}
	return lines
}

func linesOfType(lines []map[string]any, typ string) []map[string]any {
	var out []map[string]any
	for _, l := range lines {
		if l["type"] == typ {
			out = append(out, l)
		}
	}
	return out
}

// TestSyncAssetExifStream: uploaded assets stream their EXIF row as
// AssetExifV1 (and the partner variant under its own type), with the
// upstream wire fields.
func TestSyncAssetExifStream(t *testing.T) {
	h := newTestServerCfg(t, func(cfg *config.Config) {
		cfg.MachineLearning.DuplicateDetection.Enabled = false
	})
	token := loginForTest(t, h, "exifsync@t.c")
	id := uploadForTest(t, h, token, testJPEG(t, 1), "exif.jpg")

	// Wait until the metadata job has filled the EXIF row.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		code, body := doJSON(t, h, http.MethodGet, "/api/assets/"+id+"?withExif=true", token, nil)
		if code == http.StatusOK {
			exif := asMap(t, body)["exifInfo"]
			if exif != nil && asMap(t, exif)["make"] == "TestCam" {
				break
			}
		}
		time.Sleep(150 * time.Millisecond)
	}

	lines := streamTypes(t, h, token, `"AssetExifsV1"`)
	exifLines := linesOfType(lines, "AssetExifV1")
	if len(exifLines) != 1 {
		t.Fatalf("AssetExifV1 lines = %d (%v)", len(exifLines), lines)
	}
	row := asMap(t, exifLines[0]["data"])
	if row["assetId"] != id || row["make"] != "TestCam" {
		t.Fatalf("exif row = %v", row)
	}
	if _, ok := exifLines[0]["ack"].(string); !ok || !strings.HasPrefix(exifLines[0]["ack"].(string), "AssetExifV1:") {
		t.Fatalf("ack = %v", exifLines[0]["ack"])
	}
	if row["city"] != nil && row["city"] == "" {
		t.Fatal("empty city must be null, not \"\"")
	}

	// Partner variant answers under its own entity type.
	lines = streamTypes(t, h, token, `"PartnerAssetExifsV1"`)
	if got := linesOfType(lines, "PartnerAssetExifV1"); len(got) != 1 {
		t.Fatalf("PartnerAssetExifV1 lines = %d", len(got))
	}
}

// TestSyncMemoryStream: memories and their asset membership stream as
// MemoryV1 / MemoryAssetV1, and deletion emits a MemoryDeleteV1
// tombstone — the wire the official mobile app consumes.
func TestSyncMemoryStream(t *testing.T) {
	h := newTestServer(t)
	token := loginForTest(t, h, "memsync@t.c")
	id1 := uploadForTest(t, h, token, testJPEG(t, 1), "m1.jpg")
	id2 := uploadForTest(t, h, token, testJPEG(t, 2), "m2.jpg")

	code, body := doJSON(t, h, http.MethodPost, "/api/memories", token, map[string]any{
		"type": "on_this_day", "data": map[string]any{"year": 2025},
		"memoryAt": "2026-08-29T10:00:00.000Z", "assetIds": []string{id1, id2},
	})
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("create memory: %d %v", code, body)
	}
	memoryID := asMap(t, body)["id"].(string)

	lines := streamTypes(t, h, token, `"MemoriesV1","MemoryToAssetsV1"`)
	mem := linesOfType(lines, "MemoryV1")
	if len(mem) != 1 {
		t.Fatalf("MemoryV1 lines = %d (%v)", len(mem), lines)
	}
	mrow := asMap(t, mem[0]["data"])
	if mrow["id"] != memoryID || mrow["type"] != "on_this_day" {
		t.Fatalf("memory row = %v", mrow)
	}
	assets := linesOfType(lines, "MemoryAssetV1")
	if len(assets) != 2 {
		t.Fatalf("MemoryAssetV1 lines = %d", len(assets))
	}

	// Deletion must surface as a tombstone row.
	code, _ = doJSON(t, h, http.MethodDelete, "/api/memories/"+memoryID, token, nil)
	if code != http.StatusOK && code != http.StatusNoContent {
		t.Fatalf("delete memory: %d", code)
	}
	lines = streamTypes(t, h, token, `"MemoriesV1"`)
	if got := linesOfType(lines, "MemoryDeleteV1"); len(got) != 1 {
		t.Fatalf("MemoryDeleteV1 lines = %d (%v)", len(got), lines)
	}
}
