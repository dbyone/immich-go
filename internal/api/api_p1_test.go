package api

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestPeopleManagement covers create/rename/merge/stats/thumbnail and the
// cluster-generated people path.
func TestPeopleManagement(t *testing.T) {
	h := newTestServer(t)
	token := loginForTest(t, h, "people@t.c")

	// Manual person lifecycle.
	code, body := doJSON(t, h, http.MethodPost, "/api/people", token, map[string]any{
		"name": "Grandma", "color": "#ff0000", "birthDate": "1945-03-01",
	})
	if code != 201 {
		t.Fatalf("person create: %d %v", code, body)
	}
	manual := asMap(t, body)
	if manual["name"] != "Grandma" || manual["birthDate"] != "1945-03-01" {
		t.Fatalf("person shape: %v", manual)
	}
	manualID, _ := manual["id"].(string)

	code, body = doJSON(t, h, http.MethodPut, "/api/people/"+manualID, token, map[string]any{
		"name": "Grandma L.", "isFavorite": true,
	})
	if code != 200 || asMap(t, body)["name"] != "Grandma L." {
		t.Fatalf("person update: %d %v", code, body)
	}
	code, body = doJSON(t, h, http.MethodGet, "/api/people/"+manualID, token, nil)
	if code != 200 || asMap(t, body)["isFavorite"] != true {
		t.Fatalf("person get: %d %v", code, body)
	}

	// Upload three photos with the same fake face, wait for clustering.
	for i := 1; i <= 3; i++ {
		uploadForTest(t, h, token, testJPEG(t, i), "face.jpg")
	}
	deadline := time.Now().Add(20 * time.Second)
	var personID string
	for time.Now().Before(deadline) {
		doJSON(t, h, http.MethodPost, "/api/jobs", token, map[string]any{"name": "face-clustering"})
		code, body = doJSON(t, h, http.MethodGet, "/api/people", token, nil)
		if code == 200 {
			if list, _ := body.([]any); len(list) == 2 { // manual + clustered
				for _, item := range list {
					p, _ := item.(map[string]any)
					if p["faceCount"] == float64(3) {
						personID, _ = p["id"].(string)
					}
				}
				if personID != "" {
					break
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if personID == "" {
		t.Fatalf("clustered person never appeared: %v", body)
	}

	// Rename + statistics.
	code, body = doJSON(t, h, http.MethodPut, "/api/people/"+personID, token, map[string]any{
		"name": "Alice",
	})
	if code != 200 || asMap(t, body)["name"] != "Alice" {
		t.Fatalf("clustered rename: %d %v", code, body)
	}
	_, body = doJSON(t, h, http.MethodGet, "/api/people/"+personID+"/statistics", token, nil)
	if asMap(t, body)["assets"] != float64(3) {
		t.Fatalf("person stats: %v", body)
	}

	// Thumbnail crops the first face out of the source image.
	thumbReq := httptest.NewRequest(http.MethodGet, "/api/people/"+personID+"/thumbnail", nil)
	thumbReq.Header.Set("Authorization", "Bearer "+token)
	thumbRec := httptest.NewRecorder()
	h.ServeHTTP(thumbRec, thumbReq)
	if thumbRec.Code != 200 || !strings.HasPrefix(thumbRec.Header().Get("Content-Type"), "image/") {
		t.Fatalf("person thumbnail: %d %s", thumbRec.Code, thumbRec.Header().Get("Content-Type"))
	}
	if thumbRec.Body.Len() == 0 {
		t.Fatal("person thumbnail empty")
	}

	// Merge the clustered person into the manual one.
	code, body = doJSON(t, h, http.MethodPost, "/api/people/"+manualID+"/merge", token,
		map[string]any{"ids": []string{personID}})
	if code != 200 {
		t.Fatalf("merge: %d %v", code, body)
	}
	_, body = doJSON(t, h, http.MethodGet, "/api/people/"+manualID, token, nil)
	if p := asMap(t, body); p["id"] != manualID {
		t.Fatalf("merge target lost: %v", p)
	}
	code, _ = doJSON(t, h, http.MethodGet, "/api/people/"+personID, token, nil)
	if code != 404 {
		t.Fatalf("merged source should be gone: %d", code)
	}
	_, body = doJSON(t, h, http.MethodGet, "/api/people/"+manualID+"/statistics", token, nil)
	if asMap(t, body)["assets"] != float64(3) {
		t.Fatalf("post-merge stats: %v", body)
	}

	// Bulk delete removes the remaining people.
	code, _ = doJSON(t, h, http.MethodDelete, "/api/people", token, map[string]any{
		"ids": []string{manualID},
	})
	if code != 204 {
		t.Fatalf("bulk delete: %d", code)
	}
	_, body = doJSON(t, h, http.MethodGet, "/api/people", token, nil)
	if list, _ := body.([]any); len(list) != 0 {
		t.Fatalf("people should be empty: %v", body)
	}
}

func TestDuplicatesResolveAndDownloadMapView(t *testing.T) {
	h := newTestServer(t)
	token := loginForTest(t, h, "dup@t.c")

	// Three visually identical uploads (same fake embedding) + one unique.
	id1 := uploadForTest(t, h, token, testJPEG(t, 1), "a.jpg")
	id2 := uploadForTest(t, h, token, testJPEG(t, 2), "b.jpg")
	id3 := uploadForTest(t, h, token, testJPEG(t, 3), "c.jpg")

	// Wait for duplicate groups (debounced job or manual trigger).
	deadline := time.Now().Add(20 * time.Second)
	var group map[string]any
	for time.Now().Before(deadline) {
		doJSON(t, h, http.MethodPost, "/api/jobs", token, map[string]any{"name": "detect-duplicates"})
		code, body := doJSON(t, h, http.MethodGet, "/api/duplicates", token, nil)
		if code == 200 {
			if groups, _ := body.([]any); len(groups) == 1 {
				group, _ = groups[0].(map[string]any)
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if group == nil {
		t.Fatal("duplicate group never appeared")
	}
	duplicateID, _ := group["duplicateId"].(string)
	if duplicateID == "" {
		duplicateID, _ = group["id"].(string)
	}

	// Resolve: keep id1, trash the rest.
	code, body := doJSON(t, h, http.MethodPost, "/api/duplicates/resolve", token, map[string]any{
		"groups": []map[string]any{{
			"duplicateId":   duplicateID,
			"keepAssetIds":  []string{id1},
			"trashAssetIds": []string{id2, id3},
		}},
	})
	if code != 200 {
		t.Fatalf("resolve: %d %v", code, body)
	}
	if groups, _ := body.([]any); len(groups) != 2 {
		t.Fatalf("resolve results: %v", body)
	}
	_, body = doJSON(t, h, http.MethodGet, "/api/duplicates", token, nil)
	if groups, _ := body.([]any); len(groups) != 0 {
		t.Fatalf("group should be dissolved: %v", body)
	}
	_, body = doJSON(t, h, http.MethodPost, "/api/search/metadata", token,
		map[string]any{"withDeleted": true, "size": 10})
	meta := asMap(t, body)
	assets, _ := meta["assets"].([]any)
	if len(assets) != 3 {
		t.Fatalf("all three assets should still exist: %d", len(assets))
	}

	// Download info + archive: use the kept asset (need a fresh one since
	// the others are trashed — archives include trashed here for brevity).
	archiveReq := httptest.NewRequest(http.MethodPost, "/api/download/archive", strings.NewReader(
		`{"assetIds":["`+id1+`"],"archiveName":"photos"}`))
	archiveReq.Header.Set("Content-Type", "application/json")
	archiveReq.Header.Set("Authorization", "Bearer "+token)
	archiveRec := httptest.NewRecorder()
	h.ServeHTTP(archiveRec, archiveReq)
	if archiveRec.Code != 200 {
		t.Fatalf("download archive: %d %s", archiveRec.Code, archiveRec.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(archiveRec.Body.Bytes()), int64(archiveRec.Body.Len()))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	if len(zr.File) != 1 {
		t.Fatalf("archive entries: %d", len(zr.File))
	}

	code, body = doJSON(t, h, http.MethodPost, "/api/download/info", token, map[string]any{
		"assetIds": []string{id1},
	})
	if code != 201 {
		t.Fatalf("download info: %d %v", code, body)
	}
	info := asMap(t, body)
	if info["totalSize"] == nil || info["archives"] == nil {
		t.Fatalf("download info shape: %v", info)
	}

	// Map markers: the uploads carry GPS EXIF (Shanghai).
	_, body = doJSON(t, h, http.MethodGet, "/api/map/markers", token, nil)
	markers, _ := body.([]any)
	nonTrashed := 0
	for _, m := range markers {
		if mm, _ := m.(map[string]any); mm != nil {
			if lat, _ := mm["lat"].(float64); lat > 31.2 && lat < 31.3 {
				nonTrashed++
			}
		}
	}
	if nonTrashed < 1 {
		t.Fatalf("expected Shanghai markers, got %v", markers)
	}
	_, body = doJSON(t, h, http.MethodGet, "/api/map/reverse-geocode?lat=31.2&lon=121.4", token, nil)
	if _, ok := body.([]any); !ok {
		t.Fatalf("reverse-geocode should return a list: %v", body)
	}

	// Folder view: uploads live under upload/<userId>/xx/yy/.
	_, body = doJSON(t, h, http.MethodGet, "/api/view/folder/unique-paths", token, nil)
	paths, _ := body.([]any)
	if len(paths) == 0 {
		t.Fatal("unique paths should not be empty")
	}
	somePath, _ := paths[0].(string)
	code, body = doJSON(t, h, http.MethodGet, "/api/view/folder?originalPath="+somePath, token, nil)
	if code != 200 {
		t.Fatalf("folder view: %d", code)
	}
	if folderAssets, _ := body.([]any); len(folderAssets) == 0 {
		t.Fatalf("folder view should list assets under %s: %v", somePath, body)
	}
}
