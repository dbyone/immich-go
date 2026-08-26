package ml

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type capturedRequest struct {
	Entries string
	Text    string
	Image   []byte
}

// newFakeML starts a server that mimics the immich_ml FastAPI app closely
// enough to validate the wire contract: GET /ping -> "pong" and
// POST /predict with form fields entries/image/text.
func newFakeML(t *testing.T, capture *capturedRequest, respond func(r *http.Request) string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/ping"):
			w.Write([]byte("pong"))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/predict"):
			if err := r.ParseMultipartForm(64 << 20); err != nil {
				t.Errorf("parse multipart: %v", err)
				http.Error(w, "bad multipart", http.StatusBadRequest)
				return
			}
			capture.Entries = r.FormValue("entries")
			capture.Text = r.FormValue("text")
			if f, _, err := r.FormFile("image"); err == nil {
				capture.Image, _ = io.ReadAll(f)
				f.Close()
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, respond(r))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func mustWriteFile(t *testing.T, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPipelineRequestJSON(t *testing.T) {
	p := NewPipeline().
		Add(TaskClip, TypeTextual, "ViT-B-32__openai", map[string]any{"language": "en"})
	got, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"clip":{"textual":{"modelName":"ViT-B-32__openai","options":{"language":"en"}}}}`
	if string(got) != want {
		t.Fatalf("pipeline JSON mismatch:\n got %s\nwant %s", got, want)
	}

	p2 := NewPipeline().
		Add(TaskFacialRecognition, TypeDetection, "buffalo_l", map[string]any{"minScore": 0.7}).
		Add(TaskFacialRecognition, TypeRecognition, "buffalo_l", nil)
	got2, err := json.Marshal(p2)
	if err != nil {
		t.Fatal(err)
	}
	want2 := `{"facial-recognition":{"detection":{"modelName":"buffalo_l","options":{"minScore":0.7}},"recognition":{"modelName":"buffalo_l"}}}`
	if string(got2) != want2 {
		t.Fatalf("pipeline JSON mismatch:\n got %s\nwant %s", got2, want2)
	}
}

func TestEncodeTextWireFormat(t *testing.T) {
	cap := &capturedRequest{}
	srv := newFakeML(t, cap, func(r *http.Request) string {
		return `{"clip":"[0.5,0.5,0.7071]"}`
	})

	c := NewClient(Config{Enabled: true, URLs: []string{srv.URL}}, discardLogger())
	defer c.Teardown()

	vec, err := c.EncodeText(context.Background(), "sunset on the beach", TextOptions{ModelName: "ViT-B-32__openai", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}

	// entries field must be the exact pipeline JSON the TS server sends.
	wantEntries := `{"clip":{"textual":{"modelName":"ViT-B-32__openai","options":{"language":"en"}}}}`
	if cap.Entries != wantEntries {
		t.Fatalf("entries mismatch:\n got %s\nwant %s", cap.Entries, wantEntries)
	}
	if cap.Text != "sunset on the beach" {
		t.Fatalf("text field mismatch: %q", cap.Text)
	}
	if len(cap.Image) != 0 {
		t.Fatal("image must not be sent for textual queries")
	}
	if len(vec) != 3 || vec[0] != 0.5 {
		t.Fatalf("decoded vector mismatch: %v", vec)
	}
}

func TestEncodeImageWireFormat(t *testing.T) {
	cap := &capturedRequest{}
	srv := newFakeML(t, cap, func(r *http.Request) string {
		return `{"clip":"[0.1,0.2]","imageHeight":1080,"imageWidth":1920}`
	})

	c := NewClient(Config{Enabled: true, URLs: []string{srv.URL}}, discardLogger())
	defer c.Teardown()

	imageBytes := []byte("fake-jpeg-bytes")
	imgPath := mustWriteFile(t, "photo.jpg", imageBytes)

	vec, err := c.EncodeImage(context.Background(), imgPath, "ViT-B-32__openai")
	if err != nil {
		t.Fatal(err)
	}

	wantEntries := `{"clip":{"visual":{"modelName":"ViT-B-32__openai"}}}`
	if cap.Entries != wantEntries {
		t.Fatalf("entries mismatch:\n got %s\nwant %s", cap.Entries, wantEntries)
	}
	if string(cap.Image) != string(imageBytes) {
		t.Fatal("image bytes were not forwarded intact")
	}
	if cap.Text != "" {
		t.Fatalf("unexpected text field %q", cap.Text)
	}
	if len(vec) != 2 {
		t.Fatalf("vector mismatch: %v", vec)
	}
}

func TestDetectFacesWireFormat(t *testing.T) {
	cap := &capturedRequest{}
	srv := newFakeML(t, cap, func(r *http.Request) string {
		return `{"facial-recognition":[{"boundingBox":{"x1":1,"y1":2,"x2":30,"y2":31},"embedding":"[0.3]","score":0.93}],"imageHeight":100,"imageWidth":100}`
	})

	c := NewClient(Config{Enabled: true, URLs: []string{srv.URL}}, discardLogger())
	defer c.Teardown()

	imgPath := mustWriteFile(t, "face.jpg", []byte("jpg"))

	res, err := c.DetectFaces(context.Background(), imgPath, FaceDetectionOptions{ModelName: "buffalo_l", MinScore: 0.7})
	if err != nil {
		t.Fatal(err)
	}

	wantEntries := `{"facial-recognition":{"detection":{"modelName":"buffalo_l","options":{"minScore":0.7}},"recognition":{"modelName":"buffalo_l"}}}`
	if cap.Entries != wantEntries {
		t.Fatalf("entries mismatch:\n got %s\nwant %s", cap.Entries, wantEntries)
	}
	if len(res.Faces) != 1 || res.Faces[0].Score != 0.93 || res.Faces[0].BoundingBox.X2 != 30 {
		t.Fatalf("face decode mismatch: %+v", res)
	}
}

func TestFailoverToSecondURL(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	t.Cleanup(dead.Close)

	cap := &capturedRequest{}
	live := newFakeML(t, cap, func(r *http.Request) string { return `{"clip":"[1.0]"}` })

	// Availability checks enabled: a failed predict must mark the URL
	// unhealthy so later calls prefer the live instance.
	c := NewClient(Config{Enabled: true, URLs: []string{dead.URL, live.URL}, AvailabilityChecks: true}, discardLogger())
	defer c.Teardown()

	if _, err := c.EncodeText(context.Background(), "q", TextOptions{ModelName: "m"}); err != nil {
		t.Fatal(err)
	}
	if cap.Entries == "" {
		t.Fatal("live server never received the request")
	}
	if c.isHealthy(dead.URL) {
		t.Fatal("dead URL should be marked unhealthy")
	}
	if !c.isHealthy(live.URL) {
		t.Fatal("live URL should be marked healthy")
	}
}

func TestPing(t *testing.T) {
	srv := newFakeML(t, &capturedRequest{}, func(r *http.Request) string { return "{}" })
	c := NewClient(Config{Enabled: true, URLs: []string{srv.URL}}, discardLogger())
	defer c.Teardown()
	if !c.Ping(srv.URL) {
		t.Fatal("ping should succeed against healthy service")
	}
	if c.Ping("http://127.0.0.1:1") {
		t.Fatal("ping should fail for unreachable URL")
	}
}

func TestCosineSimilarity(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	if s := CosineSimilarity(a, b); s != 0 {
		t.Fatalf("orthogonal vectors: want 0, got %v", s)
	}
	if s := CosineSimilarity(a, a); math.Abs(s-1) > 1e-9 {
		t.Fatalf("identical vectors: want 1, got %v", s)
	}
}

func TestDisabledClient(t *testing.T) {
	c := NewClient(Config{Enabled: false, URLs: []string{"http://localhost:1"}}, discardLogger())
	defer c.Teardown()
	if _, err := c.EncodeText(context.Background(), "q", TextOptions{ModelName: "m"}); err == nil {
		t.Fatal("predict must fail when machine learning is disabled")
	}
}
