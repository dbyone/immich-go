package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"immich-go/internal/app"
	"immich-go/internal/config"
	"immich-go/internal/exif/exiftest"
	"immich-go/internal/videometa/videotest"
)

// fakeML mimics the immich-machine-learning service: /ping and /predict
// returning fixed embeddings for both image and text inputs, plus one face
// per image so the clustering pipeline has material to work with.
func fakeML(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/ping"):
			io.WriteString(w, "pong")
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/predict"):
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"clip":"[0.5,0.5,0.7071]",`+
				`"facial-recognition":[{"boundingBox":{"x1":1,"y1":1,"x2":30,"y2":30},"embedding":"[1.0,0.0,0.0]","score":0.9}],`+
				`"imageHeight":64,"imageWidth":64}`)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// exifTaken is the capture time stamped into every test upload.
var exifTaken = time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

func testJPEG(t *testing.T, seed int) []byte {
	t.Helper()
	lat, lon := 31.2304, 121.4737
	return exiftest.BuildJPEG(exiftest.Options{
		Width: 64, Height: 64,
		Make: "TestCam", Model: "X100", LensModel: "23mm f/2",
		Description:      fmt.Sprintf("test gradient %d", seed),
		Orientation:      1,
		Rating:           3,
		DateTimeOriginal: &exifTaken,
		Latitude:         &lat,
		Longitude:        &lon,
	})
}

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	h, _ := newTestServerApp(t, nil)
	return h
}

// newTestServerCfg builds a test server, optionally tweaking the config
// before the app wires up (e.g. shrinking the upload limit).
func newTestServerCfg(t *testing.T, mutate func(*config.Config)) http.Handler {
	t.Helper()
	h, _ := newTestServerApp(t, mutate)
	return h
}

// newTestServerApp additionally exposes the wired app for tests that
// need to seed state directly (e.g. extra users).
func newTestServerApp(t *testing.T, mutate func(*config.Config)) (http.Handler, *app.App) {
	t.Helper()
	mlSrv := fakeML(t)
	cfg := config.Load()
	cfg.MediaLocation = filepath.Join(t.TempDir(), "media")
	cfg.MachineLearning.URLs = []string{mlSrv.URL}
	cfg.MachineLearning.AvailabilityChecks.Enabled = false
	// The fake ML service returns 3-dimensional vectors; the DuckDB vector
	// store must be opened with the matching dimension.
	cfg.VectorDim = 3
	cfg.DuckDBPath = ":memory:"
	if mutate != nil {
		mutate(cfg)
	}

	// nil store → entity metadata persists to the same in-memory DuckDB
	// database (the production default, minus the file).
	a, err := app.New(cfg, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.Jobs.Start(ctx)
	t.Cleanup(a.Close)
	t.Cleanup(cancel)
	return New(a).Router(), a
}

// doJSON performs a JSON request and returns the status plus decoded body
// (object or array).
func doJSON(t *testing.T, h http.Handler, method, path, token string, body any) (int, any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	data, _ := io.ReadAll(rec.Body)
	var out any
	if len(data) > 0 {
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatalf("non-JSON response %s: %s", rec.Result().Status, data)
		}
	}
	return rec.Code, out
}

func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected JSON object, got %#v", v)
	}
	return m
}

func TestEndToEndFlow(t *testing.T) {
	h := newTestServer(t)

	// Public endpoints.
	code, body := doJSON(t, h, http.MethodGet, "/api/server/ping", "", nil)
	if code != 200 || asMap(t, body)["res"] != "pong" {
		t.Fatalf("ping: %d %v", code, body)
	}
	code, _ = doJSON(t, h, http.MethodGet, "/api/server/version", "", nil)
	if code != 200 {
		t.Fatalf("version: %d", code)
	}

	// Bootstrap admin + login.
	code, body = doJSON(t, h, http.MethodPost, "/api/auth/admin-sign-up", "", map[string]any{
		"name": "Admin", "email": "admin@example.com", "password": "password123",
	})
	if code != 201 {
		t.Fatalf("admin-sign-up: %d %v", code, body)
	}
	code, body = doJSON(t, h, http.MethodPost, "/api/auth/login", "", map[string]any{
		"email": "admin@example.com", "password": "password123",
	})
	if code != 201 {
		t.Fatalf("login: %d %v", code, body)
	}
	token, _ := asMap(t, body)["accessToken"].(string)
	if token == "" {
		t.Fatal("login returned no accessToken")
	}

	// Wrong password is rejected.
	code, _ = doJSON(t, h, http.MethodPost, "/api/auth/login", "", map[string]any{
		"email": "admin@example.com", "password": "wrong",
	})
	if code != 401 {
		t.Fatalf("wrong password should 401, got %d", code)
	}

	// Token validation and cookie auth both work.
	code, body = doJSON(t, h, http.MethodPost, "/api/auth/validateToken", token, nil)
	if code != 200 || asMap(t, body)["authStatus"] != true {
		t.Fatalf("validateToken: %d %v", code, body)
	}
	cookieReq := httptest.NewRequest(http.MethodGet, "/api/users/me", nil)
	cookieReq.AddCookie(&http.Cookie{Name: "immich_access_token", Value: token})
	cookieRec := httptest.NewRecorder()
	h.ServeHTTP(cookieRec, cookieReq)
	if cookieRec.Code != 200 {
		t.Fatalf("cookie auth failed: %d", cookieRec.Code)
	}

	// Upload three distinct images; every pipeline stores the same fake
	// CLIP embedding and the same fake face for each of them.
	upload := func(jpg []byte, name string) (int, string) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("assetData", name)
		fw.Write(jpg)
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
		return rec.Code, fmt.Sprint(out["status"])
	}
	for i := 1; i <= 3; i++ {
		code, status := upload(testJPEG(t, i), fmt.Sprintf("photo-%d.jpg", i))
		if code != 201 || status != "created" {
			t.Fatalf("upload %d: %d %s", i, code, status)
		}
	}

	// Duplicate upload is detected via SHA-1 checksum.
	code, status := upload(testJPEG(t, 1), "photo-1.jpg")
	if code != 201 || status != "duplicate" {
		t.Fatalf("duplicate upload: %d %s", code, status)
	}

	// The metadata job eventually fills dimensions; smart search needs the
	// CLIP embeddings persisted in the DuckDB vector store.
	deadline := time.Now().Add(15 * time.Second)
	var assetIDs []string
	for time.Now().Before(deadline) {
		_, page := doJSON(t, h, http.MethodPost, "/api/search/metadata", token, map[string]any{"size": 10})
		list, _ := asMap(t, page)["assets"].([]any)
		ready := len(list) == 3
		for _, item := range list {
			entry, _ := item.(map[string]any)
			if entry["width"] == nil {
				ready = false
			}
		}
		if ready {
			for _, item := range list {
				entry, _ := item.(map[string]any)
				id, _ := entry["id"].(string)
				assetIDs = append(assetIDs, id)
			}
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(assetIDs) != 3 {
		t.Fatalf("metadata extraction did not complete for 3 assets, got %d", len(assetIDs))
	}
	assetID := assetIDs[0]

	// EXIF extracted by the metadata job surfaces in the asset detail.
	_, detail := doJSON(t, h, http.MethodGet, "/api/assets/"+assetID, token, nil)
	exifInfo, _ := asMap(t, detail)["exifInfo"].(map[string]any)
	if exifInfo == nil {
		t.Fatalf("asset detail missing exifInfo: %v", detail)
	}
	if exifInfo["make"] != "TestCam" || exifInfo["model"] != "X100" || exifInfo["lensModel"] != "23mm f/2" {
		t.Fatalf("camera tags not extracted: %v", exifInfo)
	}
	if exifInfo["dateTimeOriginal"] != "2026-01-15T10:00:00.000Z" {
		t.Fatalf("dateTimeOriginal not extracted: %v", exifInfo["dateTimeOriginal"])
	}
	if lat, _ := exifInfo["latitude"].(float64); lat < 31.229 || lat > 31.231 {
		t.Fatalf("latitude not extracted: %v", exifInfo["latitude"])
	}
	if rating, _ := exifInfo["rating"].(float64); rating != 3 {
		t.Fatalf("rating not extracted: %v", exifInfo["rating"])
	}

	// Thumbnail endpoint serves the generated JPEG.
	thumbReq := httptest.NewRequest(http.MethodGet, "/api/assets/"+assetID+"/thumbnail", nil)
	thumbReq.Header.Set("Authorization", "Bearer "+token)
	thumbRec := httptest.NewRecorder()
	h.ServeHTTP(thumbRec, thumbReq)
	if thumbRec.Code != 200 {
		t.Fatalf("thumbnail: %d", thumbRec.Code)
	}

	// Smart search ranks via the machine-learning service and the DuckDB
	// vector store (identical fake embeddings -> all three match).
	var smartAssets []any
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		code, body = doJSON(t, h, http.MethodPost, "/api/search/smart", token, map[string]any{"query": "gradient"})
		if code == 200 {
			smartAssets, _ = asMap(t, body)["assets"].([]any)
			if len(smartAssets) == 3 {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(smartAssets) != 3 {
		t.Fatalf("smart search should return 3 assets, got %d (%v)", len(smartAssets), body)
	}

	// Face clustering (DBSCAN over DuckDB face_search) creates one person
	// spanning all three faces. Re-triggered while polling because the job
	// only sees faces that detection has already persisted.
	var people []any
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		doJSON(t, h, http.MethodPost, "/api/jobs", token, map[string]any{"name": "face-clustering"})
		code, body = doJSON(t, h, http.MethodGet, "/api/people", token, nil)
		if code == 200 {
			people, _ = body.([]any)
			if len(people) == 1 {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if len(people) != 1 {
		t.Fatalf("clustering should yield 1 person, got %v", people)
	}
	person, _ := people[0].(map[string]any)
	if person["faceCount"] != float64(3) {
		t.Fatalf("person faceCount should be 3: %v", person)
	}

	// Duplicate detection groups the visually identical assets (same
	// re-trigger rationale as clustering).
	var dupGroups []any
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		doJSON(t, h, http.MethodPost, "/api/jobs", token, map[string]any{"name": "detect-duplicates"})
		code, body = doJSON(t, h, http.MethodGet, "/api/duplicates", token, nil)
		if code == 200 {
			dupGroups, _ = body.([]any)
			if len(dupGroups) == 1 {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if len(dupGroups) != 1 {
		t.Fatalf("expected 1 duplicate group, got %v", dupGroups)
	}
	dup, _ := dupGroups[0].(map[string]any)
	if len(dup["assets"].([]any)) != 3 {
		t.Fatalf("duplicate group should hold 3 assets: %v", dup)
	}
	if dup["duplicateId"] == "" || len(dup["suggestedKeepAssetIds"].([]any)) == 0 {
		t.Fatalf("DuplicateResponseDto contract fields missing: %v", dup)
	}

	// Timeline buckets (monthly, columnar bucket payload).
	code, body = doJSON(t, h, http.MethodGet, "/api/timeline/buckets", token, nil)
	if code != 200 {
		t.Fatalf("buckets: %d", code)
	}
	buckets, _ := body.([]any)
	if len(buckets) != 1 {
		t.Fatalf("expected 1 bucket, got %v", body)
	}
	bucket, _ := buckets[0].(map[string]any)
	bucketReq := httptest.NewRequest(http.MethodGet, "/api/timeline/bucket?timeBucket="+fmt.Sprint(bucket["timeBucket"]), nil)
	bucketReq.Header.Set("Authorization", "Bearer "+token)
	bucketRec := httptest.NewRecorder()
	h.ServeHTTP(bucketRec, bucketReq)
	if bucketRec.Code != 200 {
		t.Fatalf("bucket content: %d", bucketRec.Code)
	}
	columnar := map[string]any{}
	json.Unmarshal(bucketRec.Body.Bytes(), &columnar)
	ids, _ := columnar["id"].([]any)
	if len(ids) != 3 {
		t.Fatalf("columnar bucket should hold 3 assets: %v", columnar)
	}

	// Albums.
	code, body = doJSON(t, h, http.MethodPost, "/api/albums", token, map[string]any{
		"albumName": "Trip", "assetIds": []string{assetID},
	})
	if code != 201 {
		t.Fatalf("create album: %d %v", code, body)
	}
	albumID, _ := asMap(t, body)["id"].(string)
	code, body = doJSON(t, h, http.MethodGet, "/api/albums/"+albumID, token, nil)
	if code != 200 || asMap(t, body)["assetCount"] != float64(1) {
		t.Fatalf("album detail: %d %v", code, body)
	}

	// API keys: create, then authenticate with x-api-key.
	code, body = doJSON(t, h, http.MethodPost, "/api/api-keys", token, map[string]any{
		"name": "ci", "permissions": []string{"all"},
	})
	if code != 201 {
		t.Fatalf("create api key: %d %v", code, body)
	}
	secret, _ := asMap(t, body)["secret"].(string)
	keyReq := httptest.NewRequest(http.MethodGet, "/api/server/about", nil)
	keyReq.Header.Set("x-api-key", secret)
	keyRec := httptest.NewRecorder()
	h.ServeHTTP(keyRec, keyReq)
	if keyRec.Code != 200 {
		t.Fatalf("api key auth: %d %s", keyRec.Code, keyRec.Body.String())
	}

	// Jobs overview exposes the 19 Immich queues.
	code, body = doJSON(t, h, http.MethodGet, "/api/jobs", token, nil)
	if code != 200 {
		t.Fatalf("jobs: %d", code)
	}
	jobsBody := asMap(t, body)
	if len(jobsBody) != 19 {
		t.Fatalf("expected 19 queues, got %d", len(jobsBody))
	}
	if q, ok := jobsBody["metadataExtraction"].(map[string]any); ok {
		counts, _ := q["jobCounts"].(map[string]any)
		if completed, _ := counts["completed"].(float64); completed < 1 {
			t.Fatalf("metadataExtraction should have completed jobs: %v", q)
		}
	} else {
		t.Fatal("metadataExtraction queue missing")
	}
}

func TestVideoUploadMetadataAndPlayback(t *testing.T) {
	h := newTestServer(t)

	_, _ = doJSON(t, h, http.MethodPost, "/api/auth/admin-sign-up", "", map[string]any{
		"name": "Admin", "email": "video@test.com", "password": "password123",
	})
	_, body := doJSON(t, h, http.MethodPost, "/api/auth/login", "", map[string]any{
		"email": "video@test.com", "password": "password123",
	})
	token, _ := asMap(t, body)["accessToken"].(string)

	// Synthetic MP4: 8s, 1280x720, 30fps, h264+aac.
	mp4 := videotest.BuildMP4(videotest.Options{
		Width: 1280, Height: 720, DurationMs: 8000, FPS: 30,
	})
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("assetData", "clip.mp4")
	fw.Write(mp4)
	mw.WriteField("fileCreatedAt", "2026-07-01T09:00:00.000Z")
	mw.WriteField("fileModifiedAt", "2026-07-01T09:00:00.000Z")
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/assets", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("video upload: %d %s", rec.Code, rec.Body.String())
	}
	uploadResp := map[string]any{}
	json.Unmarshal(rec.Body.Bytes(), &uploadResp)
	assetID, _ := uploadResp["id"].(string)

	// The metadata job extracts container info without ffmpeg.
	deadline := time.Now().Add(15 * time.Second)
	var detail map[string]any
	for time.Now().Before(deadline) {
		code, b := doJSON(t, h, http.MethodGet, "/api/assets/"+assetID, token, nil)
		if code == 200 {
			detail = asMap(t, b)
			if detail["width"] != nil {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if detail == nil || detail["width"] == nil {
		t.Fatalf("video metadata never populated: %v", detail)
	}
	if detail["type"] != "VIDEO" {
		t.Fatalf("asset type: %v", detail["type"])
	}
	if w := detail["width"].(float64); w != 1280 {
		t.Fatalf("video width: %v", detail["width"])
	}
	if d := detail["duration"].(float64); d != 8000 {
		t.Fatalf("duration should come from the container: %v", detail["duration"])
	}
	exifInfo, _ := detail["exifInfo"].(map[string]any)
	if exifInfo == nil {
		t.Fatalf("missing exifInfo: %v", detail)
	}
	fps, _ := exifInfo["fps"].(float64)
	if fps < 29.5 || fps > 31 {
		t.Fatalf("fps not extracted: %v", exifInfo["fps"])
	}

	// Playback streams the original bytes.
	playReq := httptest.NewRequest(http.MethodGet, "/api/assets/"+assetID+"/video/playback", nil)
	playReq.Header.Set("Authorization", "Bearer "+token)
	playRec := httptest.NewRecorder()
	h.ServeHTTP(playRec, playReq)
	if playRec.Code != 200 {
		t.Fatalf("playback: %d", playRec.Code)
	}
	if !bytes.Equal(playRec.Body.Bytes(), mp4) {
		t.Fatalf("playback bytes differ (%d vs %d)", playRec.Body.Len(), len(mp4))
	}

	// Statistics count the video.
	_, body = doJSON(t, h, http.MethodGet, "/api/assets/statistics", token, nil)
	stats := asMap(t, body)
	if stats["videos"] != float64(1) || stats["total"] != float64(1) {
		t.Fatalf("video statistics: %v", stats)
	}

	// Thumbnail: 200 with a poster when ffmpeg exists, otherwise a clean
	// 404 — never the video bytes as an image.
	thumbReq := httptest.NewRequest(http.MethodGet, "/api/assets/"+assetID+"/thumbnail", nil)
	thumbReq.Header.Set("Authorization", "Bearer "+token)
	thumbRec := httptest.NewRecorder()
	h.ServeHTTP(thumbRec, thumbReq)
	if thumbRec.Code == 200 {
		if ct := thumbRec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
			t.Fatalf("poster content type: %s", ct)
		}
	} else if thumbRec.Code != http.StatusNotFound {
		t.Fatalf("thumbnail should be 200 (ffmpeg) or 404, got %d", thumbRec.Code)
	}
}

func TestUnauthorizedAndPermissions(t *testing.T) {
	h := newTestServer(t)

	code, _ := doJSON(t, h, http.MethodGet, "/api/users/me", "bad-token", nil)
	if code != 401 {
		t.Fatalf("bad token should 401, got %d", code)
	}
	code, _ = doJSON(t, h, http.MethodGet, "/api/timeline/buckets", "", nil)
	if code != 401 {
		t.Fatalf("anonymous should 401, got %d", code)
	}

	// Create a user + limited API key; scoped permission enforcement.
	_, _ = doJSON(t, h, http.MethodPost, "/api/auth/admin-sign-up", "", map[string]any{
		"name": "Admin", "email": "a@b.c", "password": "password123",
	})
	_, body := doJSON(t, h, http.MethodPost, "/api/auth/login", "", map[string]any{
		"email": "a@b.c", "password": "password123",
	})
	token, _ := asMap(t, body)["accessToken"].(string)

	_, body = doJSON(t, h, http.MethodPost, "/api/api-keys", token, map[string]any{
		"name": "limited", "permissions": []string{"asset.read"},
	})
	secret, _ := asMap(t, body)["secret"].(string)

	scopedReq := httptest.NewRequest(http.MethodGet, "/api/users/me", nil)
	scopedReq.Header.Set("x-api-key", secret)
	scopedRec := httptest.NewRecorder()
	h.ServeHTTP(scopedRec, scopedReq)
	if scopedRec.Code != 200 {
		t.Fatalf("scoped key should read users/me, got %d", scopedRec.Code)
	}

	albumReq := httptest.NewRequest(http.MethodPost, "/api/albums", strings.NewReader(`{"albumName":"X"}`))
	albumReq.Header.Set("Content-Type", "application/json")
	albumReq.Header.Set("x-api-key", secret)
	albumRec := httptest.NewRecorder()
	h.ServeHTTP(albumRec, albumReq)
	if albumRec.Code != 403 {
		t.Fatalf("asset.read key must not create albums, got %d: %s", albumRec.Code, albumRec.Body.String())
	}
}
