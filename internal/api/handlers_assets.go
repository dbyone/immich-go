package api

import (
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
				writeError(w, http.StatusInternalServerError, "failed to store upload: "+err.Error())
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
		storeError(w, err)
		return
	}

	s.app.QueueAssetPipeline(asset.ID)
	writeJSON(w, http.StatusCreated, AssetMediaResponse{ID: asset.ID, Status: "created"})
}

func extOf(name string) string {
	idx := strings.LastIndexByte(name, '.')
	if idx < 0 || idx == len(name)-1 || idx < len(name)-6 {
		return ".bin"
	}
	return strings.ToLower(name[idx:])
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
	writeJSON(w, http.StatusOK, s.assetResponse(asset, true))
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
	IsFavorite *bool   `json:"isFavorite"`
	Visibility *string `json:"visibility"`
}

func (s *Server) updateAsset(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	asset, err := s.app.Store.Assets().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || asset.OwnerID != a.User.ID {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	var req updateAssetRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.IsFavorite != nil {
		asset.IsFavorite = *req.IsFavorite
	}
	if req.Visibility != nil {
		switch *req.Visibility {
		case domain.VisibilityArchive, domain.VisibilityTimeline, domain.VisibilityHidden, domain.VisibilityLocked:
			asset.Visibility = *req.Visibility
		default:
			writeError(w, http.StatusBadRequest, "invalid visibility")
			return
		}
	}
	asset.UpdatedAt = time.Now().UTC()
	if err := s.app.Store.Assets().Update(r.Context(), asset); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.assetResponse(asset, true))
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
			if existing, err := s.app.Store.Assets().GetByChecksum(r.Context(), a.User.ID, sum); err == nil {
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
	for _, id := range req.IDs {
		asset, err := s.app.Store.Assets().Get(r.Context(), id)
		if err != nil || asset.OwnerID != a.User.ID {
			continue
		}
		if req.Force {
			_ = s.app.Jobs.Queue(jobs.JobAssetDelete, app.AssetJobData(id))
		} else {
			asset.DeletedAt = &now
			asset.UpdatedAt = now
			_ = s.app.Store.Assets().Update(r.Context(), asset)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

type bulkUpdateRequest struct {
	IDs         []string `json:"ids"`
	IsFavorite  *bool    `json:"isFavorite"`
	Visibility  *string  `json:"visibility"`
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
	now := time.Now().UTC()
	for _, id := range req.IDs {
		asset, err := s.app.Store.Assets().Get(r.Context(), id)
		if err != nil || asset.OwnerID != a.User.ID {
			continue
		}
		if req.IsFavorite != nil {
			asset.IsFavorite = *req.IsFavorite
		}
		if req.Visibility != nil {
			asset.Visibility = *req.Visibility
		}
		asset.UpdatedAt = now
		_ = s.app.Store.Assets().Update(r.Context(), asset)
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
	// Renditions are generated asynchronously; fall back to whatever
	// rendition exists, then to the original.
	if path == "" {
		if asset.ThumbnailPath != "" {
			path = asset.ThumbnailPath
		} else {
			path = asset.OriginalPath
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
