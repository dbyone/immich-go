package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func loginForTest(t *testing.T, h http.Handler, email string) string {
	t.Helper()
	_, _ = doJSON(t, h, http.MethodPost, "/api/auth/admin-sign-up", "", map[string]any{
		"name": "Admin", "email": email, "password": "password123",
	})
	_, body := doJSON(t, h, http.MethodPost, "/api/auth/login", "", map[string]any{
		"email": email, "password": "password123",
	})
	token, _ := asMap(t, body)["accessToken"].(string)
	if token == "" {
		t.Fatal("no token")
	}
	return token
}

func uploadForTest(t *testing.T, h http.Handler, token string, data []byte, name string) string {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("assetData", name)
	fw.Write(data)
	mw.WriteField("fileCreatedAt", "2026-01-15T10:00:00.000Z")
	mw.WriteField("fileModifiedAt", "2026-01-15T10:00:00.000Z")
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/assets", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	out := map[string]any{}
	json.Unmarshal(rec.Body.Bytes(), &out)
	id := fmt.Sprint(out["id"])
	if id == "" {
		t.Fatalf("upload failed: %d %s", rec.Code, rec.Body.String())
	}
	return id
}

// TestP0WebOnboarding covers the endpoints the official web client hits
// before reaching the main UI.
func TestP0WebOnboarding(t *testing.T) {
	h := newTestServer(t)

	// Public config is reachable without auth.
	code, body := doJSON(t, h, http.MethodGet, "/api/public/config", "", nil)
	if code != 200 {
		t.Fatalf("public config: %d", code)
	}
	pc := asMap(t, body)
	if _, ok := pc["oauth"]; !ok {
		t.Fatalf("public config shape: %v", pc)
	}

	token := loginForTest(t, h, "web@t.c")

	// Onboarding starts unset, then persists.
	code, body = doJSON(t, h, http.MethodGet, "/api/system-metadata/admin-onboarding", token, nil)
	if code != 200 || asMap(t, body)["isOnboarded"] != false {
		t.Fatalf("onboarding initial: %d %v", code, body)
	}
	code, _ = doJSON(t, h, http.MethodPost, "/api/system-metadata/admin-onboarding", token,
		map[string]any{"isOnboarded": true})
	if code != 204 {
		t.Fatalf("onboarding update: %d", code)
	}
	_, body = doJSON(t, h, http.MethodGet, "/api/system-metadata/admin-onboarding", token, nil)
	if asMap(t, body)["isOnboarded"] != true {
		t.Fatalf("onboarding persisted: %v", body)
	}

	// Preferences return defaults, then persist updates.
	code, body = doJSON(t, h, http.MethodGet, "/api/users/me/preferences", token, nil)
	if code != 200 {
		t.Fatalf("preferences GET: %d", code)
	}
	prefs := asMap(t, body)
	if prefs["download"] == nil || prefs["memories"] == nil {
		t.Fatalf("preferences defaults incomplete: %v", prefs)
	}
	code, body = doJSON(t, h, http.MethodPut, "/api/users/me/preferences", token, map[string]any{
		"download": map[string]any{"archiveSize": 1048576, "includeEmbeddedVideos": true},
		"memories": map[string]any{"enabled": false},
	})
	if code != 200 {
		t.Fatalf("preferences PUT: %d %v", code, body)
	}
	prefs = asMap(t, body)
	dl, _ := prefs["download"].(map[string]any)
	if dl["archiveSize"] != float64(1048576) {
		t.Fatalf("preferences not persisted: %v", dl)
	}
	// Unmodified keys keep their defaults after a partial update.
	mem, _ := prefs["memories"].(map[string]any)
	if mem["duration"] == nil {
		t.Fatalf("default lost after partial update: %v", mem)
	}

	// User config + misc server endpoints.
	code, body = doJSON(t, h, http.MethodGet, "/api/config", token, nil)
	if code != 200 {
		t.Fatalf("config GET: %d", code)
	}
	cfg := asMap(t, body)
	if cfg["machineLearning"] == nil || cfg["image"] == nil {
		t.Fatalf("config shape: %v", cfg)
	}
	code, body = doJSON(t, h, http.MethodGet, "/api/server/apk-links", token, nil)
	if code != 200 {
		t.Fatalf("apk-links: %d", code)
	}
	if apk := asMap(t, body); apk["arm64v8a"] == nil {
		t.Fatalf("apk-links shape: %v", apk)
	}
	code, body = doJSON(t, h, http.MethodGet, "/api/server/version-check", token, nil)
	if code != 200 || asMap(t, body)["releaseVersion"] == nil {
		t.Fatalf("version-check: %d %v", code, body)
	}
}

func TestMemoriesCRUD(t *testing.T) {
	h := newTestServer(t)
	token := loginForTest(t, h, "m@t.c")
	assetID := uploadForTest(t, h, token, testJPEG(t, 9), "mem.jpg")

	code, body := doJSON(t, h, http.MethodPost, "/api/memories", token, map[string]any{
		"type": "on_this_day", "data": map[string]any{"year": 2020},
		"memoryAt": "2020-05-05T00:00:00.000Z", "assetIds": []string{assetID},
	})
	if code != 201 {
		t.Fatalf("memory create: %d %v", code, body)
	}
	mem := asMap(t, body)
	if mem["type"] != "on_this_day" {
		t.Fatalf("memory shape: %v", mem)
	}
	assets, _ := mem["assets"].([]any)
	if len(assets) != 1 {
		t.Fatalf("memory assets: %v", mem)
	}
	memID, _ := mem["id"].(string)

	_, body = doJSON(t, h, http.MethodGet, "/api/memories", token, nil)
	if list, _ := body.([]any); len(list) != 1 {
		t.Fatalf("memories list: %v", body)
	}
	_, body = doJSON(t, h, http.MethodGet, "/api/memories/statistics", token, nil)
	if asMap(t, body)["total"] != float64(1) {
		t.Fatalf("memories stats: %v", body)
	}

	code, body = doJSON(t, h, http.MethodPut, "/api/memories/"+memID, token, map[string]any{
		"isSaved": true, "seenAt": "2026-08-25T00:00:00.000Z",
	})
	if code != 200 || asMap(t, body)["isSaved"] != true {
		t.Fatalf("memory update: %d %v", code, body)
	}

	code, body = doJSON(t, h, http.MethodDelete, "/api/memories/"+memID+"/assets", token,
		map[string]any{"ids": []string{assetID}})
	if code != 200 {
		t.Fatalf("memory asset remove: %d", code)
	}
	if results, _ := body.([]any); len(results) != 1 {
		t.Fatalf("memory asset results: %v", body)
	}

	code, _ = doJSON(t, h, http.MethodDelete, "/api/memories/"+memID, token, nil)
	if code != 204 {
		t.Fatalf("memory delete: %d", code)
	}
	_, body = doJSON(t, h, http.MethodGet, "/api/memories", token, nil)
	if list, _ := body.([]any); len(list) != 0 {
		t.Fatalf("memories should be empty: %v", body)
	}
}

func TestSyncAckAndStream(t *testing.T) {
	h := newTestServer(t)
	token := loginForTest(t, h, "s@t.c")

	// Initial stream: full snapshot for the requested types.
	req := httptest.NewRequest(http.MethodPost, "/api/sync/stream", strings.NewReader(
		`{"types":["AuthUserV1","AssetV2","AlbumsV1","AlbumToAssetsV1"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("sync stream: %d %s", rec.Code, rec.Body.String())
	}
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) != 1 { // only AuthUserV1 (no assets/albums yet)
		t.Fatalf("expected 1 sync line, got %d: %s", len(lines), rec.Body.String())
	}
	var first struct {
		Ack  string         `json:"ack"`
		Type string         `json:"type"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("sync line: %v", err)
	}
	if first.Type != "AuthUserV1" || !strings.HasPrefix(first.Ack, "AuthUserV1:") {
		t.Fatalf("sync line shape: %+v", first)
	}

	// Client acknowledges; the next stream skips that type.
	code, _ := doJSON(t, h, http.MethodPost, "/api/sync/ack", token, map[string]any{
		"acks": []string{first.Ack},
	})
	if code != 204 {
		t.Fatalf("ack post: %d", code)
	}
	_, body := doJSON(t, h, http.MethodGet, "/api/sync/ack", token, nil)
	acks, _ := body.([]any)
	if len(acks) != 1 {
		t.Fatalf("ack get: %v", body)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/sync/stream", strings.NewReader(
		`{"types":["AuthUserV1"]}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if strings.TrimSpace(rec2.Body.String()) != "" {
		t.Fatalf("acked type should not resend: %s", rec2.Body.String())
	}

	// Deleting the ack re-enables the snapshot.
	code, _ = doJSON(t, h, http.MethodDelete, "/api/sync/ack", token, map[string]any{
		"types": []string{"AuthUserV1"},
	})
	if code != 204 {
		t.Fatalf("ack delete: %d", code)
	}
	_, body = doJSON(t, h, http.MethodGet, "/api/sync/ack", token, nil)
	if acks, _ = body.([]any); len(acks) != 0 {
		t.Fatalf("acks should be cleared: %v", body)
	}
}
