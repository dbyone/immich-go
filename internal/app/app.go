// Package app wires the store, machine-learning client, job system and
// storage together, and implements the background job handlers that the
// upstream server runs in its microservices worker.
package app

import (
	"context"
	"fmt"
	"log/slog"

	"immich-go/internal/auth"
	"immich-go/internal/config"
	"immich-go/internal/domain"
	"immich-go/internal/jobs"
	"immich-go/internal/ml"
	"immich-go/internal/storage"
	"immich-go/internal/store"
)

type App struct {
	Cfg     *config.Config
	Store   store.Store
	ML      *ml.Client
	Jobs    *jobs.System
	Auth    *auth.Service
	Storage *storage.Storage
	Log     *slog.Logger
}

func New(cfg *config.Config, st store.Store, log *slog.Logger) (*App, error) {
	stg, err := storage.New(cfg.MediaLocation)
	if err != nil {
		return nil, err
	}
	mlCfg := ml.Config{
		Enabled:            cfg.MachineLearning.Enabled,
		URLs:               cfg.MachineLearning.URLs,
		AvailabilityChecks: cfg.MachineLearning.AvailabilityChecks.Enabled,
		CheckTimeout:       cfg.MachineLearning.AvailabilityChecks.Timeout,
		CheckInterval:      cfg.MachineLearning.AvailabilityChecks.Interval,
	}
	a := &App{
		Cfg:     cfg,
		Store:   st,
		ML:      ml.NewClient(mlCfg, log),
		Jobs:    jobs.NewSystem(log),
		Auth:    auth.NewService(st),
		Storage: stg,
		Log:     log,
	}
	a.registerJobs()
	return a, nil
}

// Close releases background resources.
func (a *App) Close() {
	a.Jobs.Stop()
	a.ML.Teardown()
	_ = a.Store.Close()
}

func (a *App) registerJobs() {
	a.Jobs.Register(jobs.JobAssetExtractMetadata, jobs.QueueMetadataExtraction, a.handleExtractMetadata)
	a.Jobs.Register(jobs.JobAssetGenerateThumbnails, jobs.QueueThumbnailGeneration, a.handleGenerateThumbnails)
	a.Jobs.Register(jobs.JobSmartSearchRun, jobs.QueueSmartSearch, a.handleSmartSearch)
	a.Jobs.Register(jobs.JobAssetDetectFaces, jobs.QueueFaceDetection, a.handleDetectFaces)
	a.Jobs.Register(jobs.JobAssetDelete, jobs.QueueBackgroundTask, a.handleAssetDelete)
	a.Jobs.Register(jobs.JobOcrRun, jobs.QueueOCR, a.handleOCR)
}

// QueueAssetPipeline schedules the post-upload pipeline exactly like the
// upstream metadata-extraction flow: metadata first; that handler then
// fans out to thumbnails, smart search and face detection.
func (a *App) QueueAssetPipeline(assetID string) {
	_ = a.Jobs.Queue(jobs.JobAssetExtractMetadata, map[string]string{"assetId": assetID})
}

type assetJobData struct {
	AssetID string `json:"assetId"`
}

// AssetJobData is the canonical payload for asset-scoped jobs.
func AssetJobData(assetID string) map[string]string {
	return map[string]string{"assetId": assetID}
}

func (a *App) handleExtractMetadata(ctx context.Context, _ string, data any) error {
	id, err := jobAssetID(data)
	if err != nil {
		return err
	}
	asset, err := a.Store.Assets().Get(ctx, id)
	if err != nil {
		return err
	}

	fileSize := int64(0)
	if fi, err := fileSizeOf(asset.OriginalPath); err == nil {
		fileSize = fi
	}
	exif := asset.Exif
	if exif == nil {
		exif = &domain.AssetExif{}
	}
	exif.FileSize = fileSize

	if asset.Type == domain.AssetImage {
		if w, h, _, err := probeImage(asset.OriginalPath); err == nil {
			ww, hh := w, h
			asset.Width, asset.Height = &ww, &hh
			exif.ExifWidth, exif.ExifHeight = &ww, &hh
		}
	}
	asset.Exif = exif
	if err := a.Store.Assets().Update(ctx, asset); err != nil {
		return err
	}

	_ = a.Jobs.Queue(jobs.JobAssetGenerateThumbnails, assetJobData{AssetID: asset.ID})
	if a.ML != nil && a.Cfg.MachineLearning.Enabled {
		if a.Cfg.MachineLearning.Clip.Enabled && asset.Type == domain.AssetImage {
			_ = a.Jobs.Queue(jobs.JobSmartSearchRun, assetJobData{AssetID: asset.ID})
		}
		if a.Cfg.MachineLearning.FacialRecognition.Enabled && asset.Type == domain.AssetImage {
			_ = a.Jobs.Queue(jobs.JobAssetDetectFaces, assetJobData{AssetID: asset.ID})
		}
	}
	return nil
}

func (a *App) handleGenerateThumbnails(ctx context.Context, _ string, data any) error {
	id, err := jobAssetID(data)
	if err != nil {
		return err
	}
	asset, err := a.Store.Assets().Get(ctx, id)
	if err != nil {
		return err
	}
	if asset.Type != domain.AssetImage {
		return nil // video transcoding is out of scope for this port
	}

	thumb, err := generateThumbnail(asset.OriginalPath, a.Storage, asset.OwnerID, asset.ID, "thumbnail")
	if err != nil {
		return err
	}
	preview, err := generateThumbnail(asset.OriginalPath, a.Storage, asset.OwnerID, asset.ID, "preview")
	if err != nil {
		return err
	}
	asset.ThumbnailPath = thumb
	asset.PreviewPath = preview
	return a.Store.Assets().Update(ctx, asset)
}

func (a *App) handleSmartSearch(ctx context.Context, _ string, data any) error {
	id, err := jobAssetID(data)
	if err != nil {
		return err
	}
	asset, err := a.Store.Assets().Get(ctx, id)
	if err != nil {
		return err
	}
	vec, err := a.ML.EncodeImage(ctx, asset.OriginalPath, a.Cfg.MachineLearning.Clip.ModelName)
	if err != nil {
		return fmt.Errorf("smart search embedding: %w", err)
	}
	asset.SmartEmbedding = vec
	return a.Store.Assets().Update(ctx, asset)
}

func (a *App) handleDetectFaces(ctx context.Context, _ string, data any) error {
	id, err := jobAssetID(data)
	if err != nil {
		return err
	}
	asset, err := a.Store.Assets().Get(ctx, id)
	if err != nil {
		return err
	}
	res, err := a.ML.DetectFaces(ctx, asset.OriginalPath, ml.FaceDetectionOptions{
		ModelName: a.Cfg.MachineLearning.FacialRecognition.ModelName,
		MinScore:  a.Cfg.MachineLearning.FacialRecognition.MinScore,
	})
	if err != nil {
		return fmt.Errorf("face detection: %w", err)
	}
	faces := make([]domain.Face, 0, len(res.Faces))
	for _, f := range res.Faces {
		faces = append(faces, domain.Face{
			BoundingBox: [4]int{f.BoundingBox.X1, f.BoundingBox.Y1, f.BoundingBox.X2, f.BoundingBox.Y2},
			Embedding:   f.Embedding,
			Score:       f.Score,
		})
	}
	asset.Faces = faces
	return a.Store.Assets().Update(ctx, asset)
}

func (a *App) handleOCR(ctx context.Context, _ string, data any) error {
	id, err := jobAssetID(data)
	if err != nil {
		return err
	}
	asset, err := a.Store.Assets().Get(ctx, id)
	if err != nil {
		return err
	}
	if !a.Cfg.MachineLearning.OCR.Enabled {
		return nil
	}
	_, err = a.ML.OCR(ctx, asset.OriginalPath, ml.OCROptions{
		ModelName:           a.Cfg.MachineLearning.OCR.ModelName,
		MinDetectionScore:   a.Cfg.MachineLearning.OCR.MinDetectionScore,
		MinRecognitionScore: a.Cfg.MachineLearning.OCR.MinRecognitionScore,
		MaxResolution:       a.Cfg.MachineLearning.OCR.MaxResolution,
	})
	if err != nil {
		return fmt.Errorf("ocr: %w", err)
	}
	return nil
}

func (a *App) handleAssetDelete(ctx context.Context, _ string, data any) error {
	id, err := jobAssetID(data)
	if err != nil {
		return err
	}
	asset, err := a.Store.Assets().Get(ctx, id)
	if err != nil {
		return nil // already gone
	}
	a.Storage.Remove(asset.OriginalPath)
	a.Storage.Remove(asset.ThumbnailPath)
	a.Storage.Remove(asset.PreviewPath)
	return a.Store.Assets().Delete(ctx, id)
}

func jobAssetID(data any) (string, error) {
	switch d := data.(type) {
	case assetJobData:
		return d.AssetID, nil
	case map[string]string:
		if id, ok := d["assetId"]; ok {
			return id, nil
		}
	case map[string]any:
		if id, ok := d["assetId"].(string); ok {
			return id, nil
		}
	case string:
		return d, nil
	}
	return "", fmt.Errorf("job data missing assetId: %#v", data)
}
