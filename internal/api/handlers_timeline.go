package api

import (
	"net/http"
	"sort"
	"time"

	"immich-go/internal/domain"
	"immich-go/internal/ml"
)

// bucketKey formats a date as the YYYY-MM-01 bucket identifier the mobile
// client round-trips (monthly buckets, keyed by first day of month).
func bucketKey(t time.Time) string {
	return t.UTC().Format("2006-01") + "-01"
}

// filterAssets applies the shared timeline/search query filters.
func (s *Server) filterAssets(r *http.Request, userID string) []*domain.Asset {
	q := r.URL.Query()
	assets, _ := s.app.Store.Assets().ListForOwner(r.Context(), userID)
	withTrashed := q.Get("withDeleted") == "true"
	isTrashed := q.Get("isTrashed") == "true"

	var out []*domain.Asset
	for _, a := range assets {
		trashed := a.DeletedAt != nil
		switch {
		case isTrashed:
			if !trashed {
				continue
			}
		case trashed && !withTrashed:
			continue
		}
		if v := q.Get("visibility"); v != "" && a.Visibility != v {
			continue
		}
		if v := q.Get("isFavorite"); v == "true" && !a.IsFavorite {
			continue
		}
		if albumID := q.Get("albumId"); albumID != "" {
			al, err := s.app.Store.Albums().Get(r.Context(), albumID)
			if err != nil || !al.HasAsset(a.ID) {
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

func (s *Server) timelineBuckets(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	assets := s.filterAssets(r, a.User.ID)
	counts := map[string]int{}
	for _, asset := range assets {
		counts[bucketKey(asset.FileCreatedAt)]++
	}
	out := make([]TimeBucketResponse, 0, len(counts))
	for bucket, count := range counts {
		out = append(out, TimeBucketResponse{Count: count, TimeBucket: bucket})
	}
	// Newest bucket first, the client renders top-down.
	sort.Slice(out, func(i, j int) bool { return out[i].TimeBucket > out[j].TimeBucket })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) timelineBucket(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	timeBucket := r.URL.Query().Get("timeBucket")
	if len(timeBucket) < 7 {
		writeError(w, http.StatusBadRequest, "timeBucket is required (YYYY-MM-DD)")
		return
	}
	assets := s.filterAssets(r, a.User.ID)

	inBucket := []*domain.Asset{}
	for _, asset := range assets {
		if bucketKey(asset.FileCreatedAt) == bucketKey(parseBucketKey(timeBucket)) {
			inBucket = append(inBucket, asset)
		}
	}
	order := r.URL.Query().Get("order")
	sort.SliceStable(inBucket, func(i, j int) bool {
		if order == "asc" {
			return inBucket[i].FileCreatedAt.Before(inBucket[j].FileCreatedAt)
		}
		return inBucket[i].FileCreatedAt.After(inBucket[j].FileCreatedAt)
	})

	resp := TimeBucketAssetResponse{
		ID:               make([]string, 0, len(inBucket)),
		CreatedAt:        make([]ISOTime, 0, len(inBucket)),
		FileCreatedAt:    make([]ISOTime, 0, len(inBucket)),
		Duration:         make([]*int64, 0, len(inBucket)),
		IsFavorite:       make([]bool, 0, len(inBucket)),
		IsImage:          make([]bool, 0, len(inBucket)),
		IsTrashed:        make([]bool, 0, len(inBucket)),
		LivePhotoVideoID: make([]*string, 0, len(inBucket)),
		LocalOffsetHours: make([]float64, 0, len(inBucket)),
		OwnerID:          make([]string, 0, len(inBucket)),
		ProjectionType:   make([]*string, 0, len(inBucket)),
		Ratio:            make([]float64, 0, len(inBucket)),
		Thumbhash:        make([]*string, 0, len(inBucket)),
		Visibility:       make([]string, 0, len(inBucket)),
	}
	for _, asset := range inBucket {
		resp.ID = append(resp.ID, asset.ID)
		resp.CreatedAt = append(resp.CreatedAt, ISOTime(asset.CreatedAt))
		resp.FileCreatedAt = append(resp.FileCreatedAt, ISOTime(asset.FileCreatedAt))
		resp.Duration = append(resp.Duration, asset.Duration)
		resp.IsFavorite = append(resp.IsFavorite, asset.IsFavorite)
		resp.IsImage = append(resp.IsImage, asset.Type == domain.AssetImage)
		resp.IsTrashed = append(resp.IsTrashed, asset.DeletedAt != nil)
		resp.LivePhotoVideoID = append(resp.LivePhotoVideoID, asset.LivePhotoVideoID)
		offset := asset.FileCreatedAt.Sub(asset.LocalDateTime).Hours()
		resp.LocalOffsetHours = append(resp.LocalOffsetHours, offset)
		resp.OwnerID = append(resp.OwnerID, asset.OwnerID)
		resp.ProjectionType = append(resp.ProjectionType, nil)
		ratio := 0.0
		if asset.Width != nil && asset.Height != nil && *asset.Height != 0 {
			ratio = float64(*asset.Width) / float64(*asset.Height)
		}
		resp.Ratio = append(resp.Ratio, ratio)
		if asset.Thumbhash != "" {
			th := asset.Thumbhash
			resp.Thumbhash = append(resp.Thumbhash, &th)
		} else {
			resp.Thumbhash = append(resp.Thumbhash, nil)
		}
		resp.Visibility = append(resp.Visibility, asset.Visibility)
	}
	writeJSON(w, http.StatusOK, resp)
}

func parseBucketKey(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Now().UTC()
	}
	return t
}

// --- trash ---

func (s *Server) emptyTrash(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	assets, _ := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID)
	for _, asset := range assets {
		if asset.DeletedAt != nil {
			_ = s.app.Store.Assets().Delete(r.Context(), asset.ID)
			s.app.Storage.Remove(asset.OriginalPath)
			s.app.Storage.Remove(asset.ThumbnailPath)
			s.app.Storage.Remove(asset.PreviewPath)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) restoreTrash(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	assets, _ := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID)
	now := time.Now().UTC()
	for _, asset := range assets {
		if asset.DeletedAt != nil {
			asset.DeletedAt = nil
			asset.UpdatedAt = now
			_ = s.app.Store.Assets().Update(r.Context(), asset)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- search ---

type searchRequest struct {
	Query       string `json:"query"`
	Page        int    `json:"page"`
	Size        int    `json:"size"`
	Language    string `json:"language"`
	Type        string `json:"type"`
	IsFavorite  *bool  `json:"isFavorite"`
	TakenAfter  string `json:"takenAfter"`
	TakenBefore string `json:"takenBefore"`
	WithDeleted bool   `json:"withDeleted"`
	WithExif    bool   `json:"withExif"`
	City        string `json:"city"`
	Country     string `json:"country"`
	State       string `json:"state"`
	Make        string `json:"make"`
	Model       string `json:"model"`
}

func (s *Server) searchMetadata(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req searchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	assets, _ := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID)
	matches := s.applyMetadataFilters(assets, &req)
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].FileCreatedAt.After(matches[j].FileCreatedAt) })
	page, size := pageParams(req.Page, req.Size)
	matches = paginate(matches, page, size)

	out := make([]AssetResponse, 0, len(matches))
	for _, asset := range matches {
		out = append(out, s.assetResponse(asset, req.WithExif))
	}
	writeJSON(w, http.StatusOK, SearchResponse{Albums: []AlbumResponse{}, Assets: out})
}

func (s *Server) applyMetadataFilters(assets []*domain.Asset, req *searchRequest) []*domain.Asset {
	var after, before time.Time
	if req.TakenAfter != "" {
		if t, err := time.Parse(time.RFC3339, req.TakenAfter); err == nil {
			after = t
		}
	}
	if req.TakenBefore != "" {
		if t, err := time.Parse(time.RFC3339, req.TakenBefore); err == nil {
			before = t
		}
	}
	var out []*domain.Asset
	for _, a := range assets {
		if !req.WithDeleted && a.DeletedAt != nil {
			continue
		}
		if req.Type != "" && a.Type != req.Type {
			continue
		}
		if req.IsFavorite != nil && a.IsFavorite != *req.IsFavorite {
			continue
		}
		if !after.IsZero() && a.FileCreatedAt.Before(after) {
			continue
		}
		if !before.IsZero() && a.FileCreatedAt.After(before) {
			continue
		}
		if req.Query != "" {
			q := lower(req.Query)
			if !contains(lower(a.OriginalFileName), q) && !contains(lower(a.ExifDescription()), q) {
				continue
			}
		}
		if req.City != "" || req.Country != "" || req.State != "" || req.Make != "" || req.Model != "" {
			if a.Exif == nil {
				continue
			}
			if req.City != "" && !contains(lower(a.Exif.City), lower(req.City)) {
				continue
			}
			if req.Country != "" && !contains(lower(a.Exif.Country), lower(req.Country)) {
				continue
			}
			if req.State != "" && !contains(lower(a.Exif.State), lower(req.State)) {
				continue
			}
			if req.Make != "" && !contains(lower(a.Exif.Make), lower(req.Make)) {
				continue
			}
			if req.Model != "" && !contains(lower(a.Exif.Model), lower(req.Model)) {
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

// searchSmart runs a CLIP text query against the machine-learning service
// and ranks the owner's assets by cosine similarity — the same pipeline as
// SearchService.searchSmart upstream.
func (s *Server) searchSmart(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req searchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}
	if !s.app.Cfg.MachineLearning.Enabled || !s.app.Cfg.MachineLearning.Clip.Enabled {
		writeError(w, http.StatusBadRequest, "Smart search is disabled")
		return
	}

	queryVec, err := s.app.ML.EncodeText(r.Context(), req.Query, ml.TextOptions{
		ModelName: s.app.Cfg.MachineLearning.Clip.ModelName,
		Language:  req.Language,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Machine learning query failed: "+err.Error())
		return
	}

	assets, _ := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID)
	req.WithDeleted = false
	req.Query = "" // metadata filters already applied below if provided
	type scored struct {
		asset *domain.Asset
		score float64
	}
	var ranked []scored
	for _, asset := range assets {
		if asset.DeletedAt != nil || len(asset.SmartEmbedding) == 0 {
			continue
		}
		ranked = append(ranked, scored{asset, ml.CosineSimilarity(queryVec, asset.SmartEmbedding)})
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })

	page, size := pageParams(req.Page, req.Size)
	start := (page - 1) * size
	if start > len(ranked) {
		start = len(ranked)
	}
	end := start + size
	if end > len(ranked) {
		end = len(ranked)
	}
	out := make([]AssetResponse, 0, end-start)
	for _, hit := range ranked[start:end] {
		out = append(out, s.assetResponse(hit.asset, req.WithExif))
	}
	writeJSON(w, http.StatusOK, SearchResponse{Albums: []AlbumResponse{}, Assets: out})
}

func pageParams(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 250 // upstream default page size
	}
	if size > 1000 {
		size = 1000
	}
	return page, size
}

func paginate[T any](items []T, page, size int) []T {
	start := (page - 1) * size
	if start > len(items) {
		start = len(items)
	}
	end := start + size
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

func contains(s, substr string) bool {
	return len(substr) == 0 || indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
