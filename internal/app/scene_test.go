package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"immich-go/internal/config"
	"immich-go/internal/domain"
	"immich-go/internal/exif/exiftest"
)

// sceneML mimics the immich dialect with label-dependent text embeddings:
// the "beach" prompt aligns with every image embedding, every other label
// is orthogonal. With threshold 0.95 exactly one scene tag survives.
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
		vec := "\"[1.0, 0.0, 0.0]\""
		if txt := r.FormValue("text"); txt != "" && txt != "beach" {
			vec = "\"[0.0, 1.0, 0.0]\""
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"clip":%s,"imageHeight":64,"imageWidth":64}`, vec)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestSceneClassificationPipeline walks the whole post-upload pipeline —
// metadata, smart-search embedding, zero-shot classification — and asserts
// the asset lands under hierarchical 场景/<label> tags.
func TestSceneClassificationPipeline(t *testing.T) {
	ml := sceneML(t)
	cfg := config.Load()
	cfg.MediaLocation = filepath.Join(t.TempDir(), "media")
	cfg.DuckDBPath = ":memory:"
	cfg.VectorDim = 3
	cfg.MachineLearning.URLs = []string{ml.URL}
	cfg.MachineLearning.AvailabilityChecks.Enabled = false
	cfg.MachineLearning.FacialRecognition.Enabled = false
	cfg.MachineLearning.SceneClassification.Enabled = true
	cfg.MachineLearning.SceneClassification.Threshold = 0.95
	cfg.MachineLearning.SceneClassification.TopK = 3

	a, err := New(cfg, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ctx, cancel := context.WithCancel(context.Background())
	a.Jobs.Start(ctx)
	defer cancel()

	if err := a.Store.Users().Create(ctx, &domain.User{
		ID: "u1", Email: "s@t.c", Password: "x", Name: "S",
		IsAdmin: true, IsOnboarded: true, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	img := exiftest.BuildJPEG(exiftest.Options{Width: 64, Height: 64})
	dir := filepath.Join(cfg.MediaLocation, "upload", "u1", "aa", "bb")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "beach.jpg")
	if err := os.WriteFile(file, img, 0o644); err != nil {
		t.Fatal(err)
	}
	nowT := time.Now().UTC()
	asset := &domain.Asset{
		ID: "as1", OwnerID: "u1", Type: domain.AssetImage,
		OriginalPath: file, OriginalFileName: "beach.jpg",
		FileCreatedAt: nowT, FileModifiedAt: nowT, LocalDateTime: nowT,
		CreatedAt: nowT, UpdatedAt: nowT, Visibility: domain.VisibilityTimeline,
		Checksum: []byte("s1"), ChecksumB64: "czE=",
	}
	if err := a.Store.Assets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}

	a.QueueAssetPipeline("as1")

	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		tags, _ := a.Store.Tags().ListForAsset(ctx, "as1")
		for _, tg := range tags {
			if tg.Value == "场景/海滩" {
				return // exactly the matching label survived
			}
			t.Fatalf("unexpected scene tag %q (orthogonal labels must be filtered)", tg.Value)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("scene tag 场景/海滩 never attached")
}
