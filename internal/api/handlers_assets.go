package api

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"immich-go/internal/app"
	"immich-go/internal/auth"
	"immich-go/internal/crypto"
	"immich-go/internal/domain"
	"immich-go/internal/jobs"
	"immich-go/internal/storage"
	"immich-go/internal/store"
)

type uploadForm struct {
	FileCreatedAt    string
	FileModifiedAt   string
	OriginalFileName string
	Duration         string
	IsFavorite       string
	Visibility       string
	DeviceAssetID    string
	DeviceID         string
	Checksum         string // x-immich-checksum header value
}

func parseUploadTime(s string, def time.Time) (time.Time, error) {
	if s == "" {
		return def, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, errBadTime
}

var errBadTime = &badTimeError{}

type badTimeError struct{}

func (*badTimeError) Error() string { return "invalid date-time value" }

// uploadAsset handles POST /api/assets — a multipart upload streamed to
// disk while hashing, mirroring AssetUploadInterceptor + AssetMediaService.
func (s *Server) uploadAsset(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	if !s.requirePermission(w, r, "asset.create") {
		return
	}

	// Enforce the configured upload cap before streaming anything to disk.
	if limit := int64(s.app.Cfg.UploadLimitMB) * 1024 * 1024; limit > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, limit)
	}

	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "multipart/form-data required")
		return
	}

	form := uploadForm{Checksum: r.Header.Get("x-immich-checksum")}
	var savedPath, savedSumB64 string
	var savedSum []byte
	var contentType, fileExt string

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid multipart body")
			return
		}
		name := part.FormName()
		switch name {
		case "assetData", "file":
			if savedPath != "" {
				// A second file part would orphan the first upload on
				// disk; drain and ignore it.
				io.Copy(io.Discard, part)
				part.Close()
				continue
			}
			fileName := part.FileName()
			if fileName == "" {
				fileName = "upload.bin"
			}
			if contentType == "" {
				contentType = part.Header.Get("Content-Type")
			}
			// Extension first from filename, falling back to the part's
			// declared content type.
			fileExt = extOf(fileName)
			if fileExt == "" && contentType != "" {
				fileExt = "." + strings.SplitN(contentType, "/", 2)[1]
			}

			// Optional pre-flight dedup: the client supplied the SHA-1 of
			// the file it is about to send.
			if form.Checksum != "" {
				if sum, ok := crypto.DecodeB64SHA1(form.Checksum); ok {
					if existing, err := s.app.Store.Assets().GetByChecksum(r.Context(), a.User.ID, sum); err == nil {
						io.Copy(io.Discard, part)
						part.Close()
						writeJSON(w, http.StatusCreated, AssetMediaResponse{ID: existing.ID, Status: "duplicate"})
						return
					}
				}
			}

			id := crypto.NewUUID()
			path, sum, sumB64, _, err := s.app.Storage.SaveUpload(part, a.User.ID, id, fileExt)
			part.Close()
			if err != nil {
				if isBodyTooLarge(err) {
					writeError(w, http.StatusRequestEntityTooLarge, "Upload exceeds the configured size limit")
					return
				}
				writeError(w, http.StatusInternalServerError, "failed to store upload")
				return
			}
			savedPath, savedSum, savedSumB64 = path, sum, sumB64
			form.OriginalFileName = fileName
		case "sidecarData":
			id := crypto.NewUUID()
			path, _, _, _, _ := s.app.Storage.SaveUpload(part, a.User.ID, id, ".xmp")
			part.Close()
			_ = path // sidecars are persisted next to the library copy upstream
		default:
			value, _ := io.ReadAll(io.LimitReader(part, 1<<20))
			part.Close()
			switch name {
			case "fileCreatedAt":
				form.FileCreatedAt = string(value)
			case "fileModifiedAt":
				form.FileModifiedAt = string(value)
			case "deviceAssetId":
				form.DeviceAssetID = string(value)
			case "deviceId":
				form.DeviceID = string(value)
			case "duration":
				form.Duration = string(value)
			case "isFavorite":
				form.IsFavorite = string(value)
			case "visibility":
				form.Visibility = string(value)
			}
		}
	}

	if savedPath == "" {
		writeError(w, http.StatusBadRequest, "assetData is required")
		return
	}

	// Dedup against the just-computed checksum.
	if existing, err := s.app.Store.Assets().GetByChecksum(r.Context(), a.User.ID, savedSum); err == nil {
		s.app.Storage.Remove(savedPath)
		writeJSON(w, http.StatusCreated, AssetMediaResponse{ID: existing.ID, Status: "duplicate"})
		return
	}

	now := time.Now().UTC()
	fileCreatedAt, err := parseUploadTime(form.FileCreatedAt, now)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid fileCreatedAt")
		return
	}
	fileModifiedAt, err := parseUploadTime(form.FileModifiedAt, fileCreatedAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid fileModifiedAt")
		return
	}

	assetType := storage.AssetTypeFromMime(contentType, form.OriginalFileName)
	visibility := form.Visibility
	switch visibility {
	case domain.VisibilityArchive, domain.VisibilityTimeline, domain.VisibilityHidden, domain.VisibilityLocked:
	default:
		visibility = domain.VisibilityTimeline
	}

	asset := &domain.Asset{
		ID:               crypto.NewUUID(),
		OwnerID:          a.User.ID,
		Type:             assetType,
		OriginalPath:     savedPath,
		OriginalFileName: form.OriginalFileName,
		OriginalMimeType: contentType,
		FileCreatedAt:    fileCreatedAt,
		FileModifiedAt:   fileModifiedAt,
		LocalDateTime:    fileCreatedAt,
		CreatedAt:        now,
		UpdatedAt:        now,
		IsFavorite:       form.IsFavorite == "true",
		Checksum:         savedSum,
		ChecksumB64:      savedSumB64,
		Visibility:       visibility,
	}
	if form.Duration != "" {
		if ms, err := strconv.ParseInt(form.Duration, 10, 64); err == nil && ms >= 0 {
			asset.Duration = &ms
		}
	}
	if err := s.app.Store.Assets().Create(r.Context(), asset); err != nil {
		s.storeError(w, err)
		return
	}

	s.app.QueueAssetPipeline(asset.ID)
	// Live timeline update for the owner's other sessions.
	s.app.Realtime.BroadcastToUser(a.User.ID, "on_upload_success", s.assetResponse(r.Context(), asset, false))
	writeJSON(w, http.StatusCreated, AssetMediaResponse{ID: asset.ID, Status: "created"})
}

func extOf(name string) string {
	idx := strings.LastIndexByte(name, '.')
	if idx < 0 || idx == len(name)-1 || idx < len(name)-6 {
		return ".bin"
	}
	return strings.ToLower(name[idx:])
}

// isBodyTooLarge detects http.MaxBytesReader rejections.
func isBodyTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

func (s *Server) getAsset(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "asset.read") {
		return
	}
	asset, err := s.app.Store.Assets().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || !s.canSeeAsset(r, asset) {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	writeJSON(w, http.StatusOK, s.assetResponse(r.Context(), asset, true))
}

// canSeeAsset reports whether the caller may access the asset: the owner
// or any member of an album containing it.
func (s *Server) canSeeAsset(r *http.Request, asset *domain.Asset) bool {
	a := auth.FromRequest(r)
	if a == nil {
		return false
	}
	if asset.OwnerID == a.User.ID {
		return true
	}
	albums, _ := s.app.Store.Albums().List(r.Context())
	for _, al := range albums {
		if al.HasAsset(asset.ID) {
			if al.OwnerID == a.User.ID {
				return true
			}
			for _, u := range al.Users {
				if u.UserID == a.User.ID {
					return true
				}
			}
		}
	}
	return false
}

type updateAssetRequest struct {
	IsFavorite *bool    `json:"isFavorite"`
	Visibility *string  `json:"visibility"`
	Latitude   *float64 `json:"latitude"`
	Longitude  *float64 `json:"longitude"`
}

func (s *Server) updateAsset(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req updateAssetRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Visibility != nil {
		switch *req.Visibility {
		case domain.VisibilityArchive, domain.VisibilityTimeline, domain.VisibilityHidden, domain.VisibilityLocked:
		default:
			writeError(w, http.StatusBadRequest, "invalid visibility")
			return
		}
	}
	asset, err := s.app.UpdateAsset(r.Context(), chiURLParam(r, "id"), func(asset *domain.Asset) error {
		if asset.OwnerID != a.User.ID {
			return store.ErrForbidden
		}
		if req.IsFavorite != nil {
			asset.IsFavorite = *req.IsFavorite
		}
		if req.Visibility != nil {
			asset.Visibility = *req.Visibility
		}
		// Manual location edit from the asset detail panel: the web picker
		// (and mobile clients) send WGS-84 coordinates, which live in the
		// EXIF record; the response carries the updated exifInfo so the
		// detail panel map re-renders immediately.
		if req.Latitude != nil || req.Longitude != nil {
			if asset.Exif == nil {
				asset.Exif = &domain.AssetExif{}
			}
			if req.Latitude != nil {
				asset.Exif.Latitude = req.Latitude
			}
			if req.Longitude != nil {
				asset.Exif.Longitude = req.Longitude
			}
		}
		asset.UpdatedAt = time.Now().UTC()
		return nil
	})
	if err != nil {
		s.storeError(w, err)
		return
	}
	resp := s.assetResponse(r.Context(), asset, true)
	s.app.Realtime.BroadcastToUser(a.User.ID, "on_asset_update", resp)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) assetStatistics(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	stats := AssetStatsResponse{}
	if assets, err := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID); err == nil {
		for _, asset := range assets {
			if asset.DeletedAt != nil {
				continue
			}
			if asset.Type == domain.AssetVideo {
				stats.Videos++
			} else {
				stats.Images++
			}
		}
	}
	stats.Total = stats.Images + stats.Videos
	writeJSON(w, http.StatusOK, stats)
}

type bulkUploadCheckRequest struct {
	Assets []struct {
		ID       string `json:"id"`
		Checksum string `json:"checksum"`
	} `json:"assets"`
}

// bulkUploadCheck lets clients learn which assets are already on the
// server before uploading (mobile background sync).
func (s *Server) bulkUploadCheck(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req bulkUploadCheckRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	type result struct {
		ID        string  `json:"id"`
		Action    string  `json:"action"`
		Reason    *string `json:"reason"`
		AssetID   *string `json:"assetId"`
		IsTrashed *bool   `json:"isTrashed"`
	}
	results := make([]result, 0, len(req.Assets))
	for _, item := range req.Assets {
		res := result{ID: item.ID, Action: "accept"}
		if sum, ok := crypto.DecodeB64SHA1(item.Checksum); ok {
			// Match trashed assets too: the client can then un-trash
			// instead of re-uploading (upstream isTrashed semantics).
			if existing, err := s.app.Store.Assets().GetByChecksumAny(r.Context(), a.User.ID, sum); err == nil {
				res.Action = "reject"
				reason := "duplicate"
				res.Reason = &reason
				res.AssetID = &existing.ID
				if existing.DeletedAt != nil {
					trashed := true
					res.IsTrashed = &trashed
				}
			}
		}
		results = append(results, res)
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

type assetJobsRequest struct {
	AssetIDs []string `json:"assetIds"`
	Name     string   `json:"name"`
}

func (s *Server) assetJobs(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req assetJobsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	for _, id := range req.AssetIDs {
		asset, err := s.app.Store.Assets().Get(r.Context(), id)
		if err != nil || asset.OwnerID != a.User.ID {
			continue
		}
		switch req.Name {
		case "regenerate-thumbnail":
			_ = s.app.Jobs.Queue(jobs.JobAssetGenerateThumbnails, app.AssetJobData(id))
		case "refresh-metadata":
			_ = s.app.Jobs.Queue(jobs.JobAssetExtractMetadata, app.AssetJobData(id))
		default:
			writeError(w, http.StatusBadRequest, "unknown asset job: "+req.Name)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

type bulkDeleteRequest struct {
	IDs   []string `json:"ids"`
	Force bool     `json:"force"`
}

func (s *Server) bulkDeleteAssets(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req bulkDeleteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	now := time.Now().UTC()
	var trashed, deleted []string
	for _, id := range req.IDs {
		if req.Force {
			_, _ = s.app.UpdateAsset(r.Context(), id, func(asset *domain.Asset) error {
				if asset.OwnerID != a.User.ID {
					return store.ErrForbidden
				}
				return nil
			})
			_ = s.app.Jobs.Queue(jobs.JobAssetDelete, app.AssetJobData(id))
			deleted = append(deleted, id)
		} else {
			_, _ = s.app.UpdateAsset(r.Context(), id, func(asset *domain.Asset) error {
				if asset.OwnerID != a.User.ID {
					return store.ErrForbidden
				}
				asset.DeletedAt = &now
				asset.UpdatedAt = now
				return nil
			})
			trashed = append(trashed, id)
		}
	}
	if len(trashed) > 0 {
		s.app.Realtime.BroadcastToUser(a.User.ID, "on_asset_trash", trashed)
	}
	for _, id := range deleted {
		s.app.Realtime.BroadcastToUser(a.User.ID, "on_asset_delete", id)
	}
	w.WriteHeader(http.StatusNoContent)
}

type bulkUpdateRequest struct {
	IDs        []string `json:"ids"`
	IsFavorite *bool    `json:"isFavorite"`
	Visibility *string  `json:"visibility"`
}

func (s *Server) bulkUpdateAssets(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req bulkUpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	validVisibility := req.Visibility == nil || *req.Visibility == domain.VisibilityArchive ||
		*req.Visibility == domain.VisibilityTimeline || *req.Visibility == domain.VisibilityHidden ||
		*req.Visibility == domain.VisibilityLocked
	if !validVisibility {
		writeError(w, http.StatusBadRequest, "invalid visibility")
		return
	}
	now := time.Now().UTC()
	for _, id := range req.IDs {
		updated, err := s.app.UpdateAsset(r.Context(), id, func(asset *domain.Asset) error {
			if asset.OwnerID != a.User.ID {
				return store.ErrForbidden
			}
			if req.IsFavorite != nil {
				asset.IsFavorite = *req.IsFavorite
			}
			if req.Visibility != nil {
				asset.Visibility = *req.Visibility
			}
			asset.UpdatedAt = now
			return nil
		})
		if err == nil && updated != nil {
			s.app.Realtime.BroadcastToUser(a.User.ID, "on_asset_update", s.assetResponse(r.Context(), updated, false))
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- file serving ---

func (s *Server) getAssetOriginal(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.serveableAsset(w, r)
	if !ok {
		return
	}
	s.serveAssetFile(w, r, asset.OriginalPath, asset.OriginalMimeType, asset.OriginalFileName)
}

func (s *Server) getAssetVideoPlayback(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.serveableAsset(w, r)
	if !ok {
		return
	}
	if asset.Type != domain.AssetVideo {
		writeError(w, http.StatusBadRequest, "Asset is not a video")
		return
	}
	s.serveAssetFile(w, r, asset.OriginalPath, asset.OriginalMimeType, asset.OriginalFileName)
}

func (s *Server) getAssetThumbnail(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.serveableAsset(w, r)
	if !ok {
		return
	}
	size := r.URL.Query().Get("size")
	switch size {
	case "thumbnail", "preview", "fullsize":
	default:
		size = "preview"
	}

	path := asset.PreviewPath
	if size == "thumbnail" && asset.ThumbnailPath != "" {
		path = asset.ThumbnailPath
	}
	if path == "" {
		if asset.ThumbnailPath != "" {
			path = asset.ThumbnailPath
		} else if asset.Type == domain.AssetImage {
			// Renditions are generated asynchronously; images can fall
			// back to the original bytes.
			path = asset.OriginalPath
		} else {
			// Videos without a poster frame have nothing image-like to
			// serve (ffmpeg absent or job still pending).
			writeError(w, http.StatusNotFound, "Preview not yet available")
			return
		}
	}
	contentType := "image/jpeg"
	if path == asset.OriginalPath {
		contentType = storage.MimeTypeByAsset(asset)
	}
	s.serveAssetFile(w, r, path, contentType, "")
}

func (s *Server) serveableAsset(w http.ResponseWriter, r *http.Request) (*domain.Asset, bool) {
	if !s.requirePermission(w, r, "asset.read") {
		return nil, false
	}
	asset, err := s.app.Store.Assets().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || !s.canSeeAsset(r, asset) {
		writeError(w, http.StatusNotFound, "Not found")
		return nil, false
	}
	return asset, true
}

func (s *Server) serveAssetFile(w http.ResponseWriter, r *http.Request, path, contentType, downloadName string) {
	f, err := s.app.Storage.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "File not found")
		return
	}
	defer f.Close()
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	if downloadName != "" {
		disposition := mime.FormatMediaType("inline", map[string]string{"filename": downloadName})
		w.Header().Set("Content-Disposition", disposition)
	}
	http.ServeContent(w, r, "", time.Time{}, f)
}

// refreshAsset re-runs the post-upload pipeline for one asset: EXIF /
// video metadata first, which fans out to thumbnails, smart search (and
// scene classification) and face detection. immich-go extension backing
// a per-photo refresh affordance.
func (s *Server) refreshAsset(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	asset, err := s.app.Store.Assets().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || asset.OwnerID != a.User.ID {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	s.app.QueueAssetPipeline(asset.ID)
	w.WriteHeader(http.StatusNoContent)
}

// assetClassification scores one asset against the scene taxonomy live,
// reading the stored CLIP embedding — no persistence, no extra model
// calls beyond the cached label embeddings. immich-go extension.
func (s *Server) assetClassification(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	if s.app.Classifier == nil {
		writeError(w, http.StatusBadRequest, "Scene classification is disabled")
		return
	}
	asset, err := s.app.Store.Assets().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || asset.OwnerID != a.User.ID {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	vec, err := s.app.Vectors.GetSmartSearch(r.Context(), asset.ID)
	if err != nil {
		s.storeError(w, err)
		return
	}
	if vec == nil {
		writeError(w, http.StatusNotFound, "No embedding for asset")
		return
	}
	scores, err := s.app.Classifier.Classify(r.Context(), vec)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Classification failed: "+err.Error())
		return
	}
	type entry struct {
		Label string  `json:"label"`
		Score float64 `json:"score"`
	}
	out := []entry{}
	for _, sc := range scores {
		out = append(out, entry{Label: sc.Label.ZH, Score: sc.Score})
	}
	writeJSON(w, http.StatusOK, out)
}
