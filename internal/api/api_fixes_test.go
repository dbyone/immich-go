package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"immich-go/internal/config"
	"immich-go/internal/crypto"
	"immich-go/internal/domain"
	"immich-go/internal/vectordb"
)

// TestAlbumBulkAddRejectsForeignAssets: a shared album member must not
// be able to attach another user's assets via PUT /albums/assets.
func TestAlbumBulkAddRejectsForeignAssets(t *testing.T) {
	h, a := newTestServerApp(t, nil)

	// Two users: owner (via API) + partner (seeded directly).
	ownerToken := loginForTest(t, h, "owner@t.c")
	partnerHash, err := crypto.HashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	partnerID := crypto.NewUUID()
	if err := a.Store.Users().Create(context.Background(), &domain.User{
		ID: partnerID, Email: "partner@t.c", Password: partnerHash, Name: "Partner",
		AvatarColor: "primary", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	_, body := doJSON(t, h, http.MethodPost, "/api/auth/login", "", map[string]any{
		"email": "partner@t.c", "password": "password123",
	})
	partnerToken, _ := asMap(t, body)["accessToken"].(string)
	if partnerToken == "" {
		t.Fatal("partner login failed")
	}

	foreignAsset := uploadForTest(t, h, ownerToken, testJPEG(t, 42), "secret.jpg")

	// Owner creates an album shared with the partner.
	_, body = doJSON(t, h, http.MethodPost, "/api/albums", ownerToken, map[string]any{
		"albumName":  "Shared",
		"albumUsers": []map[string]any{{"userId": partnerID, "role": "editor"}},
	})
	albumID, _ := asMap(t, body)["id"].(string)
	if albumID == "" {
		t.Fatalf("album create failed: %v", body)
	}

	// Partner (album member) tries to inject the owner's asset via the
	// bulk endpoint.
	code, _ := doJSON(t, h, http.MethodPut, "/api/albums/assets", partnerToken, map[string]any{
		"albumIds": []string{albumID}, "assetIds": []string{foreignAsset},
	})
	if code != 200 {
		t.Fatalf("bulk add call: %d", code)
	}
	_, body = doJSON(t, h, http.MethodGet, "/api/albums/"+albumID, ownerToken, nil)
	if asMap(t, body)["assetCount"] != float64(0) {
		t.Fatalf("foreign asset leaked into album: %v", body)
	}
}

// TestSessionsWithAPIKey: /sessions must not panic for API-key callers.
func TestSessionsWithAPIKey(t *testing.T) {
	h := newTestServer(t)
	token := loginForTest(t, h, "key@t.c")

	_, body := doJSON(t, h, http.MethodPost, "/api/api-keys", token, map[string]any{
		"name": "k", "permissions": []string{"all"},
	})
	secret, _ := asMap(t, body)["secret"].(string)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("x-api-key", secret)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sessions with API key: %d %s", rec.Code, rec.Body.String())
	}
	sessions := []map[string]any{}
	json.Unmarshal(rec.Body.Bytes(), &sessions)
	for _, s := range sessions {
		if s["current"] != false {
			t.Fatalf("API-key session list must mark all as non-current: %v", s)
		}
	}
}

// TestEmptyTrashCleansVectors: hard-deleted assets must leave no
// embeddings behind (smart search stops returning them).
func TestEmptyTrashCleansVectors(t *testing.T) {
	h := newTestServer(t)
	token := loginForTest(t, h, "trash@t.c")

	for i := 1; i <= 3; i++ {
		uploadForTest(t, h, token, testJPEG(t, i), "v.jpg")
	}
	// Wait until embeddings are searchable.
	deadline := time.Now().Add(20 * time.Second)
	haveEmbeddings := false
	for time.Now().Before(deadline) {
		code, body := doJSON(t, h, http.MethodPost, "/api/search/smart", token,
			map[string]any{"query": "x"})
		if code == 200 {
			if n := len(asMap(t, body)["assets"].([]any)); n == 3 {
				haveEmbeddings = true
				break
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	if !haveEmbeddings {
		t.Fatal("embeddings never became searchable")
	}

	// Soft-delete every asset, then empty the trash.
	_, page := doJSON(t, h, http.MethodPost, "/api/search/metadata", token, map[string]any{"size": 10})
	var ids []string
	for _, item := range asMap(t, page)["assets"].([]any) {
		ids = append(ids, asMap(t, item)["id"].(string))
	}
	code, _ := doJSON(t, h, http.MethodDelete, "/api/assets", token, map[string]any{"ids": ids})
	if code != 204 {
		t.Fatalf("soft delete: %d", code)
	}
	code, _ = doJSON(t, h, http.MethodPost, "/api/trash/empty", token, nil)
	if code != 204 {
		t.Fatalf("empty trash: %d", code)
	}

	// Smart search must return nothing now.
	code, body := doJSON(t, h, http.MethodPost, "/api/search/smart", token,
		map[string]any{"query": "x"})
	if code != 200 {
		t.Fatalf("smart search after trash: %d", code)
	}
	if n := len(asMap(t, body)["assets"].([]any)); n != 0 {
		t.Fatalf("orphaned embeddings still searchable: %d results", n)
	}
}

// TestTimelineVisibilityDefault: archived assets stay off the default
// timeline and appear only under an explicit visibility filter.
func TestTimelineVisibilityDefault(t *testing.T) {
	h := newTestServer(t)
	token := loginForTest(t, h, "vis@t.c")

	id1 := uploadForTest(t, h, token, testJPEG(t, 1), "one.jpg")
	uploadForTest(t, h, token, testJPEG(t, 2), "two.jpg")

	// Archive asset one.
	code, _ := doJSON(t, h, http.MethodPut, "/api/assets/"+id1, token,
		map[string]any{"visibility": "archive"})
	if code != 200 {
		t.Fatalf("archive asset: %d", code)
	}

	countBucket := func(query string) int {
		_, body := doJSON(t, h, http.MethodGet, "/api/timeline/bucket?timeBucket=2026-01-01"+query, token, nil)
		columnar := asMap(t, body)
		ids, _ := columnar["id"].([]any)
		return len(ids)
	}
	if n := countBucket(""); n != 1 {
		t.Fatalf("default timeline should show 1 asset, got %d", n)
	}
	if n := countBucket("&visibility=archive"); n != 1 {
		t.Fatalf("archive bucket should show 1 asset, got %d", n)
	}
	if n := countBucket("&withDeleted=true"); n != 1 {
		t.Fatalf("withDeleted has no trashed assets: got %d", n)
	}
}

// TestUploadLimitExceeded: oversized uploads fail with 413.
func TestUploadLimitExceeded(t *testing.T) {
	h := newTestServerCfg(t, func(cfg *config.Config) {
		cfg.UploadLimitMB = 1 // 1MiB
	})
	token := loginForTest(t, h, "limit@t.c")

	// Small image within 1MiB passes.
	if id := uploadForTest(t, h, token, testJPEG(t, 1), "small.jpg"); id == "" {
		t.Fatal("small upload failed")
	}

	// Build an oversized body (~2MiB).
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("assetData", "big.jpg")
	fw.Write(bytes.Repeat([]byte{0xFF}, 2<<20))
	mw.WriteField("fileCreatedAt", "2026-01-15T10:00:00.000Z")
	mw.WriteField("fileModifiedAt", "2026-01-15T10:00:00.000Z")
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/assets", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized upload: want 413, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestReassignRejectsForeignPerson: faces may only move to the caller's
// own people.
func TestReassignRejectsForeignPerson(t *testing.T) {
	h, a := newTestServerApp(t, nil)
	token := loginForTest(t, h, "reassign@t.c")

	// Seed a foreign person owned by another user.
	if err := a.Store.Users().Create(context.Background(), &domain.User{
		ID: "victim", Email: "victim@t.c", Password: "x", Name: "Victim",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	foreign := &vectordb.Person{ID: "victim-person", OwnerID: "victim", Name: "V"}
	if err := a.Vectors.CreatePerson(context.Background(), foreign); err != nil {
		t.Fatal(err)
	}

	// Caller creates their own person to reassign from.
	_, body := doJSON(t, h, http.MethodPost, "/api/people", token, map[string]any{"name": "Me"})
	mineID, _ := asMap(t, body)["id"].(string)

	code, _ := doJSON(t, h, http.MethodPut, "/api/people/"+mineID+"/reassign", token,
		map[string]any{"data": []map[string]any{{"assetId": "whatever", "personId": "victim-person"}}})
	if code != http.StatusBadRequest {
		t.Fatalf("foreign reassign must 400, got %d", code)
	}
}

// TestAlbumViewerCannotMutate: viewers must not add or remove assets.
func TestAlbumViewerCannotMutate(t *testing.T) {
	h, a := newTestServerApp(t, nil)
	ownerToken := loginForTest(t, h, "owner2@t.c")

	partnerHash, _ := crypto.HashPassword("password123")
	partnerID := crypto.NewUUID()
	if err := a.Store.Users().Create(context.Background(), &domain.User{
		ID: partnerID, Email: "viewer@t.c", Password: partnerHash, Name: "Viewer",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	_, body := doJSON(t, h, http.MethodPost, "/api/auth/login", "", map[string]any{
		"email": "viewer@t.c", "password": "password123",
	})
	viewerToken, _ := asMap(t, body)["accessToken"].(string)

	_, body = doJSON(t, h, http.MethodPost, "/api/albums", ownerToken, map[string]any{
		"albumName":  "ViewOnly",
		"albumUsers": []map[string]any{{"userId": partnerID, "role": "viewer"}},
	})
	albumID, _ := asMap(t, body)["id"].(string)

	code, _ := doJSON(t, h, http.MethodPut, "/api/albums/"+albumID+"/assets", viewerToken,
		map[string]any{"assetIds": []string{"irrelevant"}})
	if code != http.StatusForbidden {
		t.Fatalf("viewer add must 403, got %d", code)
	}
	code, _ = doJSON(t, h, http.MethodDelete, "/api/albums/"+albumID+"/assets", viewerToken,
		map[string]any{"assetIds": []string{"irrelevant"}})
	if code != http.StatusForbidden {
		t.Fatalf("viewer remove must 403, got %d", code)
	}
}

// TestNarrowAPIKeyBlockedByRoutePermission: a read-only key must not
// reach mutating routes even though handler code forgot to check.
func TestNarrowAPIKeyBlockedByRoutePermission(t *testing.T) {
	h := newTestServer(t)
	token := loginForTest(t, h, "narrow@t.c")

	_, body := doJSON(t, h, http.MethodPost, "/api/api-keys", token, map[string]any{
		"name": "ro", "permissions": []string{"album.read", "asset.read"},
	})
	secret, _ := asMap(t, body)["secret"].(string)

	// Reads pass.
	req := httptest.NewRequest(http.MethodGet, "/api/albums", nil)
	req.Header.Set("x-api-key", secret)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("album.read GET /albums: %d", rec.Code)
	}

	// Mutations blocked at the router regardless of handler internals.
	checks := []struct{ method, path string }{
		{http.MethodDelete, "/api/assets"},
		{http.MethodPut, "/api/assets"},
		{http.MethodPost, "/api/trash/empty"},
	}
	for _, c := range checks {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", secret)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s with read-only key: want 403, got %d", c.method, c.path, rec.Code)
		}
	}
}

// TestSessionExpiry: tokens past the TTL stop authenticating.
func TestSessionExpiry(t *testing.T) {
	h := newTestServerCfg(t, func(cfg *config.Config) {
		cfg.SessionTTL = 20 * time.Millisecond
	})
	token := loginForTest(t, h, "expire@t.c")

	code, _ := doJSON(t, h, http.MethodGet, "/api/users/me", token, nil)
	if code != 200 {
		t.Fatalf("fresh token must work: %d", code)
	}
	time.Sleep(60 * time.Millisecond)
	code, _ = doJSON(t, h, http.MethodGet, "/api/users/me", token, nil)
	if code != 401 {
		t.Fatalf("expired token must 401: %d", code)
	}
}

// TestDuplicatesNotResurrected: after resolving a group, rerunning
// detection must not bring it back while a member is trashed.
func TestDuplicatesNotResurrected(t *testing.T) {
	h := newTestServer(t)
	token := loginForTest(t, h, "resurrect@t.c")

	id1 := uploadForTest(t, h, token, testJPEG(t, 1), "r1.jpg")
	id2 := uploadForTest(t, h, token, testJPEG(t, 2), "r2.jpg")

	deadline := time.Now().Add(20 * time.Second)
	var groupID string
	for time.Now().Before(deadline) {
		doJSON(t, h, http.MethodPost, "/api/jobs", token, map[string]any{"name": "detect-duplicates"})
		_, body := doJSON(t, h, http.MethodGet, "/api/duplicates", token, nil)
		if groups, _ := body.([]any); len(groups) == 1 {
			groupID, _ = asMap(t, groups[0])["duplicateId"].(string)
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if groupID == "" {
		t.Fatal("duplicate group never appeared")
	}

	// Resolve: keep id1, trash id2.
	code, _ := doJSON(t, h, http.MethodPost, "/api/duplicates/resolve", token, map[string]any{
		"groups": []map[string]any{{
			"duplicateId": groupID, "keepAssetIds": []string{id1}, "trashAssetIds": []string{id2},
		}},
	})
	if code != 200 {
		t.Fatalf("resolve: %d", code)
	}

	// Re-run detection; the group must stay dissolved.
	for i := 0; i < 3; i++ {
		doJSON(t, h, http.MethodPost, "/api/jobs", token, map[string]any{"name": "detect-duplicates"})
		time.Sleep(300 * time.Millisecond)
	}
	_, body := doJSON(t, h, http.MethodGet, "/api/duplicates", token, nil)
	if groups, _ := body.([]any); len(groups) != 0 {
		t.Fatalf("resolved group resurrected: %v", groups)
	}
}

// TestBulkUploadCheckReportsTrashed: a trashed duplicate must be
// reported as reject+isTrashed instead of forcing a re-upload.
func TestBulkUploadCheckReportsTrashed(t *testing.T) {
	h := newTestServer(t)
	token := loginForTest(t, h, "trashed@t.c")

	jpg := testJPEG(t, 7)
	id := uploadForTest(t, h, token, jpg, "t.jpg")
	code, _ := doJSON(t, h, http.MethodDelete, "/api/assets", token, map[string]any{"ids": []string{id}})
	if code != 204 {
		t.Fatalf("soft delete: %d", code)
	}

	sumB64, _, err := crypto.Sha1B64(bytes.NewReader(jpg))
	if err != nil {
		t.Fatal(err)
	}
	code, body := doJSON(t, h, http.MethodPost, "/api/assets/bulk-upload-check", token, map[string]any{
		"assets": []map[string]any{{"id": "device-1", "checksum": sumB64}},
	})
	if code != 200 {
		t.Fatalf("bulk check: %d", code)
	}
	results, _ := asMap(t, body)["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("bulk check results: %v", body)
	}
	res := asMap(t, results[0])
	if res["action"] != "reject" || res["isTrashed"] != true {
		t.Fatalf("trashed duplicate must reject with isTrashed: %v", res)
	}
}
