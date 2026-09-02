// Package app wires the store, machine-learning client, job system and
// storage together, and implements the background job handlers that the
// upstream server runs in its microservices worker.
package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"immich-go/internal/auth"
	"immich-go/internal/classify"
	"immich-go/internal/config"
	"immich-go/internal/crypto"
	"immich-go/internal/domain"
	"immich-go/internal/exif"
	"immich-go/internal/jobs"
	"immich-go/internal/media"
	"immich-go/internal/ml"
	"immich-go/internal/realtime"
	"immich-go/internal/storage"
	"immich-go/internal/store"
	"immich-go/internal/store/duckstore"
	"immich-go/internal/vectordb"
	"immich-go/internal/videometa"
)

type App struct {
	Cfg     *config.Config
	Store   store.Store
	ML      ml.Provider
	Jobs    *jobs.System
	Auth    *auth.Service
	Storage *storage.Storage
	Log     *slog.Logger

	// Classifier turns stored CLIP embeddings into hierarchical scene
	// tags ("场景/<label>"); nil when scene classification is disabled.
	Classifier *classify.Classifier

	// Realtime is the Socket.IO gateway (/api/socket.io) pushing live
	// timeline/asset events to the official web and mobile clients.
	Realtime *realtime.Hub

	// Vectors is the DuckDB-backed vector store replacing the upstream
	// pgvector layer: CLIP embeddings, face embeddings, people clusters.
	Vectors *vectordb.Store

	// db is the shared DuckDB writer pool for entities and vectors; App
	// owns it (and ro, when separate) and closes them on Close.
	db *sql.DB
	ro *sql.DB

	// Debounced re-computation: batches of face/smart-search jobs collapse
	// into a single clustering / dedup run.
	clusterMu    sync.Mutex
	clusterTimer *time.Timer
	dedupMu      sync.Mutex
	dedupTimer   *time.Timer

	// assetMu serializes read-modify-write cycles on asset rows (jobs and
	// handlers alike) — DuckDB has no row locking and a naive
	// Get→mutate→Update pair can interleave with another writer.
	assetMu sync.Mutex
}

// New wires the application. A nil entity store selects the DuckDB
// persistence (default); pass store.Store to inject an alternative
// (e.g. memory in tests).
func New(cfg *config.Config, st store.Store, log *slog.Logger) (*App, error) {
	stg, err := storage.New(cfg.MediaLocation)
	if err != nil {
		return nil, err
	}

	// One DuckDB database holds everything: entity metadata and vectors.
	// File-backed databases get a second pool of read-only snapshot
	// connections so listings and auth stop queueing behind writes;
	// :memory: must share the single pool (separate opens = separate DBs).
	db, err := duckstore.OpenDB(cfg.DuckDBPath)
	if err != nil {
		return nil, fmt.Errorf("open duckdb %s: %w", cfg.DuckDBPath, err)
	}
	ro := db
	if cfg.DuckDBPath != ":memory:" && cfg.DuckDBReaders > 0 {
		if ro, err = duckstore.OpenDB(cfg.DuckDBPath); err != nil {
			db.Close()
			return nil, fmt.Errorf("open duckdb read pool: %w", err)
		}
		ro.SetMaxOpenConns(cfg.DuckDBReaders)
	}
	vectors, err := vectordb.AttachReadWrite(db, ro, cfg.VectorDim)
	if err != nil {
		ro.Close()
		db.Close()
		return nil, fmt.Errorf("init vector store: %w", err)
	}
	if st == nil {
		st, err = duckstore.NewWithReadPool(db, ro)
		if err != nil {
			ro.Close()
			db.Close()
			return nil, fmt.Errorf("init entity store: %w", err)
		}
		if ds, ok := st.(*duckstore.Store); ok {
			ds.SetReaderPoolSize(cfg.DuckDBReaders)
		}
	}
	log.Info("duckdb ready", "path", cfg.DuckDBPath, "dim", cfg.VectorDim,
		"entities", storeKind(st), "sqlCosine", vectors.HasSQLCosine())
	// Startup census: makes session-persistence regressions (e.g. a hard
	// kill losing a just-written WAL entry) visible at a glance on the
	// next boot.
	if n, err := st.Users().Count(context.Background()); err == nil {
		sessions, _ := st.Sessions().Count(context.Background())
		if assets, err := st.Assets().List(context.Background()); err == nil {
			log.Info("store census", "users", n, "sessions", sessions, "assets", len(assets))
		}
	}

	a := &App{
		Cfg:   cfg,
		Store: st,
		ML: ml.NewProvider(ml.ProviderConfig{
			Provider:           cfg.MachineLearning.Provider,
			Enabled:            cfg.MachineLearning.Enabled,
			URLs:               cfg.MachineLearning.URLs,
			APIKey:             cfg.MachineLearning.APIKey,
			AvailabilityChecks: cfg.MachineLearning.AvailabilityChecks.Enabled,
			CheckTimeout:       cfg.MachineLearning.AvailabilityChecks.Timeout,
			CheckInterval:      cfg.MachineLearning.AvailabilityChecks.Interval,
		}, log),
		Jobs:    jobs.NewSystem(log),
		Auth:    auth.NewService(st),
		Storage: stg,
		Log:     log,
		Vectors: vectors,
		db:      db,
		ro:      ro,
	}
	if cfg.MachineLearning.SceneClassification.Enabled && cfg.MachineLearning.Clip.Enabled {
		a.Classifier = classify.New(a.ML, classify.Options{
			Threshold: cfg.MachineLearning.SceneClassification.Threshold,
			TopK:      cfg.MachineLearning.SceneClassification.TopK,
		})
	}
	// Same credential chain as the REST guard; sockets join their user
	// and session rooms after the handshake.
	a.Realtime = realtime.New(func(r *http.Request) (realtime.Credentials, bool) {
		authCtx, err := a.Auth.Authenticate(context.Background(), r)
		if err != nil || authCtx == nil {
			return realtime.Credentials{}, false
		}
		creds := realtime.Credentials{UserID: authCtx.User.ID}
		if authCtx.Session != nil {
			creds.SessionID = authCtx.Session.ID
		}
		return creds, true
	}, map[string]any{
		"major": config.VersionMajor,
		"minor": config.VersionMinor,
		"patch": config.VersionPatch,
	}, log)
	a.Auth.SetSessionTTL(cfg.SessionTTL)
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
	a.clusterMu.Lock()
	if a.clusterTimer != nil {
		a.clusterTimer.Stop()
	}
	a.clusterMu.Unlock()
	a.dedupMu.Lock()
	if a.dedupTimer != nil {
		a.dedupTimer.Stop()
	}
	a.dedupMu.Unlock()
	a.Jobs.Stop()
	if a.Realtime != nil {
		a.Realtime.Close()
	}
	a.ML.Teardown()
	_ = a.Vectors.Close()
	_ = a.Store.Close()
	if a.db != nil {
		_ = a.db.Close()
	}
	if a.ro != nil && a.ro != a.db {
		_ = a.ro.Close()
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

// mergeAssetUpdate re-reads the asset and applies mutate before writing.
// The whole read-modify-write cycle runs under assetMu so background jobs
// and API handlers can never clobber each other's edits (a naive
// Get→mutate→Update pair interleaves: last writer wins with a stale row).
func (a *App) mergeAssetUpdate(ctx context.Context, id string, mutate func(*domain.Asset) error) (*domain.Asset, error) {
	a.assetMu.Lock()
	defer a.assetMu.Unlock()
	asset, err := a.Store.Assets().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := mutate(asset); err != nil {
		return nil, err
	}
	if err := a.Store.Assets().Update(ctx, asset); err != nil {
		return nil, err
	}
	return asset, nil
}

// UpdateAsset is the single mutation entry point for API handlers; it
// returns the persisted row (ErrNotFound when the asset is missing).
// mutate may abort the write by returning an error (e.g. ErrForbidden
// for a foreign asset).
func (a *App) UpdateAsset(ctx context.Context, id string, mutate func(*domain.Asset) error) (*domain.Asset, error) {
	return a.mergeAssetUpdate(ctx, id, mutate)
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
	exifRow := asset.Exif
	if exifRow == nil {
		exifRow = &domain.AssetExif{}
	}
	exifRow.FileSize = fileSize

	// Pixel dimensions from the bitstream, refined by EXIF when present.
	w, h := 0, 0
	haveDims := false
	if asset.Type == domain.AssetImage {
		if pw, ph, _, err := probeImage(asset.OriginalPath); err == nil && pw > 0 && ph > 0 {
			w, h, haveDims = pw, ph, true
		}
	}

	// Full EXIF extraction (camera, capture time, GPS, rating, ...).
	meta, err := exif.ParseFile(asset.OriginalPath)
	if err != nil {
		a.Log.Debug("exif parse failed", "asset", id, "err", err)
	}
	if err == nil && meta != nil {
		exifRow.Make = meta.Make
		exifRow.Model = meta.Model
		exifRow.LensModel = meta.LensModel
		if meta.Description != "" {
			exifRow.Description = meta.Description
		}
		exifRow.DateTimeOriginal = meta.DateTimeOriginal
		exifRow.Rating = meta.Rating
		exifRow.Latitude = meta.Latitude
		exifRow.Longitude = meta.Longitude
		if meta.Width > 0 {
			w, haveDims = meta.Width, true
		}
		if meta.Height > 0 {
			h, haveDims = meta.Height, true
		}
		if meta.DateTimeOriginal != nil {
			asset.LocalDateTime = *meta.DateTimeOriginal
		}
	}

	// Video containers: duration, dimensions, fps from the MP4 family
	// parser (pure Go) or ffprobe when installed. The parser reports
	// post-rotation dimensions already.
	if asset.Type == domain.AssetVideo {
		if info, err := videometa.ParseFile(asset.OriginalPath); err == nil && info != nil {
			a.Log.Debug("video probed", "asset", id,
				"durationMs", info.DurationMs, "dims", fmt.Sprint(info.Width, "x", info.Height),
				"fps", info.FPS, "videoCodec", info.VideoCodec, "audioCodec", info.AudioCodec,
				"rotation", info.RotationDeg)
			if info.DurationMs > 0 {
				duration := info.DurationMs
				asset.Duration = &duration
			}
			if info.Width > 0 && info.Height > 0 {
				w, h, haveDims = info.Width, info.Height, true
			}
			if info.FPS > 0 {
				fps := info.FPS
				exifRow.FPS = &fps
			}
		} else if err != nil && err != videometa.ErrNoProbe {
			a.Log.Debug("video probe failed", "asset", id, "err", err)
		}
	}

	if haveDims && w > 0 && h > 0 {
		// Orientations 5-8 describe a rotated capture; report the upright
		// dimensions the way sharp's rotate() pipeline would.
		if meta != nil && meta.Orientation >= 5 && meta.Orientation <= 8 {
			w, h = h, w
		}
		ww, hh := w, h
		asset.Width, asset.Height = &ww, &hh
		exifRow.ExifWidth, exifRow.ExifHeight = &ww, &hh
	}
	asset.Exif = exifRow
	finalAsset := asset
	if _, err := a.mergeAssetUpdate(ctx, id, func(fresh *domain.Asset) error {
		fresh.Width, fresh.Height = finalAsset.Width, finalAsset.Height
		fresh.Exif = finalAsset.Exif
		if finalAsset.Duration != nil {
			fresh.Duration = finalAsset.Duration
		}
		fresh.LocalDateTime = finalAsset.LocalDateTime
		return nil
	}); err != nil {
		return err
	}

	_ = a.Jobs.Queue(jobs.JobAssetGenerateThumbnails, assetJobData{AssetID: asset.ID})
	if a.ML != nil && a.Cfg.MachineLearning.Enabled {
		if a.Cfg.MachineLearning.Clip.Enabled && a.ML.SupportsCLIP() && asset.Type == domain.AssetImage {
			_ = a.Jobs.Queue(jobs.JobSmartSearchRun, assetJobData{AssetID: asset.ID})
		}
		if a.Cfg.MachineLearning.FacialRecognition.Enabled && a.ML.SupportsFaces() && asset.Type == domain.AssetImage {
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
	switch asset.Type {
	case domain.AssetImage:
		thumb, err := generateThumbnail(asset.OriginalPath, a.Storage, asset.OwnerID, asset.ID, "thumbnail")
		if err != nil {
			return err
		}
		preview, err := generateThumbnail(asset.OriginalPath, a.Storage, asset.OwnerID, asset.ID, "preview")
		if err != nil {
			return err
		}
		_, err = a.mergeAssetUpdate(ctx, id, func(fresh *domain.Asset) error {
			fresh.ThumbnailPath = thumb
			fresh.PreviewPath = preview
			return nil
		})
		return err
	case domain.AssetVideo:
		return a.generateVideoPoster(ctx, asset)
	default:
		return nil
	}
}

// generateVideoPoster extracts a poster frame with ffmpeg, derives the
// preview and thumbnail renditions, and stores both. Without ffmpeg the
// job completes without renditions — the thumbnail endpoint then reports
// that no preview is available for the asset.
func (a *App) generateVideoPoster(ctx context.Context, asset *domain.Asset) error {
	if !media.HasFFmpeg() {
		a.Log.Info("ffmpeg not installed; skipping video poster", "asset", asset.ID)
		return nil
	}
	at := 0.0
	if asset.Duration != nil && *asset.Duration > 2000 {
		at = 1.0 // one second in, mirroring a stable opening frame
	}
	frame, err := media.ExtractFrame(asset.OriginalPath, at, media.PreviewMax)
	if err != nil {
		return fmt.Errorf("video poster: %w", err)
	}
	preview, err := a.Storage.WriteThumb(asset.OwnerID, asset.ID, "preview", frame)
	if err != nil {
		return err
	}
	small, err := media.GenerateThumbFromBytes(frame, media.ThumbnailMax)
	if err != nil {
		return err
	}
	thumb, err := a.Storage.WriteThumb(asset.OwnerID, asset.ID, "thumbnail", small)
	if err != nil {
		return err
	}
	_, err = a.mergeAssetUpdate(ctx, asset.ID, func(fresh *domain.Asset) error {
		fresh.ThumbnailPath = thumb
		fresh.PreviewPath = preview
		return nil
	})
	return err
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
	if !a.ML.SupportsCLIP() {
		return ml.ErrUnsupported
	}
	vec, err := a.ML.EncodeImage(ctx, asset.OriginalPath, a.Cfg.MachineLearning.Clip.ModelName)
	if err != nil {
		return fmt.Errorf("smart search embedding: %w", err)
	}
	if _, err := a.mergeAssetUpdate(ctx, id, func(fresh *domain.Asset) error {
		fresh.SmartEmbedding = vec
		return nil
	}); err != nil {
		return err
	}
	// Persist the embedding in the DuckDB vector store (smart_search).
	if err := a.Vectors.UpsertSmartSearch(ctx, asset.ID, asset.OwnerID,
		a.Cfg.MachineLearning.Clip.ModelName, vec); err != nil {
		return fmt.Errorf("vector upsert: %w", err)
	}
	// Zero-shot scene tagging rides on the freshly stored embedding: the
	// classifier only calls the provider to embed its label taxonomy once.
	if a.Classifier != nil {
		if err := a.applySceneTags(ctx, asset, vec); err != nil {
			a.Log.Warn("scene classification failed", "asset", asset.ID, "err", err)
		}
	}
	if a.Cfg.MachineLearning.DuplicateDetection.Enabled {
		a.scheduleDuplicateDetection()
	}
	return nil
}

// applySceneTags files the asset under hierarchical "场景/<label>" tags
// and removes stale scene tags that no longer clear the threshold.
func (a *App) applySceneTags(ctx context.Context, asset *domain.Asset, vec []float32) error {
	scores, err := a.Classifier.Classify(ctx, vec)
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, s := range scores {
		tag, err := a.Store.Tags().UpsertValue(ctx, asset.OwnerID, sceneTagPrefix+s.Label.ZH)
		if err != nil {
			return err
		}
		if _, err := a.Store.Tags().Attach(ctx, tag.ID, []string{asset.ID}); err != nil {
			return err
		}
		want[tag.ID] = true
	}
	current, err := a.Store.Tags().ListForAsset(ctx, asset.ID)
	if err != nil {
		return err
	}
	for _, tag := range current {
		if !strings.HasPrefix(tag.Value, sceneTagPrefix) || want[tag.ID] {
			continue
		}
		if _, err := a.Store.Tags().Detach(ctx, tag.ID, []string{asset.ID}); err != nil {
			return err
		}
	}
	return nil
}

// sceneTagPrefix namespaces auto-generated scene tags so they never
// collide with user tags.
const sceneTagPrefix = "场景/"

func (a *App) handleDetectFaces(ctx context.Context, _ string, data any) error {
	id, err := jobAssetID(data)
	if err != nil {
		return err
	}
	asset, err := a.Store.Assets().Get(ctx, id)
	if err != nil {
		return err
	}
	if !a.ML.SupportsFaces() {
		// The configured AI provider (e.g. mt-photos-ai) has no face
		// endpoints; skip cleanly instead of failing the queue.
		return nil
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
				AssetID: asset.ID,
				FaceIdx: i,
				Box:     [4]int{f.BoundingBox.X1, f.BoundingBox.Y1, f.BoundingBox.X2, f.BoundingBox.Y2},
				Vec:     vec,
			})
		}
	}
	finalFaces := faces
	if _, err := a.mergeAssetUpdate(ctx, id, func(fresh *domain.Asset) error {
		fresh.Faces = finalFaces
		return nil
	}); err != nil {
		return err
	}
	// Persist face embeddings for clustering (face_search). A re-detect
	// yielding zero faces must clear the previous rows, not leave them.
	if len(rows) == 0 {
		if err := a.Vectors.DeleteFaces(ctx, asset.ID); err != nil {
			return fmt.Errorf("face vector cleanup: %w", err)
		}
	} else if err := a.Vectors.UpsertFaces(ctx, asset.OwnerID, asset.ID, rows); err != nil {
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
	for _, u := range users {
		groups, err := a.Vectors.DetectDuplicates(ctx, u.ID, a.Cfg.MachineLearning.DuplicateDetection.MaxDistance)
		if err != nil {
			return fmt.Errorf("duplicate detection for user %s: %w", u.ID, err)
		}
		// Only live assets may (re)form a group: pairs the user already
		// resolved keep a trashed member and must not resurrect, and
		// groups below two live members are dissolved outright.
		live := map[string]bool{}
		assets, _ := a.Store.Assets().ListForOwner(ctx, u.ID)
		for _, asset := range assets {
			live[asset.ID] = asset.DeletedAt == nil
		}
		grouped := map[string]string{} // assetID -> groupID
		for _, members := range groups {
			var liveMembers []string
			for _, id := range members {
				if live[id] {
					liveMembers = append(liveMembers, id)
				}
			}
			if len(liveMembers) < 2 {
				continue
			}
			groupID := crypto.NewUUID()
			for _, id := range liveMembers {
				grouped[id] = groupID
			}
		}
		for _, asset := range assets {
			want, inGroup := grouped[asset.ID]
			need := (inGroup && (asset.DuplicateID == nil || *asset.DuplicateID != want)) ||
				(!inGroup && asset.DuplicateID != nil)
			if !need {
				continue
			}
			assetID := asset.ID
			_, _ = a.mergeAssetUpdate(ctx, assetID, func(fresh *domain.Asset) error {
				if inGroup {
					id := want
					fresh.DuplicateID = &id
				} else {
					fresh.DuplicateID = nil
				}
				return nil
			})
		}
		if len(grouped) > 0 {
			a.Log.Info("duplicate detection complete", "user", u.ID, "groups", len(grouped))
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
	if !a.ML.SupportsOCR() {
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
