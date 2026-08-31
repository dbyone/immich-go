// Package config loads server configuration from environment variables.
// Variable names intentionally mirror the official Immich server so that a
// deployment can swap containers without changing the environment.
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// Version reported by /api/server/version — tracks the upstream Immich
	// API version this server is compatible with.
	VersionMajor = 3
	VersionMinor = 1
	VersionPatch = 0

	DefaultPort               = 2283
	DefaultHost               = "0.0.0.0"
	DefaultMediaLocation      = "./data"
	DefaultMachineLearningURL = "http://immich-machine-learning:3003"
)

type MachineLearning struct {
	Enabled bool
	// Provider selects the AI dialect: "immich" (default, the
	// immich-machine-learning /ping + /predict contract) or "mtphotos"
	// (the mt-photos-ai sidecar: api-key auth, /clip/*, /ocr — no faces).
	Provider string
	// APIKey is the mt-photos-ai sidecar's API_AUTH_KEY value
	// (IMMICH_MACHINE_LEARNING_API_KEY); unused by the immich dialect.
	APIKey             string
	URLs               []string
	AvailabilityChecks struct {
		Enabled  bool
		Timeout  time.Duration
		Interval time.Duration
	}
	Clip struct {
		Enabled   bool
		ModelName string
	}
	FacialRecognition struct {
		Enabled     bool
		ModelName   string
		MinScore    float64
		MaxDistance float64 // DBSCAN cosine-distance threshold for clustering
		MinFaces    int     // minimum cluster size to become a person
	}
	DuplicateDetection struct {
		Enabled     bool
		MaxDistance float64 // CLIP distance below which assets are duplicates
	}
	OCR struct {
		Enabled             bool
		ModelName           string
		MinDetectionScore   float64
		MinRecognitionScore float64
		MaxResolution       int
	}
	// SceneClassification is the MT-Photos-inspired zero-shot tagger:
	// assets are matched against a built-in bilingual scene taxonomy via
	// the CLIP provider and filed under hierarchical "场景/<label>" tags.
	SceneClassification struct {
		Enabled   bool
		Threshold float64 // minimum cosine similarity to accept a label
		TopK      int     // labels kept per asset
	}
}

type Config struct {
	Port          int
	Host          string
	MediaLocation string

	// DuckDB — the single durable state of the server: entity metadata
	// plus the vector store. Path defaults to <media>/immich.duckdb;
	// IMMICH_VECTOR_DB is honored as a legacy alias. Dim must match the
	// embedding dimension of the configured models (512 by default).
	DuckDBPath string
	VectorDim  int

	// UploadLimitMB caps a single upload's request body
	// (IMMICH_UPLOAD_LIMIT_MB); 0 disables the cap.
	UploadLimitMB int

	// DuckDBReaders sizes the read connection pool for file-backed
	// databases (IMMICH_DUCKDB_READERS); :memory: always shares the
	// single writer pool.
	DuckDBReaders int

	// Debounce window batching clustering runs after face detection.
	ClusterDebounce time.Duration

	// Session token lifetime (official server: none by default — sessions
	// live until logged out; 400-day cookie maxAge). 0 disables expiry.
	// IMMICH_SESSION_TTL_HOURS overrides.
	SessionTTL time.Duration

	MachineLearning MachineLearning

	// Map selects the basemap dialect: "osm" (default, Immich-hosted
	// vector styles) or "amap" (Gaode raster tiles for mainland China;
	// photo coordinates shift onto GCJ-02 inside China only).
	Map struct {
		Provider string
		AMAPKey  string // reserved for AMap REST services (geocoding)
	}
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v != "false" && v != "0"
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

// Load builds the configuration from the process environment, applying the
// same defaults the upstream Immich server uses.
func Load() *Config {
	c := &Config{
		Port:            envInt("IMMICH_PORT", DefaultPort),
		Host:            env("IMMICH_HOST", DefaultHost),
		MediaLocation:   env("IMMICH_MEDIA_LOCATION", DefaultMediaLocation),
		VectorDim:       envInt("IMMICH_VECTOR_DIM", 512),
		ClusterDebounce: time.Duration(envInt("IMMICH_CLUSTER_DEBOUNCE_MS", 5000)) * time.Millisecond,
		UploadLimitMB:   envInt("IMMICH_UPLOAD_LIMIT_MB", 8192),
		DuckDBReaders:   envInt("IMMICH_DUCKDB_READERS", 4),
	}
	if hours := envInt("IMMICH_SESSION_TTL_HOURS", 9600); hours > 0 {
		c.SessionTTL = time.Duration(hours) * time.Hour
	}
	c.DuckDBPath = env("IMMICH_DUCKDB", "")
	if c.DuckDBPath == "" {
		c.DuckDBPath = env("IMMICH_VECTOR_DB", "") // legacy alias
	}
	if c.DuckDBPath == "" {
		c.DuckDBPath = filepath.Join(c.MediaLocation, "immich.duckdb")
	}

	ml := &c.MachineLearning
	ml.Enabled = envBool("IMMICH_MACHINE_LEARNING_ENABLED", true)
	ml.Provider = strings.ToLower(env("IMMICH_MACHINE_LEARNING_PROVIDER", "immich"))
	ml.APIKey = env("IMMICH_MACHINE_LEARNING_API_KEY", "")
	ml.URLs = []string{env("IMMICH_MACHINE_LEARNING_URL", DefaultMachineLearningURL)}
	ml.AvailabilityChecks.Enabled = envBool("IMMICH_MACHINE_LEARNING_AVAILABILITY_CHECK", true)
	ml.AvailabilityChecks.Timeout = time.Duration(envInt("IMMICH_MACHINE_LEARNING_AVAILABILITY_CHECK_TIMEOUT", 2000)) * time.Millisecond
	ml.AvailabilityChecks.Interval = time.Duration(envInt("IMMICH_MACHINE_LEARNING_AVAILABILITY_CHECK_INTERVAL", 30_000)) * time.Millisecond

	ml.Clip.Enabled = envBool("IMMICH_MACHINE_LEARNING_CLIP_ENABLED", true)
	ml.Clip.ModelName = env("IMMICH_MACHINE_LEARNING_CLIP_MODEL", "ViT-B-32__openai")

	ml.FacialRecognition.Enabled = envBool("IMMICH_MACHINE_LEARNING_FACIAL_RECOGNITION_ENABLED", true)
	ml.FacialRecognition.ModelName = env("IMMICH_MACHINE_LEARNING_FACIAL_RECOGNITION_MODEL", "buffalo_l")
	ml.FacialRecognition.MinScore = envFloat("IMMICH_MACHINE_LEARNING_FACIAL_RECOGNITION_MIN_SCORE", 0.7)
	ml.FacialRecognition.MaxDistance = envFloat("IMMICH_MACHINE_LEARNING_FACIAL_RECOGNITION_MAX_DISTANCE", 0.5)
	ml.FacialRecognition.MinFaces = envInt("IMMICH_MACHINE_LEARNING_FACIAL_RECOGNITION_MIN_FACES", 3)

	ml.DuplicateDetection.Enabled = envBool("IMMICH_MACHINE_LEARNING_DUPLICATE_DETECTION_ENABLED", true)
	ml.DuplicateDetection.MaxDistance = envFloat("IMMICH_MACHINE_LEARNING_DUPLICATE_DETECTION_MAX_DISTANCE", 0.01)

	c.Map.Provider = strings.ToLower(env("IMMICH_MAP_PROVIDER", "osm"))
	c.Map.AMAPKey = env("IMMICH_MAP_AMAP_KEY", "")

	ml.OCR.Enabled = envBool("IMMICH_MACHINE_LEARNING_OCR_ENABLED", true)
	ml.OCR.ModelName = env("IMMICH_MACHINE_LEARNING_OCR_MODEL", "PP-OCRv5_mobile")
	ml.OCR.MinDetectionScore = envFloat("IMMICH_MACHINE_LEARNING_OCR_DETECT_SCORE", 0.5)
	ml.OCR.MinRecognitionScore = envFloat("IMMICH_MACHINE_LEARNING_OCR_RECOGNIZE_SCORE", 0.8)
	ml.OCR.MaxResolution = envInt("IMMICH_MACHINE_LEARNING_OCR_MAX_RESOLUTION", 736)

	// Chinese-CLIP (mt-photos-ai) runs hotter than OpenAI CLIP; give it a
	// higher acceptance threshold unless the operator overrides.
	sceneThresholdDefault := 0.24
	if ml.Provider == "mtphotos" {
		sceneThresholdDefault = 0.30
	}
	ml.SceneClassification.Enabled = envBool("IMMICH_SCENE_CLASSIFICATION_ENABLED", false)
	ml.SceneClassification.Threshold = envFloat("IMMICH_SCENE_CLASSIFICATION_THRESHOLD", sceneThresholdDefault)
	ml.SceneClassification.TopK = envInt("IMMICH_SCENE_CLASSIFICATION_TOP_K", 3)

	// Comma-separated extra URLs allow the multi-instance failover that the
	// upstream repository supports.
	if extra := os.Getenv("IMMICH_MACHINE_LEARNING_URLS"); extra != "" {
		for _, u := range strings.Split(extra, ",") {
			if u = strings.TrimSpace(u); u != "" {
				ml.URLs = append(ml.URLs, u)
			}
		}
	}
	return c
}
