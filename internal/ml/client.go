// Package ml is the Go client for the immich-machine-learning service
// (immich_ml FastAPI app). It is wire-compatible with the upstream
// TypeScript MachineLearningRepository: the same /ping health probe, the
// same POST /predict multipart contract (form field "entries" carrying the
// pipeline request JSON plus an "image" file part or a "text" form value),
// the same multi-URL health-aware failover and the same response decoding.
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

// ModelTask values as defined by immich_ml/schemas.py (StrEnum). The clip
// task is literally "clip", not "search".
const (
	TaskFacialRecognition = "facial-recognition"
	TaskClip              = "clip"
	TaskOCR               = "ocr"
)

// ModelType values accepted inside the pipeline entries.
const (
	TypeDetection   = "detection"
	TypeRecognition = "recognition"
	TypeTextual     = "textual"
	TypeVisual      = "visual"
)

// PipelineEntry is one model invocation inside a predict request.
// It marshals as {"modelName": "...", "options": {...}}.
type PipelineEntry struct {
	ModelName string         `json:"modelName"`
	Options   map[string]any `json:"options,omitempty"`
}

// PipelineRequest mirrors the TS PipelineRequest:
// map[task]map[type]PipelineEntry. Insertion order is preserved by building
// the JSON by hand so the wire format matches the reference implementation.
type PipelineRequest struct {
	entries []pipelineSlot
}

type pipelineSlot struct {
	task  string
	typ   string
	entry PipelineEntry
}

func NewPipeline() *PipelineRequest { return &PipelineRequest{} }

func (p *PipelineRequest) Add(task, typ, modelName string, options map[string]any) *PipelineRequest {
	p.entries = append(p.entries, pipelineSlot{task: task, typ: typ, entry: PipelineEntry{ModelName: modelName, Options: options}})
	return p
}

// MarshalJSON emits the nested {task: {type: entry}} object, grouping the
// entries of one task under a single key so no duplicate keys ever appear.
func (p *PipelineRequest) MarshalJSON() ([]byte, error) {
	var order []string
	byTask := map[string]map[string]PipelineEntry{}
	for _, s := range p.entries {
		if _, ok := byTask[s.task]; !ok {
			byTask[s.task] = map[string]PipelineEntry{}
			order = append(order, s.task)
		}
		byTask[s.task][s.typ] = s.entry
	}

	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, task := range order {
		if i > 0 {
			buf.WriteByte(',')
		}
		taskJSON, err := json.Marshal(task)
		if err != nil {
			return nil, err
		}
		buf.Write(taskJSON)
		buf.WriteString(":{")
		types := byTask[task]
		first := true
		for _, s := range p.entries { // keep insertion order of types
			if s.task != task {
				continue
			}
			if _, seen := types[s.typ]; !seen {
				continue
			}
			delete(types, s.typ)
			if !first {
				buf.WriteByte(',')
			}
			first = false
			buf.WriteString(strconv.Quote(s.typ))
			buf.WriteByte(':')
			entryJSON, err := json.Marshal(s.entry)
			if err != nil {
				return nil, err
			}
			buf.Write(entryJSON)
		}
		buf.WriteByte('}')
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

type BoundingBox struct {
	X1 int `json:"x1"`
	Y1 int `json:"y1"`
	X2 int `json:"x2"`
	Y2 int `json:"y2"`
}

// DetectedFace matches DetectedFace in immich_ml/schemas.py.
type DetectedFace struct {
	BoundingBox BoundingBox `json:"boundingBox"`
	Embedding   string      `json:"embedding"`
	Score       float64     `json:"score"`
}

// OCRResult matches the OCR type in the TS repository.
type OCRResult struct {
	Text      []string  `json:"text"`
	Box       []float64 `json:"box"`
	BoxScore  []float64 `json:"boxScore"`
	TextScore []float64 `json:"textScore"`
}

// predictResponse covers every task's reply. Keys not requested stay zero.
type predictResponse struct {
	Clip              *string        `json:"clip"`
	FacialRecognition []DetectedFace `json:"facial-recognition"`
	OCR               *OCRResult     `json:"ocr"`
	ImageHeight       int            `json:"imageHeight"`
	ImageWidth        int            `json:"imageWidth"`
}

// FaceDetectionResult is DetectFaces' normalized return value.
type FaceDetectionResult struct {
	ImageHeight int
	ImageWidth  int
	Faces       []DetectedFace
}

// FaceDetectionOptions mirrors FaceDetectionOptions in the TS repository.
type FaceDetectionOptions struct {
	ModelName string
	MinScore  float64
}

// OCROptions mirrors OcrOptions in the TS repository.
type OCROptions struct {
	ModelName           string
	MinDetectionScore   float64
	MinRecognitionScore float64
	MaxResolution       int
}

// TextOptions mirrors TextEncodingOptions in the TS repository.
type TextOptions struct {
	ModelName string
	Language  string
}

// Config is the subset of the Immich machineLearning config the client uses.
type Config struct {
	Enabled            bool
	URLs               []string
	AvailabilityChecks bool
	CheckTimeout       time.Duration
	CheckInterval      time.Duration
}

type Client struct {
	cfg    Config
	http   *http.Client
	logger *slog.Logger

	mu      sync.RWMutex
	healthy map[string]bool
	stop    chan struct{}
	stopped sync.Once
}

func NewClient(cfg Config, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	c := &Client{
		cfg:     cfg,
		http:    &http.Client{Timeout: 5 * time.Minute},
		logger:  logger,
		healthy: map[string]bool{},
		stop:    make(chan struct{}),
	}
	if cfg.Enabled && cfg.AvailabilityChecks {
		go c.watch()
	}
	return c
}

// Teardown stops the background health loop.
func (c *Client) Teardown() {
	c.stopped.Do(func() { close(c.stop) })
}

// watch probes every URL on an interval, exactly like the repository's
// setup()/tick() loop.
func (c *Client) watch() {
	interval := c.cfg.CheckInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	c.tick()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
			c.tick()
		}
	}
}

func (c *Client) tick() {
	for _, u := range c.cfg.URLs {
		go func(u string) {
			c.setHealthy(u, c.Ping(u))
		}(u)
	}
}

// Ping probes GET {url}/ping; the service answers "pong".
func (c *Client) Ping(baseURL string) bool {
	u, err := url.JoinPath(baseURL, "ping")
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.checkTimeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

func (c *Client) checkTimeout() time.Duration {
	if c.cfg.CheckTimeout > 0 {
		return c.cfg.CheckTimeout
	}
	return 2 * time.Second
}

func (c *Client) isHealthy(u string) bool {
	if !c.cfg.AvailabilityChecks {
		return true
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy[u]
}

func (c *Client) setHealthy(u string, healthy bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.healthy[u] != healthy {
		c.logger.Info("machine learning server became healthy", "healthy", healthy, "url", u)
	}
	c.healthy[u] = healthy
}

// predict posts the pipeline request as multipart form data, walking the
// configured URLs healthy-first — the same failover order as the upstream
// repository.
func (c *Client) predict(ctx context.Context, image []byte, text string, req *PipelineRequest) (*predictResponse, error) {
	if !c.cfg.Enabled {
		return nil, errors.New("machine learning is disabled")
	}
	entriesJSON, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	order := append([]string{}, c.cfg.URLs...)
	for i := 0; i < len(order)-1; i++ { // stable healthy-first partition
		if !c.isHealthy(order[i]) {
			for j := i + 1; j < len(order); j++ {
				if c.isHealthy(order[j]) {
					order[i], order[j] = order[j], order[i]
					break
				}
			}
		}
	}

	var lastErr error
	for _, base := range order {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		if err := writer.WriteField("entries", string(entriesJSON)); err != nil {
			return nil, err
		}
		if image != nil {
			part, err := writer.CreateFormFile("image", path.Base("image.jpg"))
			if err != nil {
				return nil, err
			}
			if _, err := part.Write(image); err != nil {
				return nil, err
			}
		} else if text != "" {
			if err := writer.WriteField("text", text); err != nil {
				return nil, err
			}
		} else {
			return nil, errors.New("invalid input")
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}

		u, err := url.JoinPath(base, "predict")
		if err != nil {
			return nil, err
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u, body)
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", writer.FormDataContentType())

		resp, err := c.http.Do(httpReq)
		if err != nil {
			lastErr = err
			c.logger.Warn("machine learning request failed", "url", base, "err", err)
			c.setHealthy(base, false)
			continue
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			c.setHealthy(base, true)
			out := &predictResponse{}
			if err := json.Unmarshal(data, out); err != nil {
				return nil, fmt.Errorf("decode machine learning response: %w", err)
			}
			return out, nil
		}
		lastErr = fmt.Errorf("status %d: %s", resp.StatusCode, resp.Status)
		c.logger.Warn("machine learning request failed", "url", base, "status", resp.StatusCode)
		if resp.StatusCode >= 500 {
			// 4xx means we sent a bad request (e.g. unknown model); the
			// server itself may still be healthy for other traffic.
			c.setHealthy(base, false)
		}
	}
	return nil, fmt.Errorf("machine learning request failed for all URLs: %w", lastErr)
}

// DetectFaces runs the two-stage facial recognition pipeline on an image
// file, mirroring MachineLearningRepository.detectFaces.
func (c *Client) DetectFaces(ctx context.Context, imagePath string, opts FaceDetectionOptions) (*FaceDetectionResult, error) {
	req := NewPipeline().
		Add(TaskFacialRecognition, TypeDetection, opts.ModelName, map[string]any{"minScore": opts.MinScore}).
		Add(TaskFacialRecognition, TypeRecognition, opts.ModelName, nil)
	img, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, err
	}
	resp, err := c.predict(ctx, img, "", req)
	if err != nil {
		return nil, err
	}
	return &FaceDetectionResult{
		ImageHeight: resp.ImageHeight,
		ImageWidth:  resp.ImageWidth,
		Faces:       resp.FacialRecognition,
	}, nil
}

// EncodeImage embeds an image file with CLIP and decodes the JSON number
// array string returned by the service into a float vector.
func (c *Client) EncodeImage(ctx context.Context, imagePath, modelName string) ([]float32, error) {
	req := NewPipeline().Add(TaskClip, TypeVisual, modelName, nil)
	img, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, err
	}
	resp, err := c.predict(ctx, img, "", req)
	if err != nil {
		return nil, err
	}
	if resp.Clip == nil {
		return nil, errors.New("machine learning response missing clip embedding")
	}
	return DecodeVector(*resp.Clip)
}

// EncodeText embeds a search query, optionally with a translation language.
func (c *Client) EncodeText(ctx context.Context, text string, opts TextOptions) ([]float32, error) {
	req := NewPipeline()
	if opts.Language != "" {
		req.Add(TaskClip, TypeTextual, opts.ModelName, map[string]any{"language": opts.Language})
	} else {
		req.Add(TaskClip, TypeTextual, opts.ModelName, nil)
	}
	resp, err := c.predict(ctx, nil, text, req)
	if err != nil {
		return nil, err
	}
	if resp.Clip == nil {
		return nil, errors.New("machine learning response missing clip embedding")
	}
	return DecodeVector(*resp.Clip)
}

// OCR runs the two-stage OCR pipeline over an image file.
func (c *Client) OCR(ctx context.Context, imagePath string, opts OCROptions) (*OCRResult, error) {
	req := NewPipeline().
		Add(TaskOCR, TypeDetection, opts.ModelName, map[string]any{
			"minScore":      opts.MinDetectionScore,
			"maxResolution": opts.MaxResolution,
		}).
		Add(TaskOCR, TypeRecognition, opts.ModelName, map[string]any{"minScore": opts.MinRecognitionScore})
	img, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, err
	}
	resp, err := c.predict(ctx, img, "", req)
	if err != nil {
		return nil, err
	}
	if resp.OCR == nil {
		return nil, errors.New("machine learning response missing ocr result")
	}
	return resp.OCR, nil
}
