package api

import (
	"net/http"
	"testing"
	"time"
)

// TestTagDuplicateCreateConflict pins the 409 (not 500) on repeat names.
func TestTagDuplicateCreateConflict(t *testing.T) {
	h := newTestServer(t)
	token := loginForTest(t, h, "tag409@t.c")
	code, _ := doJSON(t, h, http.MethodPost, "/api/tags", token, map[string]any{"name": "dup"})
	if code != http.StatusCreated {
		t.Fatalf("first create: %d", code)
	}
	code, body := doJSON(t, h, http.MethodPost, "/api/tags", token, map[string]any{"name": "dup"})
	if code != http.StatusConflict {
		t.Fatalf("duplicate create: %d %v (want 409)", code, body)
	}
}

// TestTagRenameCascadesChildren: renaming a branch relabels the subtree.
func TestTagRenameCascadesChildren(t *testing.T) {
	h := newTestServer(t)
	token := loginForTest(t, h, "tagren@t.c")
	code, body := doJSON(t, h, http.MethodPut, "/api/tags", token,
		map[string]any{"tags": []string{"旅行/2026/元旦"}})
	if code != http.StatusOK {
		t.Fatalf("upsert: %d", code)
	}
	// Walk up to the root via the listing.
	code, body = doJSON(t, h, http.MethodGet, "/api/tags", token, nil)
	var rootID string
	for _, raw := range body.([]any) {
		m := asMap(t, raw)
		if m["value"] == "旅行" {
			rootID = m["id"].(string)
		}
	}
	if rootID == "" {
		t.Fatal("root tag missing")
	}
	code, body = doJSON(t, h, http.MethodPut, "/api/tags/"+rootID, token,
		map[string]any{"name": "Trips"})
	if code != http.StatusOK {
		t.Fatalf("rename: %d %v", code, body)
	}
	code, body = doJSON(t, h, http.MethodGet, "/api/tags", token, nil)
	values := map[string]bool{}
	for _, raw := range body.([]any) {
		values[asMap(t, raw)["value"].(string)] = true
	}
	for _, want := range []string{"Trips", "Trips/2026", "Trips/2026/元旦"} {
		if !values[want] {
			t.Fatalf("cascade missing %q, have %v", want, values)
		}
	}
	if values["旅行"] || values["旅行/2026"] {
		t.Fatalf("stale values remain: %v", values)
	}
}

// TestBulkTagInvalidIdsRejected: unknown ids fail the whole request.
func TestBulkTagInvalidIdsRejected(t *testing.T) {
	h := newTestServer(t)
	token := loginForTest(t, h, "tagbulk@t.c")
	assetID := uploadForTest(t, h, token, testJPEG(t, 1), "b.jpg")
	code, body := doJSON(t, h, http.MethodPut, "/api/tags", token, map[string]any{"tags": []string{"x"}})
	tagID := asMap(t, body.([]any)[0])["id"].(string)

	code, _ = doJSON(t, h, http.MethodPut, "/api/tags/assets", token,
		map[string]any{"tagIds": []string{"not-a-uuid"}, "assetIds": []string{assetID}})
	if code != http.StatusBadRequest {
		t.Fatalf("unknown tagId: %d (want 400)", code)
	}
	code, _ = doJSON(t, h, http.MethodPut, "/api/tags/assets", token,
		map[string]any{"tagIds": []string{tagID}, "assetIds": []string{"not-an-asset"}})
	if code != http.StatusBadRequest {
		t.Fatalf("unknown assetId: %d (want 400)", code)
	}
	// Valid pair still works.
	code, body = doJSON(t, h, http.MethodPut, "/api/tags/assets", token,
		map[string]any{"tagIds": []string{tagID}, "assetIds": []string{assetID}})
	if code != http.StatusOK || asMap(t, body)["count"] != float64(1) {
		t.Fatalf("valid bulk: %d %v", code, body)
	}
}

// TestTrashRestoreAssets: per-id untrash reports the restored count.
func TestTrashRestoreAssets(t *testing.T) {
	h := newTestServer(t)
	token := loginForTest(t, h, "trashra@t.c")
	id1 := uploadForTest(t, h, token, testJPEG(t, 1), "t1.jpg")
	id2 := uploadForTest(t, h, token, testJPEG(t, 2), "t2.jpg")
	code, _ := doJSON(t, h, http.MethodDelete, "/api/assets", token,
		map[string]any{"ids": []string{id1, id2}})
	if code != http.StatusNoContent {
		t.Fatalf("soft delete: %d", code)
	}
	code, body := doJSON(t, h, http.MethodPost, "/api/trash/restore/assets", token,
		map[string]any{"ids": []string{id1}})
	if code != http.StatusOK || asMap(t, body)["count"] != float64(1) {
		t.Fatalf("restore one: %d %v", code, body)
	}
	code, body = doJSON(t, h, http.MethodGet, "/api/assets/"+id1, token, nil)
	if code != http.StatusOK || asMap(t, body)["isTrashed"] != false {
		t.Fatalf("asset 1 must be restored: %d", code)
	}
	code, body = doJSON(t, h, http.MethodGet, "/api/assets/"+id2, token, nil)
	if code != http.StatusOK || asMap(t, body)["isTrashed"] != true {
		t.Fatalf("asset 2 must stay trashed: %d", code)
	}
}

// TestSearchSuggestionsContract: the upstream type enum (camera-make…)
// with facet filters.
func TestSearchSuggestionsContract(t *testing.T) {
	h := newTestServer(t)
	token := loginForTest(t, h, "sugg@t.c")
	uploadForTest(t, h, token, testJPEG(t, 1), "s1.jpg")

	// EXIF extraction is async; poll until the make is suggestable.
	deadline := time.Now().Add(20 * time.Second)
	var got []any
	for time.Now().Before(deadline) {
		code, body := doJSON(t, h, http.MethodGet, "/api/search/suggestions?type=camera-make", token, nil)
		if code == http.StatusOK {
			got = body.([]any)
			if len(got) > 0 {
				break
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	if len(got) == 0 || got[0] != "TestCam" {
		t.Fatalf("camera-make suggestions = %v", got)
	}
	code, body := 0, any(nil)
	code, _ = doJSON(t, h, http.MethodGet, "/api/search/suggestions?type=make", token, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("legacy 'make' enum must 400, got %d", code)
	}
	code, body = doJSON(t, h, http.MethodGet, "/api/search/suggestions?type=camera-model&make=TestCam", token, nil)
	if code != http.StatusOK || len(body.([]any)) == 0 {
		t.Fatalf("filtered model suggestions: %d %v", code, body)
	}
	code, body = doJSON(t, h, http.MethodGet, "/api/search/suggestions?type=camera-model&make=Other", token, nil)
	if code != http.StatusOK || len(body.([]any)) != 0 {
		t.Fatalf("filter must exclude: %d %v", code, body)
	}
}

// TestSmartSearchSkipsArchived: archived assets stay out of the filename
// fallback (upstream searches timeline-visible assets only).
func TestSmartSearchSkipsArchived(t *testing.T) {
	h := newTestServerCfg(t, disableMLForTest)
	token := loginForTest(t, h, "arch@t.c")
	id := uploadForTest(t, h, token, testJPEG(t, 1), "archived_file.jpg")
	code, body := doJSON(t, h, http.MethodPost, "/api/search/smart", token,
		map[string]any{"query": "archived_file"})
	if code != http.StatusOK || len(asMap(t, body)["assets"].([]any)) != 1 {
		t.Fatalf("pre-archive: %d %v", code, body)
	}
	code, _ = doJSON(t, h, http.MethodPut, "/api/assets/"+id, token, map[string]any{"visibility": "archive"})
	if code != http.StatusOK {
		t.Fatalf("archive: %d", code)
	}
	code, body = doJSON(t, h, http.MethodPost, "/api/search/smart", token,
		map[string]any{"query": "archived_file"})
	if code != http.StatusOK || len(asMap(t, body)["assets"].([]any)) != 0 {
		t.Fatalf("archived asset must be filtered: %d %v", code, body)
	}
}
