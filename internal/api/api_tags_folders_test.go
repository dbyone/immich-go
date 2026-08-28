package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"immich-go/internal/config"
	"immich-go/internal/domain"
)

// ---- tags ----

func TestTagsLifecycle(t *testing.T) {
	h := newTestServer(t)
	token := loginForTest(t, h, "tags@t.c")

	// Hierarchical upsert creates every path segment; the response carries
	// one entry per requested name (the leaf), like upstream.
	code, body := doJSON(t, h, http.MethodPut, "/api/tags", token,
		map[string]any{"tags": []string{"旅行/2026/元旦"}})
	if code != http.StatusOK {
		t.Fatalf("upsert: %d %v", code, body)
	}
	upserted := body.([]any)
	if len(upserted) != 1 {
		t.Fatalf("want 1 leaf per requested name, got %d", len(upserted))
	}
	if asMap(t, upserted[0])["value"] != "旅行/2026/元旦" {
		t.Fatalf("leaf value = %v", upserted[0])
	}

	// Listing shows all of them with values.
	code, body = doJSON(t, h, http.MethodGet, "/api/tags", token, nil)
	if code != http.StatusOK {
		t.Fatalf("list: %d", code)
	}
	listed := body.([]any)
	if len(listed) != 3 {
		t.Fatalf("list: want 3, got %d", len(listed))
	}

	// Create a standalone tag with a color.
	code, body = doJSON(t, h, http.MethodPost, "/api/tags", token,
		map[string]any{"name": "收藏夹", "color": "#FF5733"})
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, body)
	}
	tag := asMap(t, body)
	if tag["value"] != "收藏夹" {
		t.Fatalf("value = %v", tag["value"])
	}
	tagID := tag["id"].(string)

	// Names containing "/" are rejected.
	code, _ = doJSON(t, h, http.MethodPost, "/api/tags", token, map[string]any{"name": "a/b"})
	if code != http.StatusBadRequest {
		t.Fatalf("slash in name must 400, got %d", code)
	}

	// Rename.
	code, body = doJSON(t, h, http.MethodPut, "/api/tags/"+tagID, token,
		map[string]any{"name": "精选"})
	if code != http.StatusOK {
		t.Fatalf("update: %d", code)
	}
	if asMap(t, body)["value"] != "精选" {
		t.Fatalf("rename did not recompute value: %v", body)
	}

	// Attach to an uploaded asset.
	assetID := uploadForTest(t, h, token, testJPEG(t, 1), "t.jpg")
	code, body = doJSON(t, h, http.MethodPut, "/api/tags/"+tagID+"/assets", token,
		map[string]any{"assetIds": []string{assetID}})
	if code != http.StatusOK || asMap(t, body)["count"] != float64(1) {
		t.Fatalf("tag assets: %d %v", code, body)
	}

	// The asset response carries its tags.
	code, body = doJSON(t, h, http.MethodGet, "/api/assets/"+assetID, token, nil)
	if code != http.StatusOK {
		t.Fatalf("get asset: %d", code)
	}
	tags := asMap(t, body)["tags"].([]any)
	if len(tags) != 1 || asMap(t, tags[0])["value"] != "精选" {
		t.Fatalf("asset tags = %v", tags)
	}

	// Metadata search filters by tag.
	code, body = doJSON(t, h, http.MethodPost, "/api/search/metadata", token,
		map[string]any{"tagIds": []string{tagID}, "size": 10})
	if code != http.StatusOK || len(asMap(t, body)["assets"].([]any)) != 1 {
		t.Fatalf("search by tag: %d %v", code, body)
	}
	code, body = doJSON(t, h, http.MethodPost, "/api/search/metadata", token,
		map[string]any{"tagIds": []string{"00000000-0000-4000-8000-000000000000"}, "size": 10})
	if code != http.StatusOK || len(asMap(t, body)["assets"].([]any)) != 0 {
		t.Fatalf("search by unknown tag: %d %v", code, body)
	}

	// Untag: the delete-with-body variant returns the removed count.
	code, body = doJSON(t, h, http.MethodDelete, "/api/tags/"+tagID+"/assets", token,
		map[string]any{"assetIds": []string{assetID}})
	if code != http.StatusOK || asMap(t, body)["count"] != float64(1) {
		t.Fatalf("untag: %d %v", code, body)
	}

	// Bulk retag after untagging (PUT /tags/assets).
	code, body = doJSON(t, h, http.MethodPut, "/api/tags/assets", token,
		map[string]any{"tagIds": []string{tagID}, "assetIds": []string{assetID}})
	if code != http.StatusOK || asMap(t, body)["count"] != float64(1) {
		t.Fatalf("bulk tag: %d %v", code, body)
	}

	code, body = doJSON(t, h, http.MethodDelete, "/api/tags/"+tagID, token, nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete tag: %d", code)
	}
	code, _ = doJSON(t, h, http.MethodGet, "/api/tags/"+tagID, token, nil)
	if code != http.StatusNotFound {
		t.Fatalf("deleted tag must 404, got %d", code)
	}
}

// ---- folders ----

func TestFolderViewContract(t *testing.T) {
	h := newTestServer(t)
	token := loginForTest(t, h, "folders@t.c")
	a1 := uploadForTest(t, h, token, testJPEG(t, 1), "a.jpg")
	a2 := uploadForTest(t, h, token, testJPEG(t, 2), "b.jpg")

	// unique-paths lists each asset's directory, ascending.
	code, body := doJSON(t, h, http.MethodGet, "/api/view/folder/unique-paths", token, nil)
	if code != http.StatusOK {
		t.Fatalf("unique-paths: %d", code)
	}
	paths := body.([]any)
	if len(paths) != 2 {
		t.Fatalf("want 2 dirs, got %v", paths)
	}
	for i := 1; i < len(paths); i++ {
		if paths[i-1].(string) >= paths[i].(string) {
			t.Fatalf("paths not ascending: %v", paths)
		}
	}

	// The folder query returns the direct child only.
	dir := paths[0].(string)
	code, body = doJSON(t, h, http.MethodGet, "/api/view/folder?path="+pathQueryEscape(dir), token, nil)
	if code != http.StatusOK {
		t.Fatalf("folder view: %d", code)
	}
	got := body.([]any)
	if len(got) != 1 {
		t.Fatalf("want exactly the direct child, got %d", len(got))
	}
	if asMap(t, got[0])["originalPath"] == "" {
		t.Fatal("folder view must return full asset rows")
	}

	// Archiving removes the asset (and its now-empty directory) from the
	// folder views — upstream filters on timeline visibility.
	code, _ = doJSON(t, h, http.MethodPut, "/api/assets/"+a1, token, map[string]any{"visibility": "archive"})
	if code != http.StatusOK {
		t.Fatalf("archive: %d", code)
	}
	code, body = doJSON(t, h, http.MethodGet, "/api/view/folder/unique-paths", token, nil)
	if code != http.StatusOK {
		t.Fatalf("unique-paths after archive: %d", code)
	}
	after := body.([]any)
	if len(after) != 1 {
		t.Fatalf("archived asset's dir must disappear, got %v", after)
	}
	_ = a2
}

func pathQueryEscape(s string) string {
	return strings.ReplaceAll(s, `\`, `/`)
}

// ---- duplicates exact filter ----

func TestDuplicatesExactFilter(t *testing.T) {
	h, a := newTestServerApp(t, nil)
	token := loginForTest(t, h, "dup@t.c")
	users, _ := a.Store.Users().List(contextTODO())
	var owner string
	for _, u := range users {
		if u.Email == "dup@t.c" {
			owner = u.ID
		}
	}
	if owner == "" {
		t.Fatal("owner not found")
	}

	now := time.Now().UTC()
	dupID := "dup-group-1"
	mk := func(id string, checksum string) *domain.Asset {
		d := dupID
		return &domain.Asset{
			ID: id, OwnerID: owner, Type: domain.AssetImage,
			OriginalPath: "x/" + id + ".jpg", OriginalFileName: id + ".jpg",
			FileCreatedAt: now, FileModifiedAt: now, LocalDateTime: now,
			CreatedAt: now, UpdatedAt: now,
			Visibility: domain.VisibilityTimeline,
			Checksum:   []byte(checksum), ChecksumB64: checksum,
			DuplicateID: &d,
		}
	}
	for _, asset := range []*domain.Asset{
		mk("dup-a", "same"), mk("dup-b", "same"), mk("dup-c", "other"),
	} {
		if err := a.Store.Assets().Create(contextTODO(), asset); err != nil {
			t.Fatal(err)
		}
	}

	code, body := doJSON(t, h, http.MethodGet, "/api/duplicates", token, nil)
	if code != http.StatusOK || len(body.([]any)) != 1 {
		t.Fatalf("all duplicates: %d %v", code, body)
	}
	group := asMap(t, body.([]any)[0])
	if len(group["assets"].([]any)) != 3 {
		t.Fatalf("group size = %v", group["assets"])
	}
	if group["duplicateId"] != "dup-group-1" {
		t.Fatalf("duplicateId = %v (contract drift)", group["duplicateId"])
	}
	if keep := group["suggestedKeepAssetIds"].([]any); len(keep) != 2 {
		t.Fatalf("one keeper per checksum class expected, got %v", keep)
	}

	code, body = doJSON(t, h, http.MethodGet, "/api/duplicates?exact=true", token, nil)
	if code != http.StatusOK {
		t.Fatalf("exact duplicates: %d", code)
	}
	groups := body.([]any)
	if len(groups) != 1 {
		t.Fatalf("exact: want 1 group, got %d", len(groups))
	}
	g := asMap(t, groups[0])
	if len(g["assets"].([]any)) != 2 {
		t.Fatalf("exact group must keep only the identical pair, got %v", g["assets"])
	}
	if keep := g["suggestedKeepAssetIds"].([]any); len(keep) != 1 {
		t.Fatalf("exact keeper count = %v", keep)
	}
}

// ---- smart search filename fallback ----

func disableMLForTest(c *config.Config) {
	c.MachineLearning.Enabled = false
}

func TestSmartSearchFilenameFallback(t *testing.T) {
	h := newTestServerCfg(t, disableMLForTest)
	token := loginForTest(t, h, "fname@t.c")
	id := uploadForTest(t, h, token, testJPEG(t, 1), "sunset_beach.jpg")

	// Without any ML service the query still finds file-name matches.
	code, body := doJSON(t, h, http.MethodPost, "/api/search/smart", token,
		map[string]any{"query": "sunset"})
	if code != http.StatusOK {
		t.Fatalf("filename search: %d %v", code, body)
	}
	assets := asMap(t, body)["assets"].([]any)
	if len(assets) != 1 || asMap(t, assets[0])["id"] != id {
		t.Fatalf("filename match missing: %v", body)
	}

	// Path fragments match too.
	code, body = doJSON(t, h, http.MethodPost, "/api/search/smart", token,
		map[string]any{"query": "upload"})
	if code != http.StatusOK || len(asMap(t, body)["assets"].([]any)) != 1 {
		t.Fatalf("path match: %d %v", code, body)
	}

	// No match and no ML keeps the upstream "disabled" signal.
	code, _ = doJSON(t, h, http.MethodPost, "/api/search/smart", token,
		map[string]any{"query": "zzz-nothing"})
	if code != http.StatusBadRequest {
		t.Fatalf("no-ML no-match must 400, got %d", code)
	}
}

// ---- per-asset refresh (immich-go extension) ----

func TestRefreshAsset(t *testing.T) {
	h := newTestServer(t)
	token := loginForTest(t, h, "refresh@t.c")
	id := uploadForTest(t, h, token, testJPEG(t, 1), "r.jpg")

	code, _ := doJSON(t, h, http.MethodPost, "/api/assets/"+id+"/refresh", token, nil)
	if code != http.StatusNoContent {
		t.Fatalf("refresh: want 204, got %d", code)
	}
	code, body := doJSON(t, h, http.MethodGet, "/api/assets/"+id, token, nil)
	if code != http.StatusOK {
		t.Fatalf("asset after refresh: %d", code)
	}
	if asMap(t, body)["id"] != id {
		t.Fatal("wrong asset")
	}
	// The classification extension reports its disabled state clearly.
	code, body = doJSON(t, h, http.MethodGet, "/api/assets/"+id+"/classification", token, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("classification disabled: %d %v", code, body)
	}
}

// ---- scene classification end to end ----

// sceneML is an immich-dialect fake whose text embeddings are label
// dependent: "beach" collides with every image, everything else is
// orthogonal. Threshold 0.95 → only 海滩 survives.
func sceneML(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/ping") {
			w.Write([]byte("pong"))
			return
		}
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/predict") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err := r.ParseMultipartForm(16 << 20); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		// The clip field is a JSON array *string* (orjson wire format).
		vec := "\"[1.0, 0.0, 0.0]\"" // image embedding
		if txt := r.FormValue("text"); txt != "" {
			if txt == "beach" {
				vec = "\"[1.0, 0.0, 0.0]\""
			} else {
				vec = "\"[0.0, 1.0, 0.0]\""
			}
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"clip":%s,"imageHeight":64,"imageWidth":64}`, vec)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSceneClassificationPipeline(t *testing.T) {
	ml := sceneML(t)
	h := newTestServerCfg(t, func(c *config.Config) {
		c.MachineLearning.URLs = []string{ml.URL}
		c.MachineLearning.FacialRecognition.Enabled = false
		c.MachineLearning.SceneClassification.Enabled = true
		c.MachineLearning.SceneClassification.Threshold = 0.95
		c.MachineLearning.SceneClassification.TopK = 3
	})
	token := loginForTest(t, h, "scene@t.c")
	id := uploadForTest(t, h, token, testJPEG(t, 1), "beach.jpg")

	// The async pipeline ends with the scene tag attached to the asset.
	deadline := time.Now().Add(20 * time.Second)
	tagged := false
	var classification []any
	for time.Now().Before(deadline) {
		code, body := doJSON(t, h, http.MethodGet, "/api/assets/"+id, token, nil)
		if code == http.StatusOK {
			for _, raw := range asMap(t, body)["tags"].([]any) {
				if asMap(t, raw)["value"] == "场景/海滩" {
					tagged = true
					break
				}
			}
		}
		if tagged {
			code, body = doJSON(t, h, http.MethodGet, "/api/assets/"+id+"/classification", token, nil)
			if code == http.StatusOK {
				classification = body.([]any)
				break
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	if !tagged {
		t.Fatal("scene tag 场景/海滩 never appeared")
	}
	if len(classification) != 1 {
		t.Fatalf("classification = %v", classification)
	}
	entry := asMap(t, classification[0])
	if entry["label"] != "海滩" {
		t.Fatalf("label = %v", entry["label"])
	}
	if s, ok := entry["score"].(float64); !ok || s < 0.95 {
		t.Fatalf("score = %v", entry["score"])
	}

	// The tag tree contains the auto-generated namespace root.
	code, body := doJSON(t, h, http.MethodGet, "/api/tags", token, nil)
	if code != http.StatusOK {
		t.Fatalf("tags: %d", code)
	}
	values := map[string]bool{}
	for _, raw := range body.([]any) {
		values[asMap(t, raw)["value"].(string)] = true
	}
	if !values["场景"] || !values["场景/海滩"] {
		t.Fatalf("tag tree missing scene entries: %v", values)
	}
}

// contextTODO avoids importing context just for two seeding calls.
func contextTODO() context.Context { return context.Background() }
