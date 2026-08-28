package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"immich-go/internal/app"
	"immich-go/internal/config"
	"immich-go/internal/crypto"
	"immich-go/internal/domain"
)

func userIDOf(h http.Handler, token string) string {
	req := httptest.NewRequest(http.MethodGet, "/api/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	return fmt.Sprint(m["id"])
}

func hashForTest(s string) (string, error) { return crypto.HashPassword(s) }

func cryptoNewUUID() string { return crypto.NewUUID() }

// TestSearchLongTail covers explore/cities/places/person/random/
// statistics/suggestions/large-assets.
// waitExif polls until the metadata pipeline populated EXIF (EXIF has no
// city upstream — city comes from reverse geocoding — so we key on make).
func waitExif(t *testing.T, h http.Handler, token string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		code, b := doJSON(t, h, http.MethodGet, "/api/search/suggestions?type=camera-make", token, nil)
		if code == 200 {
			if list, _ := b.([]any); len(list) >= 1 {
				return
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatal("exif never populated")
}

func TestSearchLongTail(t *testing.T) {
	h, a := newTestServerApp(t, nil)
	token := loginForTest(t, h, "search@t.c")
	for i := 1; i <= 3; i++ {
		uploadForTest(t, h, token, testJPEG(t, i), "s.jpg")
	}
	waitExif(t, h, token)
	// Seed a city like reverse geocoding would — but only once EVERY
	// asset's metadata job has landed, else a later job re-parses EXIF
	// over the seed (fast/loaded runners make the window real).
	uid := userIDOf(h, token)
	deadline := time.Now().Add(15 * time.Second)
	for {
		assets, _ := a.Store.Assets().ListForOwner(context.Background(), uid)
		ready := 0
		for _, asset := range assets {
			if asset.Exif != nil {
				ready++
			}
		}
		if ready == 3 {
			for _, asset := range assets {
				asset.Exif.City = "Shanghai"
				_ = a.Store.Assets().Update(context.Background(), asset)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("exif not ready for all assets: %d/3", ready)
		}
		time.Sleep(150 * time.Millisecond)
	}

	code, body := doJSON(t, h, http.MethodGet, "/api/search/cities", token, nil)
	if code != 200 {
		t.Fatalf("cities: %d", code)
	}
	if list, _ := body.([]any); len(list) != 1 { // all uploads share one EXIF city
		t.Fatalf("cities: %v", body)
	}

	code, body = doJSON(t, h, http.MethodGet, "/api/search/explore", token, nil)
	if code != 200 {
		t.Fatalf("explore: %d", code)
	}
	groups, _ := body.([]any)
	if len(groups) != 1 {
		t.Fatalf("explore groups: %v", body)
	}
	g := asMap(t, groups[0])
	if g["fieldName"] != "exifInfo.city" {
		t.Fatalf("explore field: %v", g)
	}
	if items, _ := g["items"].([]any); len(items) != 1 {
		t.Fatalf("explore items: %v", g)
	}

	code, body = doJSON(t, h, http.MethodGet, "/api/search/places?name=shang", token, nil)
	if code != 200 {
		t.Fatalf("places: %d", code)
	}
	placesList, _ := body.([]any)
	if len(placesList) != 1 {
		t.Fatalf("places: %v", body)
	}
	p := asMap(t, placesList[0])
	if p["admin2name"] == "" || p["latitude"] == nil {
		t.Fatalf("place shape: %v", p)
	}

	code, body = doJSON(t, h, http.MethodPost, "/api/search/random", token, map[string]any{"size": 2})
	if code != 200 {
		t.Fatalf("random: %d", code)
	}
	if picks, _ := body.([]any); len(picks) != 2 {
		t.Fatalf("random size: %v", body)
	}

	code, body = doJSON(t, h, http.MethodPost, "/api/search/statistics", token, map[string]any{"type": "IMAGE"})
	if code != 200 {
		t.Fatalf("statistics: %d", code)
	}
	if asMap(t, body)["total"] != float64(3) {
		t.Fatalf("statistics: %v", body)
	}

	code, body = doJSON(t, h, http.MethodGet, "/api/search/suggestions?type=camera-make", token, nil)
	if code != 200 || body.([]any)[0] != "TestCam" {
		t.Fatalf("suggestions: %d %v", code, body)
	}

	code, body = doJSON(t, h, http.MethodPost, "/api/search/large-assets?threshold=1", token, nil)
	if code != 200 {
		t.Fatalf("large-assets: %d", code)
	}
	if big, _ := body.([]any); len(big) != 3 {
		t.Fatalf("large-assets: %v", body)
	}
}

// TestSessionsCreateUpdateLock covers the three new session endpoints.
func TestSessionsCreateUpdateLock(t *testing.T) {
	h := newTestServer(t)
	token := loginForTest(t, h, "sess@t.c")

	code, body := doJSON(t, h, http.MethodPost, "/api/sessions", token, map[string]any{
		"deviceOS": "iOS 99", "deviceType": "mobile", "duration": 3600,
	})
	if code != 201 {
		t.Fatalf("session create: %d %v", code, body)
	}
	created := asMap(t, body)
	if created["token"] == "" || created["current"] != false {
		t.Fatalf("session create shape: %v", created)
	}
	if created["expiresAt"] == nil {
		t.Fatal("duration should set expiresAt")
	}
	sessID, _ := created["id"].(string)
	newToken, _ := created["token"].(string)

	// The new token authenticates.
	req := httptest.NewRequest(http.MethodGet, "/api/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+newToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("new session token must work: %d", rec.Code)
	}

	code, body = doJSON(t, h, http.MethodPut, "/api/sessions/"+sessID, token,
		map[string]any{"isPendingSyncReset": true})
	if code != 200 {
		t.Fatalf("session update: %d %v", code, body)
	}
	code, _ = doJSON(t, h, http.MethodPost, "/api/sessions/"+sessID+"/lock", token, nil)
	if code != 204 {
		t.Fatalf("session lock: %d", code)
	}
}

// TestStacks covers the full stack lifecycle.
func TestStacks(t *testing.T) {
	h := newTestServer(t)
	token := loginForTest(t, h, "stack@t.c")
	id1 := uploadForTest(t, h, token, testJPEG(t, 1), "a.jpg")
	id2 := uploadForTest(t, h, token, testJPEG(t, 2), "b.jpg")
	id3 := uploadForTest(t, h, token, testJPEG(t, 3), "c.jpg")

	code, body := doJSON(t, h, http.MethodPost, "/api/stacks", token, map[string]any{
		"assetIds": []string{id1, id2, id3},
	})
	if code != 201 {
		t.Fatalf("stack create: %d %v", code, body)
	}
	st := asMap(t, body)
	if st["primaryAssetId"] != id1 || st["assetCount"] != float64(3) {
		t.Fatalf("stack shape: %v", st)
	}
	stackID, _ := st["id"].(string)

	code, body = doJSON(t, h, http.MethodGet, "/api/stacks", token, nil)
	if code != 200 || len(body.([]any)) != 1 {
		t.Fatalf("stack list: %d %v", code, body)
	}
	code, body = doJSON(t, h, http.MethodGet, "/api/stacks/"+stackID, token, nil)
	if code != 200 || len(asMap(t, body)["assets"].([]any)) != 3 {
		t.Fatalf("stack get: %d %v", code, body)
	}

	// Change primary.
	code, body = doJSON(t, h, http.MethodPut, "/api/stacks/"+stackID, token, map[string]any{
		"primaryAssetId": id2,
	})
	if code != 200 || asMap(t, body)["primaryAssetId"] != id2 {
		t.Fatalf("stack update: %d %v", code, body)
	}

	// Remove one asset -> still 2, stack survives.
	code, _ = doJSON(t, h, http.MethodDelete, "/api/stacks/"+stackID+"/assets/"+id3, token, nil)
	if code != 204 {
		t.Fatalf("stack asset remove: %d", code)
	}
	_, body = doJSON(t, h, http.MethodGet, "/api/stacks/"+stackID, token, nil)
	if asMap(t, body)["assetCount"] != float64(2) {
		t.Fatalf("after remove: %v", body)
	}

	// Removing another dissolves the stack.
	code, _ = doJSON(t, h, http.MethodDelete, "/api/stacks/"+stackID+"/assets/"+id1, token, nil)
	if code != 204 {
		t.Fatalf("second remove: %d", code)
	}
	code, _ = doJSON(t, h, http.MethodGet, "/api/stacks/"+stackID, token, nil)
	if code != 404 {
		t.Fatalf("dissolved stack should 404: %d", code)
	}
}

// TestPartners covers share create/list/update/remove.
func TestPartners(t *testing.T) {
	h, a := newTestServerApp(t, nil)
	token := loginForTest(t, h, "me@t.c")

	seedHash, _ := cryptoHash("password123")
	if err := aStoreCreateUser(a, "friend@t.c", seedHash); err != nil {
		t.Fatal(err)
	}
	friendID := userIDByEmail(a, "friend@t.c")

	code, body := doJSON(t, h, http.MethodPost, "/api/partners", token, map[string]any{
		"sharedWithId": friendID,
	})
	if code != 201 {
		t.Fatalf("partner create: %d %v", code, body)
	}
	p := asMap(t, body)
	if p["email"] != "friend@t.c" || p["inTimeline"] != true {
		t.Fatalf("partner shape: %v", p)
	}

	// shared-by direction lists the friend.
	_, body = doJSON(t, h, http.MethodGet, "/api/partners?direction=shared-by", token, nil)
	if list, _ := body.([]any); len(list) != 1 || asMap(t, list[0])["id"] != friendID {
		t.Fatalf("partners shared-by: %v", body)
	}

	code, body = doJSON(t, h, http.MethodPut, "/api/partners/"+friendID, token, map[string]any{
		"inTimeline": false,
	})
	if code != 200 || asMap(t, body)["inTimeline"] != false {
		t.Fatalf("partner update: %d %v", code, body)
	}

	code, _ = doJSON(t, h, http.MethodDelete, "/api/partners/"+friendID, token, nil)
	if code != 204 {
		t.Fatalf("partner delete: %d", code)
	}
	_, body = doJSON(t, h, http.MethodGet, "/api/partners?direction=shared-by", token, nil)
	if list, _ := body.([]any); len(list) != 0 {
		t.Fatalf("partners after delete: %v", body)
	}
}

// TestSyncIncremental proves the watermark protocol: initial snapshot,
// ack, then only the new asset streams; hard delete emits a tombstone.
func TestSyncIncremental(t *testing.T) {
	// The fake ML returns identical embeddings, which arms the duplicate
	// detection debounce — its late update_id stamps would race the
	// watermark assertions below.
	h := newTestServerCfg(t, func(cfg *config.Config) {
		cfg.MachineLearning.DuplicateDetection.Enabled = false
	})
	token := loginForTest(t, h, "sync@t.c")

	id1 := uploadForTest(t, h, token, testJPEG(t, 1), "one.jpg")

	stream := func(body string) []map[string]any {
		req := httptest.NewRequest(http.MethodPost, "/api/sync/stream", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("stream: %d %s", rec.Code, rec.Body.String())
		}
		var lines []map[string]any
		for _, l := range strings.Split(strings.TrimSpace(rec.Body.String()), "\n") {
			if l == "" {
				continue
			}
			var m map[string]any
			json.Unmarshal([]byte(l), &m)
			lines = append(lines, m)
		}
		return lines
	}

	// 0) Let the async pipeline (metadata/thumbnails/faces) settle so its
	// update_id stamps stop moving before we take the watermark.
	settle := func() string {
		prev := ""
		for i := 0; i < 40; i++ {
			lines := stream(`{"types":["AssetsV1"]}`)
			ack := ""
			for _, l := range lines {
				if l["type"] == "AssetV1" {
					ack = l["ack"].(string)
				}
			}
			if ack != "" && ack == prev {
				return ack
			}
			prev = ack
			time.Sleep(150 * time.Millisecond)
		}
		return prev
	}
	initialAck := settle()

	// 1) Full snapshot: asset one.
	lines := stream(`{"types":["AssetsV1"]}`)
	_ = initialAck
	var upserts []string
	for _, l := range lines {
		if l["type"] == "AssetV1" {
			upserts = append(upserts, fmt.Sprint(l["ack"]))
		}
	}
	if len(upserts) != 1 {
		t.Fatalf("initial snapshot: %v", lines)
	}

	// 2) Ack the watermark; nothing new streams.
	stream2 := fmt.Sprintf(`{"types":["AssetsV1"],"acks":[%q]}`, upserts[0])
	doJSON(t, h, http.MethodPost, "/api/sync/ack", token, map[string]any{"acks": []string{upserts[0]}})
	if lines = stream(stream2); len(lines) != 0 {
		t.Fatalf("acked stream should be empty: %v", lines)
	}

	// 3) Upload a second asset — only it streams.
	id2 := uploadForTest(t, h, token, testJPEG(t, 2), "two.jpg")
	if lines = stream(stream2); len(lines) != 1 || lines[0]["type"] != "AssetV1" {
		t.Fatalf("incremental stream: %v", lines)
	}
	data := lines[0]["data"].(map[string]any)
	if data["id"] != id2 {
		t.Fatalf("incremental returned wrong asset: %v", data["id"])
	}
	ack2 := lines[0]["ack"].(string)
	doJSON(t, h, http.MethodPost, "/api/sync/ack", token, map[string]any{"acks": []string{ack2}})
	// Let the second upload's pipeline settle too, then re-ack its head.
	head := settle()
	if head != "" {
		ack2 = head
		doJSON(t, h, http.MethodPost, "/api/sync/ack", token, map[string]any{"acks": []string{ack2}})
	}

	// 4) Edit asset one (favorite) — it re-streams with a higher watermark.
	code, _ := doJSON(t, h, http.MethodPut, "/api/assets/"+id1, token, map[string]any{"isFavorite": true})
	if code != 200 {
		t.Fatalf("favorite: %d", code)
	}
	stream3 := fmt.Sprintf(`{"types":["AssetsV1"],"acks":[%q]}`, ack2)
	if lines = stream(stream3); len(lines) != 1 || lines[0]["data"].(map[string]any)["id"] != id1 {
		t.Fatalf("edited asset should re-stream: %v", lines)
	}
	ack3 := lines[0]["ack"].(string)

	// 5) Hard delete asset two via trash + empty — a tombstone streams.
	// (Late background stamps may legitimately re-deliver other rows; the
	// contract under test is that deletes produce AssetDeleteV1 events.)
	doJSON(t, h, http.MethodDelete, "/api/assets", token, map[string]any{"ids": []string{id2}})
	doJSON(t, h, http.MethodPost, "/api/trash/empty", token, nil)
	stream4 := fmt.Sprintf(`{"types":["AssetsV1"],"acks":[%q]}`, ack3)
	lines = stream(stream4)
	var tomb map[string]any
	for _, l := range lines {
		if l["type"] == "AssetDeleteV1" {
			tomb = l["data"].(map[string]any)
		}
	}
	if tomb == nil {
		t.Fatalf("delete tombstone missing: %v", lines)
	}
	if tomb["assetId"] != id2 {
		t.Fatalf("tombstone target: %v", tomb)
	}
	_ = time.Now
}

// helpers for seeding users directly
func cryptoHash(s string) (string, error) { return hashForTest(s) }

func aStoreCreateUser(a *app.App, email, hash string) error {
	return a.Store.Users().Create(context.Background(), &domain.User{
		ID: cryptoNewUUID(), Email: email, Password: hash, Name: "Friend",
		AvatarColor: "primary", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
}

func userIDByEmail(a *app.App, email string) string {
	u, err := a.Store.Users().GetByEmail(context.Background(), email)
	if err != nil {
		return ""
	}
	return u.ID
}
