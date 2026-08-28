package ml

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"sync"
	"time"
)

// mtPhotos is the client for the mt-photos-ai sidecar
// (github.com/MT-Photos/mt-photos-ai, FastAPI + Chinese-CLIP + RapidOCR).
//
// Wire contract (verified against onnx/server.py in that repository):
//
//	POST /check    header-only            -> {"result": "pass", ...}
//	POST /ocr      multipart field "file" -> {"result": {texts, scores, boxes}}
//	POST /clip/img multipart field "file" -> {"result": ["0.33...", ...]}
//	POST /clip/txt JSON {"text": "..."}   -> {"result": ["0.33...", ...]}
//
// Auth: every endpoint takes the API_AUTH_KEY value in the "api-key"
// header (FastAPI Header(...) maps the api_key parameter to that name);
// a mismatch answers 401 {"detail": "Invalid API key"}.
//
// Quirk: the sidecar reports failures as HTTP 200 with
// {"result": [], "msg": "..."} — an empty result plus a message means the
// call failed. There are no face endpoints; DetectFaces returns
// ErrUnsupported.
type mtPhotos struct {
	cfg    ProviderConfig
	http   *http.Client
	logger *slog.Logger

	mu      sync.RWMutex
	healthy map[string]bool
}

// NewMTPhotos builds the mt-photos-ai dialect client.
func NewMTPhotos(cfg ProviderConfig, logger *slog.Logger) Provider {
	if logger == nil {
		logger = slog.Default()
	}
	return &mtPhotos{
		cfg:     cfg,
		http:    &http.Client{Timeout: 5 * time.Minute},
		logger:  logger,
		healthy: map[string]bool{},
	}
}

func (m *mtPhotos) Name() string        { return "mtphotos" }
func (m *mtPhotos) SupportsCLIP() bool  { return true }
func (m *mtPhotos) SupportsFaces() bool { return false }
func (m *mtPhotos) SupportsOCR() bool   { return true }
func (m *mtPhotos) Teardown()           {}

// Ping probes POST /check with the api-key header; only that endpoint
// validates the key without doing model work.
func (m *mtPhotos) Ping(baseURL string) bool {
	u, err := url.JoinPath(baseURL, "check")
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return false
	}
	req.Header.Set("api-key", m.cfg.APIKey)
	resp, err := m.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

// mtEnvelope is the sidecar's universal reply. result is a float-string
// array (CLIP), an object (OCR), or an empty array on failure.
type mtEnvelope struct {
	Result json.RawMessage `json:"result"`
	Msg    string          `json:"msg"`
}

func (m *mtPhotos) isHealthy(u string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.healthy[u]
}

func (m *mtPhotos) setHealthy(u string, healthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.healthy[u] != healthy {
		m.logger.Info("mt-photos-ai server became healthy", "healthy", healthy, "url", u)
	}
	m.healthy[u] = healthy
}

// call walks the configured URLs (healthy first) and decodes the envelope.
// An empty result with a message is surfaced as an error, mirroring the
// sidecar's 200-on-failure behavior.
func (m *mtPhotos) call(ctx context.Context, build func(base string) (*http.Request, error)) (json.RawMessage, error) {
	if !m.cfg.Enabled {
		return nil, errors.New("machine learning is disabled")
	}
	order := append([]string{}, m.cfg.URLs...)
	for i := 0; i < len(order)-1; i++ {
		if !m.isHealthy(order[i]) {
			for j := i + 1; j < len(order); j++ {
				if m.isHealthy(order[j]) {
					order[i], order[j] = order[j], order[i]
					break
				}
			}
		}
	}
	var lastErr error
	for _, base := range order {
		req, err := build(base)
		if err != nil {
			return nil, err
		}
		req.Header.Set("api-key", m.cfg.APIKey)
		resp, err := m.http.Do(req)
		if err != nil {
			lastErr = err
			m.logger.Warn("mt-photos-ai request failed", "url", base, "err", err)
			m.setHealthy(base, false)
			continue
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("status %d: %s", resp.StatusCode, string(data))
			m.logger.Warn("mt-photos-ai request failed", "url", base, "status", resp.StatusCode)
			if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode < 500 {
				// 4xx (other than a bad key) means our request was bad; the
				// sidecar may still be fine for other traffic.
				m.setHealthy(base, true)
			} else {
				m.setHealthy(base, false)
			}
			continue
		}
		m.setHealthy(base, true)
		env := mtEnvelope{}
		if err := json.Unmarshal(data, &env); err != nil {
			return nil, fmt.Errorf("decode mt-photos-ai response: %w", err)
		}
		if env.Msg != "" && isEmptyResult(env.Result) {
			return nil, fmt.Errorf("mt-photos-ai: %s", env.Msg)
		}
		return env.Result, nil
	}
	return nil, fmt.Errorf("mt-photos-ai request failed for all URLs: %w", lastErr)
}

func isEmptyResult(raw json.RawMessage) bool {
	return len(raw) == 0 || string(raw) == "[]" || string(raw) == "null"
}

// mtMultipart builds a "file" upload request against a base URL.
func mtMultipart(endpoint string, img []byte) func(string) (*http.Request, error) {
	return func(baseURL string) (*http.Request, error) {
		u, err := url.JoinPath(baseURL, endpoint)
		if err != nil {
			return nil, err
		}
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("file", path.Base("image.jpg"))
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(img); err != nil {
			return nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		req, err := http.NewRequest(http.MethodPost, u, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		return req, nil
	}
}

// decodeMTVector parses the sidecar's float-string array. Elements arrive
// as 16-decimal strings ("0.3305919170379639"); plain JSON numbers are
// accepted too so hand-rolled test doubles stay simple.
func decodeMTVector(raw json.RawMessage) ([]float32, error) {
	var parts []json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, fmt.Errorf("decode mt-photos-ai vector: %w", err)
	}
	if len(parts) == 0 {
		return nil, errors.New("mt-photos-ai returned an empty embedding")
	}
	out := make([]float32, 0, len(parts))
	for _, p := range parts {
		var f float64
		if err := json.Unmarshal(p, &f); err == nil {
			out = append(out, float32(f))
			continue
		}
		var s string
		if err := json.Unmarshal(p, &s); err != nil {
			return nil, fmt.Errorf("decode mt-photos-ai vector element %s: %w", p, err)
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("decode mt-photos-ai vector element %q: %w", s, err)
		}
		out = append(out, float32(v))
	}
	return out, nil
}

// EncodeImage embeds an image file via POST /clip/img. The modelName
// argument is accepted for interface parity; the sidecar always serves
// its bundled Chinese-CLIP ViT-B-16 model.
func (m *mtPhotos) EncodeImage(ctx context.Context, imagePath, modelName string) ([]float32, error) {
	img, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, err
	}
	raw, err := m.call(ctx, mtMultipart("clip/img", img))
	if err != nil {
		return nil, err
	}
	return decodeMTVector(raw)
}

// EncodeText embeds a query via POST /clip/txt. Chinese queries are the
// dialect's native strength (Chinese-CLIP); the language option of the
// immich dialect does not apply.
func (m *mtPhotos) EncodeText(ctx context.Context, text string, opts TextOptions) ([]float32, error) {
	payload, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return nil, err
	}
	build := func(baseURL string) (*http.Request, error) {
		u, err := url.JoinPath(baseURL, "clip/txt")
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	}
	raw, err := m.call(ctx, build)
	if err != nil {
		return nil, err
	}
	return decodeMTVector(raw)
}

// mtOCR is the /ocr reply shape: every scalar arrives as a string.
type mtOCR struct {
	Texts  []string `json:"texts"`
	Scores []string `json:"scores"`
	Boxes  []struct {
		X      string `json:"x"`
		Y      string `json:"y"`
		Width  string `json:"width"`
		Height string `json:"height"`
	} `json:"boxes"`
}

// OCR runs RapidOCR over the image via POST /ocr and maps the box/score
// shape into the dialect-neutral OCRResult.
func (m *mtPhotos) OCR(ctx context.Context, imagePath string, opts OCROptions) (*OCRResult, error) {
	img, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, err
	}
	raw, err := m.call(ctx, mtMultipart("ocr", img))
	if err != nil {
		return nil, err
	}
	if isEmptyResult(raw) {
		// No text found: a valid empty result.
		return &OCRResult{Text: []string{}, Box: []float64{}, BoxScore: []float64{}, TextScore: []float64{}}, nil
	}
	var mt mtOCR
	if err := json.Unmarshal(raw, &mt); err != nil {
		return nil, fmt.Errorf("decode mt-photos-ai ocr: %w", err)
	}
	out := &OCRResult{
		Text:      append([]string{}, mt.Texts...),
		Box:       []float64{},
		BoxScore:  []float64{},
		TextScore: []float64{},
	}
	for i := range mt.Boxes {
		x, _ := strconv.ParseFloat(mt.Boxes[i].X, 64)
		y, _ := strconv.ParseFloat(mt.Boxes[i].Y, 64)
		w, _ := strconv.ParseFloat(mt.Boxes[i].Width, 64)
		h, _ := strconv.ParseFloat(mt.Boxes[i].Height, 64)
		out.Box = append(out.Box, x, y, x+w, y+h)
		if i < len(mt.Scores) {
			s, _ := strconv.ParseFloat(mt.Scores[i], 64)
			out.BoxScore = append(out.BoxScore, s)
			out.TextScore = append(out.TextScore, s)
		}
	}
	return out, nil
}

// DetectFaces is not offered by the sidecar.
func (m *mtPhotos) DetectFaces(ctx context.Context, imagePath string, opts FaceDetectionOptions) (*FaceDetectionResult, error) {
	return nil, ErrUnsupported
}
