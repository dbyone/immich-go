// Package app wires the store, machine-learning client, job system and
// storage together, and implements the background job handlers that the
// upstream server runs in its microservices worker.
package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"immich-go/internal/auth"
	"immich-go/internal/config"
	"immich-go/internal/crypto"
	"immich-go/internal/domain"
	"immich-go/internal/jobs"
	"immich-go/internal/ml"
	"immich-go/internal/storage"
	"immich-go/internal/store"
	"immich-go/internal/store/duckstore"
	"immich-go/internal/vectordb"
)

type App struct {
	Cfg     *config.Config
	Store   store.Store
	ML      *ml.Client
	Jobs    *jobs.System
	Auth    *auth.Service
	Storage *storage.Storage
	Log     *slog.Logger

	// Vectors is the DuckDB-backed vector store replacing the upstream
	// pgvector layer: CLIP embeddings, face embeddings, people clusters.
	Vectors *vectordb.Store

	// db is the shared DuckDB connection for entities and vectors; App
	// owns it and closes it on Close.
	db *sql.DB

	// Debounced re-computation: batches of face/smart-search jobs collapse
	// into a single clustering / dedup run.
	clusterMu    sync.Mutex
	clusterTimer *time.Timer
	dedupMu      sync.Mutex
	dedupTimer   *time.Timer
}

// New wires the application. A nil entity store selects the DuckDB
// persistence (default); pass store.Store to inject an alternative
// (e.g. memory in tests).
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

	// One DuckDB database holds everything: entity metadata and vectors.
	db, err := duckstore.OpenDB(cfg.DuckDBPath)
	if err != nil {
		return nil, fmt.Errorf("open duckdb %s: %w", cfg.DuckDBPath, err)
	}
	vectors, err := vectordb.Attach(db, cfg.VectorDim)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("init vector store: %w", err)
	}
	if st == nil {
		st, err = duckstore.New(db)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("init entity store: %w", err)
		}
	}
	log.Info("duckdb ready", "path", cfg.DuckDBPath, "dim", cfg.VectorDim,
		"entities", storeKind(st), "sqlCosine", vectors.HasSQLCosine())

	a := &App{
		Cfg:     cfg,
		Store:   st,
		ML:      ml.NewClient(mlCfg, log),
		Jobs:    jobs.NewSystem(log),
		Auth:    auth.NewService(st),
		Storage: stg,
		Log:     log,
		Vectors: vectors,
		db:      db,
	}
	a.registerJobs()
	return a, nil
}

func storeKind(st store.Store) string {
	if st == nil {
		return "nil"
	}
	return fmt.Sprintf("%T", st)
}

// Close releases background resources.
func (a *App) Close() {
	a.Jobs.Stop()
	a.ML.Teardown()
	_ = a.Vectors.Close()
	_ = a.Store.Close()
	if a.db != nil {
		_ = a.db.Close()
	}
}

func (a *App) registerJobs() {
	a.Jobs.Register(jobs.JobAssetExtractMetadata, jobs.QueueMetadataExtraction, a.handleExtractMetadata)
	a.Jobs.Register(jobs.JobAssetGenerateThumbnails, jobs.QueueThumbnailGeneration, a.handleGenerateThumbnails)
	a.Jobs.Register(jobs.JobSmartSearchRun, jobs.QueueSmartSearch, a.handleSmartSearch)
	a.Jobs.Register(jobs.JobAssetDetectFaces, jobs.QueueFaceDetection, a.handleDetectFaces)
	a.Jobs.Register(jobs.JobFacialRecognitionRun, jobs.QueueFacialRecognition, a.handleFacialRecognition)
	a.Jobs.Register(jobs.JobDuplicateDetectionRun, jobs.QueueDuplicateDetection, a.handleDuplicateDetection)
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
	if err := a.Store.Assets().Update(ctx, asset); err != nil {
		return err
	}
	// Persist the embedding in the DuckDB vector store (smart_search).
	if err := a.Vectors.UpsertSmartSearch(ctx, asset.ID, asset.OwnerID,
		a.Cfg.MachineLearning.Clip.ModelName, vec); err != nil {
		return fmt.Errorf("vector upsert: %w", err)
	}
	if a.Cfg.MachineLearning.DuplicateDetection.Enabled {
		a.scheduleDuplicateDetection()
	}
	return nil
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
	rows := make([]vectordb.FaceRow, 0, len(res.Faces))
	for i, f := range res.Faces {
		faces = append(faces, domain.Face{
			BoundingBox: [4]int{f.BoundingBox.X1, f.BoundingBox.Y1, f.BoundingBox.X2, f.BoundingBox.Y2},
			Embedding:   f.Embedding,
			Score:       f.Score,
		})
		if vec, err := ml.DecodeVector(f.Embedding); err == nil {
			rows = append(rows, vectordb.FaceRow{
				AssetID:  asset.ID,
				FaceIdx:  i,
				Box:      [4]int{f.BoundingBox.X1, f.BoundingBox.Y1, f.BoundingBox.X2, f.BoundingBox.Y2},
				Vec:      vec,
			})
		}
	}
	asset.Faces = faces
	if err := a.Store.Assets().Update(ctx, asset); err != nil {
		return err
	}
	// Persist face embeddings for clustering (face_search).
	if err := a.Vectors.UpsertFaces(ctx, asset.OwnerID, asset.ID, rows); err != nil {
		return fmt.Errorf("face vector upsert: %w", err)
	}
	if a.Cfg.MachineLearning.FacialRecognition.Enabled {
		a.scheduleClustering()
	}
	return nil
}

// handleFacialRecognition clusters every user's face embeddings into
// people (DBSCAN over the DuckDB face_search table).
func (a *App) handleFacialRecognition(ctx context.Context, _ string, _ any) error {
	users, err := a.Store.Users().List(ctx)
	if err != nil {
		return err
	}
	total := 0
	for _, u := range users {
		people, err := a.Vectors.ClusterFaces(ctx, u.ID,
			a.Cfg.MachineLearning.FacialRecognition.MaxDistance,
			a.Cfg.MachineLearning.FacialRecognition.MinFaces)
		if err != nil {
			return fmt.Errorf("cluster faces for user %s: %w", u.ID, err)
		}
		total += people
	}
	if total > 0 {
		a.Log.Info("face clustering complete", "people", total)
	}
	return nil
}

// handleDuplicateDetection groups visually near-identical assets via CLIP
// embeddings and stamps duplicateId onto the assets.
func (a *App) handleDuplicateDetection(ctx context.Context, _ string, _ any) error {
	if !a.Cfg.MachineLearning.DuplicateDetection.Enabled {
		return nil
	}
	users, err := a.Store.Users().List(ctx)
	if err != nil {
		return err
	}
	cleared := map[string]bool{}
	for _, u := range users {
		groups, err := a.Vectors.DetectDuplicates(ctx, u.ID, a.Cfg.MachineLearning.DuplicateDetection.MaxDistance)
		if err != nil {
			return fmt.Errorf("duplicate detection for user %s: %w", u.ID, err)
		}
		grouped := map[string]string{} // assetID -> groupID
		for _, members := range groups {
			groupID := crypto.NewUUID()
			for _, id := range members {
				grouped[id] = groupID
			}
		}
		assets, _ := a.Store.Assets().ListForOwner(ctx, u.ID)
		for _, asset := range assets {
			want, inGroup := grouped[asset.ID]
			switch {
			case inGroup && (asset.DuplicateID == nil || *asset.DuplicateID != want):
				asset.DuplicateID = &want
				_ = a.Store.Assets().Update(ctx, asset)
			case !inGroup && !cleared[asset.ID] && asset.DuplicateID != nil:
				asset.DuplicateID = nil
				_ = a.Store.Assets().Update(ctx, asset)
			}
			cleared[asset.ID] = true
		}
		if len(groups) > 0 {
			a.Log.Info("duplicate detection complete", "user", u.ID, "groups", len(groups))
		}
	}
	return nil
}

// scheduleClustering debounces DBSCAN runs while face-detection jobs keep
// arriving, so a burst of uploads yields one clustering pass.
func (a *App) scheduleClustering() {
	debounce := a.Cfg.ClusterDebounce
	if debounce <= 0 {
		debounce = 5 * time.Second
	}
	a.clusterMu.Lock()
	defer a.clusterMu.Unlock()
	if a.clusterTimer != nil {
		a.clusterTimer.Stop()
	}
	a.clusterTimer = time.AfterFunc(debounce, func() {
		_ = a.Jobs.Queue(jobs.JobFacialRecognitionRun, map[string]string{"trigger": "debounce"})
	})
}

func (a *App) scheduleDuplicateDetection() {
	debounce := a.Cfg.ClusterDebounce
	if debounce <= 0 {
		debounce = 5 * time.Second
	}
	a.dedupMu.Lock()
	defer a.dedupMu.Unlock()
	if a.dedupTimer != nil {
		a.dedupTimer.Stop()
	}
	a.dedupTimer = time.AfterFunc(debounce, func() {
		_ = a.Jobs.Queue(jobs.JobDuplicateDetectionRun, map[string]string{"trigger": "debounce"})
	})
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
	if err := a.Vectors.DeleteSmartSearch(ctx, id); err != nil {
		a.Log.Warn("vector cleanup failed", "asset", id, "err", err)
	}
	if err := a.Vectors.DeleteFaces(ctx, id); err != nil {
		a.Log.Warn("face cleanup failed", "asset", id, "err", err)
	}
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
