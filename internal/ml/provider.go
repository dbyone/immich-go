// Package ml — pluggable AI provider layer.
//
// The server talks to two wire-level dialects behind one interface:
//
//   - "immich":   the immich-machine-learning /ping + /predict contract
//     (faces + CLIP + OCR, one pipeline request per call);
//   - "mtphotos": the mt-photos-ai sidecar contract (CLIP + OCR only,
//     api-key header, Chinese-CLIP ViT-B-16 under the hood).
package ml

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// ErrUnsupported marks a capability the configured provider cannot serve
// (e.g. mt-photos-ai has no face endpoints). Callers skip the work
// gracefully instead of failing the job.
var ErrUnsupported = errors.New("capability not supported by this AI provider")

// Provider is the AI surface consumed by the app pipeline. Implementations
// must be safe for concurrent use.
type Provider interface {
	// Name identifies the dialect: "immich" or "mtphotos".
	Name() string
	// Supports* report per-capability availability.
	SupportsCLIP() bool
	SupportsFaces() bool
	SupportsOCR() bool

	EncodeImage(ctx context.Context, imagePath, modelName string) ([]float32, error)
	EncodeText(ctx context.Context, text string, opts TextOptions) ([]float32, error)
	DetectFaces(ctx context.Context, imagePath string, opts FaceDetectionOptions) (*FaceDetectionResult, error)
	OCR(ctx context.Context, imagePath string, opts OCROptions) (*OCRResult, error)

	// Ping probes one base URL for liveness/auth validity.
	Ping(baseURL string) bool
	// Teardown stops background health loops.
	Teardown()
}

// ProviderConfig is the provider-agnostic subset of the machine-learning
// configuration.
type ProviderConfig struct {
	Provider           string // "immich" (default) or "mtphotos"
	Enabled            bool
	URLs               []string
	APIKey             string // mtphotos only: the sidecar's API_AUTH_KEY
	AvailabilityChecks bool
	CheckTimeout       time.Duration
	CheckInterval      time.Duration
}

// NewProvider builds the provider selected by cfg.Provider.
func NewProvider(cfg ProviderConfig, logger *slog.Logger) Provider {
	switch cfg.Provider {
	case "mtphotos", "mt-photos-ai":
		return NewMTPhotos(cfg, logger)
	default:
		clientCfg := Config{
			Enabled:            cfg.Enabled,
			URLs:               cfg.URLs,
			AvailabilityChecks: cfg.AvailabilityChecks,
			CheckTimeout:       cfg.CheckTimeout,
			CheckInterval:      cfg.CheckInterval,
		}
		return NewClient(clientCfg, logger)
	}
}

// Name makes the immich dialect's *Client satisfy Provider.
func (c *Client) Name() string { return "immich" }

func (c *Client) SupportsCLIP() bool  { return true }
func (c *Client) SupportsFaces() bool { return true }
func (c *Client) SupportsOCR() bool   { return true }
