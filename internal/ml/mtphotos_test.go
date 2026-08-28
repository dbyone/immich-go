package ml

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newMTSidecar stands up an httptest server speaking the mt-photos-ai
// dialect exactly as onnx/server.py does: api-key header auth, multipart
// "file" field, {"result": [...]} envelopes, 200-with-msg on failure.
func newMTSidecar(t *testing.T, key string, hitCounter func(endpoint string)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	auth := func(r *http.Request) bool { return r.Header.Get("api-key") == key }

	mux.HandleFunc("/check", func(w http.ResponseWriter, r *http.Request) {
		if hitCounter != nil {
			hitCounter("check")
		}
		if !auth(r) {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"detail": "Invalid API key"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": "pass", "title": "mt-photos-ai服务"})
	})
	mux.HandleFunc("/clip/img", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if hitCounter != nil {
			hitCounter("clip/img")
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"result": []string{}, "msg": "missing file"})
			return
		}
		defer file.Close()
		buf := make([]byte, 1)
		n, _ := file.Read(buf)
		if n == 0 {
			_ = json.NewEncoder(w).Encode(map[string]any{"result": []string{}, "msg": "empty upload"})
			return
		}
		// Deterministic vector depending on the first byte, formatted the
		// sidecar way: a 16-decimal float string.
		v := float64(buf[0]) / 255.0
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": []string{strings.Replace(string(mustJSON(v)), "\"", "", -1)},
		})
	})
	mux.HandleFunc("/clip/txt", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if hitCounter != nil {
			hitCounter("clip/txt")
		}
		var req struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Text == "" {
			_ = json.NewEncoder(w).Encode(map[string]any{"result": []string{}, "msg": "empty text"})
			return
		}
		if req.Text == "boom" {
			// The sidecar swallows model errors into a 200 + msg envelope.
			_ = json.NewEncoder(w).Encode(map[string]any{"result": []string{}, "msg": "image decode failed"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": []string{"0.5", "-0.25", "0.125"}})
	})
	mux.HandleFunc("/ocr", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if hitCounter != nil {
			hitCounter("ocr")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"texts":  []string{"MT照片"},
				"scores": []string{"0.98"},
				"boxes": []map[string]string{
					{"x": "4.0", "y": "283.0", "width": "120.0", "height": "30.0"},
				},
			},
		})
	})
	return httptest.NewServer(mux)
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func mtTestImage(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(p, []byte{7, 8, 9}, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMTPhotosAuthAndPing(t *testing.T) {
	srv := newMTSidecar(t, "s3cret", nil)
	defer srv.Close()

	good := NewMTPhotos(ProviderConfig{Enabled: true, URLs: []string{srv.URL}, APIKey: "s3cret"}, nil)
	if !good.Ping(srv.URL) {
		t.Fatal("ping with the right key should pass")
	}
	bad := NewMTPhotos(ProviderConfig{Enabled: true, URLs: []string{srv.URL}, APIKey: "wrong"}, nil)
	if bad.Ping(srv.URL) {
		t.Fatal("ping with a wrong key must fail (401)")
	}
}

func TestMTPhotosEncodeText(t *testing.T) {
	srv := newMTSidecar(t, "k", nil)
	defer srv.Close()
	p := NewMTPhotos(ProviderConfig{Enabled: true, URLs: []string{srv.URL}, APIKey: "k"}, nil)

	vec, err := p.EncodeText(context.Background(), "海滩 日落", TextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := []float32{0.5, -0.25, 0.125}
	if len(vec) != len(want) {
		t.Fatalf("vector %v, want %v", vec, want)
	}
	for i := range want {
		if vec[i] != want[i] {
			t.Fatalf("vector %v, want %v", vec, want)
		}
	}
}

func TestMTPhotosErrorEnvelopeIsAnError(t *testing.T) {
	srv := newMTSidecar(t, "k", nil)
	defer srv.Close()
	p := NewMTPhotos(ProviderConfig{Enabled: true, URLs: []string{srv.URL}, APIKey: "k"}, nil)

	if _, err := p.EncodeText(context.Background(), "boom", TextOptions{}); err == nil {
		t.Fatal("the 200+msg envelope must surface as an error")
	}
}

func TestMTPhotosEncodeImage(t *testing.T) {
	srv := newMTSidecar(t, "k", nil)
	defer srv.Close()
	p := NewMTPhotos(ProviderConfig{Enabled: true, URLs: []string{srv.URL}, APIKey: "k"}, nil)

	img := mtTestImage(t)
	vec, err := p.EncodeImage(context.Background(), img, "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 1 {
		t.Fatalf("expected 1-dim deterministic vector, got %v", vec)
	}
	want := float32(7) / 255.0
	if diff := vec[0] - want; diff > 1e-6 || diff < -1e-6 {
		t.Fatalf("vector %v, want ~%v", vec[0], want)
	}
}

func TestMTPhotosOCR(t *testing.T) {
	srv := newMTSidecar(t, "k", nil)
	defer srv.Close()
	p := NewMTPhotos(ProviderConfig{Enabled: true, URLs: []string{srv.URL}, APIKey: "k"}, nil)

	res, err := p.OCR(context.Background(), mtTestImage(t), OCROptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Text) != 1 || res.Text[0] != "MT照片" {
		t.Fatalf("text %v", res.Text)
	}
	if len(res.Box) != 4 || res.Box[0] != 4 || res.Box[1] != 283 || res.Box[2] != 124 || res.Box[3] != 313 {
		t.Fatalf("box %v", res.Box)
	}
	if len(res.BoxScore) != 1 || res.BoxScore[0] != 0.98 {
		t.Fatalf("score %v", res.BoxScore)
	}
}

func TestMTPhotosFacesUnsupported(t *testing.T) {
	p := NewMTPhotos(ProviderConfig{Enabled: true, URLs: []string{"http://127.0.0.1:1"}, APIKey: "k"}, nil)
	if p.SupportsFaces() {
		t.Fatal("mt-photos-ai has no face endpoints")
	}
	if _, err := p.DetectFaces(context.Background(), "x.jpg", FaceDetectionOptions{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
}

func TestMTPhotosFailover(t *testing.T) {
	var hits int
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer dead.Close()
	live := newMTSidecar(t, "k", nil)
	defer live.Close()

	p := NewMTPhotos(ProviderConfig{
		Enabled: true,
		// dead first: the client must fail over to the healthy sidecar.
		URLs:   []string{dead.URL, live.URL},
		APIKey: "k",
	}, nil)
	if _, err := p.EncodeText(context.Background(), "x", TextOptions{}); err != nil {
		t.Fatal(err)
	}
	if hits == 0 {
		t.Fatal("the dead URL should have been tried first")
	}
}

func TestNewProviderFactory(t *testing.T) {
	if got := NewProvider(ProviderConfig{Provider: "mtphotos"}, nil).Name(); got != "mtphotos" {
		t.Fatalf("got %q", got)
	}
	if got := NewProvider(ProviderConfig{Provider: ""}, nil).Name(); got != "immich" {
		t.Fatalf("default provider must be immich, got %q", got)
	}
}
