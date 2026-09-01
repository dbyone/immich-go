package api

import (
	"context"
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

// filterAssets applies the shared timeline query filters. Without an
// explicit visibility parameter only timeline-visible assets are
// returned — archived, hidden and locked items stay off the main
// timeline, matching the upstream contract.
func (s *Server) filterAssets(r *http.Request, userID string) []*domain.Asset {
	q := r.URL.Query()
	assets, _ := s.app.Store.Assets().ListForOwner(r.Context(), userID)
	withTrashed := q.Get("withDeleted") == "true"
	isTrashed := q.Get("isTrashed") == "true"

	visibility := q.Get("visibility")
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
		if visibility != "" {
			if a.Visibility != visibility {
				continue
			}
		} else if a.Visibility != domain.VisibilityTimeline {
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

// parseBucketKey accepts both the date-only form the mobile client sends
// (2026-06-01) and the full ISO timestamps the web client sends
// (2026-06-01T00:00:00.000Z). Falling back to time.Now() on a parse error
// silently matched the *current* month, which emptied every bucket the day
// the month rolled over.
func parseBucketKey(s string) time.Time {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	return time.Now().UTC()
}

// --- trash ---

func (s *Server) emptyTrash(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	assets, _ := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID)
	var deleted []string
	for _, asset := range assets {
		if asset.DeletedAt != nil {
			// Hard delete must clear the vector store too, or orphaned
			// embeddings keep feeding smart search, duplicate detection
			// and face clustering.
			_ = s.app.Vectors.DeleteSmartSearch(r.Context(), asset.ID)
			_ = s.app.Vectors.DeleteFaces(r.Context(), asset.ID)
			_ = s.app.Store.Assets().Delete(r.Context(), asset.ID)
			s.app.Storage.Remove(asset.OriginalPath)
			s.app.Storage.Remove(asset.ThumbnailPath)
			s.app.Storage.Remove(asset.PreviewPath)
			deleted = append(deleted, asset.ID)
		}
	}
	for _, id := range deleted {
		s.app.Realtime.BroadcastToUser(a.User.ID, "on_asset_delete", id)
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
	var restored []string
	for _, asset := range assets {
		if asset.DeletedAt != nil {
			_, _ = s.app.UpdateAsset(r.Context(), asset.ID, func(fresh *domain.Asset) error {
				fresh.DeletedAt = nil
				fresh.UpdatedAt = now
				return nil
			})
			restored = append(restored, asset.ID)
		}
	}
	if len(restored) > 0 {
		s.app.Realtime.BroadcastToUser(a.User.ID, "on_asset_restore", restored)
	}
	w.WriteHeader(http.StatusNoContent)
}

// restoreAssets untrashes the given ids only (POST /trash/restore/assets,
// BulkIdsDto -> TrashResponseDto).
func (s *Server) restoreAssets(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	now := time.Now().UTC()
	count := 0
	var restored []string
	for _, id := range req.IDs {
		asset, err := s.app.Store.Assets().Get(r.Context(), id)
		if err != nil || asset.OwnerID != a.User.ID || asset.DeletedAt == nil {
			continue
		}
		if _, err := s.app.UpdateAsset(r.Context(), id, func(fresh *domain.Asset) error {
			fresh.DeletedAt = nil
			fresh.UpdatedAt = now
			return nil
		}); err == nil {
			count++
			restored = append(restored, id)
		}
	}
	if len(restored) > 0 {
		s.app.Realtime.BroadcastToUser(a.User.ID, "on_asset_restore", restored)
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
}

// searchSuggestions answers the typeahead dropdown: distinct values of
// one metadata facet across the caller's assets (GET /search/suggestions,
// SearchSuggestionsDto -> string[]).
func (s *Server) searchSuggestions(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	typ := r.URL.Query().Get("type")
	country := r.URL.Query().Get("country")
	state := r.URL.Query().Get("state")
	makeFilter := r.URL.Query().Get("make")
	modelFilter := r.URL.Query().Get("model")
	lensFilter := r.URL.Query().Get("lensModel")

	switch typ {
	case "country", "state", "city", "camera-make", "camera-model", "camera-lens-model":
	default:
		writeError(w, http.StatusBadRequest, "invalid suggestion type")
		return
	}
	pick := func(e *domain.AssetExif) string {
		switch typ {
		case "country":
			return e.Country
		case "state":
			return e.State
		case "city":
			return e.City
		case "camera-make":
			return e.Make
		case "camera-model":
			return e.Model
		default:
			return e.LensModel
		}
	}

	assets, err := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID)
	if err != nil {
		s.storeError(w, err)
		return
	}
	seen := map[string]bool{}
	out := []string{}
	for _, asset := range assets {
		if asset.DeletedAt != nil || asset.Exif == nil {
			continue
		}
		e := asset.Exif
		if country != "" && e.Country != country {
			continue
		}
		if state != "" && e.State != state {
			continue
		}
		if makeFilter != "" && e.Make != makeFilter {
			continue
		}
		if modelFilter != "" && e.Model != modelFilter {
			continue
		}
		if lensFilter != "" && e.LensModel != lensFilter {
			continue
		}
		if v := pick(e); v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	writeJSON(w, http.StatusOK, out)
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
	// MetadataSearchDto fields that power the folders/tags UIs and the
	// MT-Photos-style file search.
	OriginalFileName string   `json:"originalFileName"`
	OriginalPath     string   `json:"originalPath"`
	TagIDs           []string `json:"tagIds"`
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
	if len(req.TagIDs) > 0 {
		assets = s.filterByTags(r.Context(), assets, req.TagIDs)
	}
	matches := s.applyMetadataFilters(assets, &req)
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].FileCreatedAt.After(matches[j].FileCreatedAt) })
	page, size := pageParams(req.Page, req.Size)
	matches = paginate(matches, page, size)

	out := make([]AssetResponse, 0, len(matches))
	for _, asset := range matches {
		out = append(out, s.assetResponse(r.Context(), asset, req.WithExif))
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
		if req.OriginalFileName != "" && !contains(lower(a.OriginalFileName), lower(req.OriginalFileName)) {
			continue
		}
		if req.OriginalPath != "" && !contains(lower(a.OriginalPath), lower(req.OriginalPath)) {
			continue
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

// searchSmart runs a CLIP text query against the machine-learning
// service and ranks the owner's assets by cosine similarity inside the
// DuckDB vector store — the same pipeline as SearchService.searchSmart
// upstream, with pgvector replaced by DuckDB. immich-go extension:
// assets whose file name or path contains the raw query rank ahead of
// the vector hits, and the query works without any ML service at all.
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

	mlReady := s.app.ML != nil &&
		s.app.Cfg.MachineLearning.Enabled &&
		s.app.Cfg.MachineLearning.Clip.Enabled &&
		s.app.ML.SupportsCLIP()

	type ranked struct {
		asset *domain.Asset
		score float64
	}
	entries := []ranked{}
	seen := map[string]bool{}
	q := lower(req.Query)

	// Exact text matches first: file names and paths are precise signals
	// (MT-Photos-style file search, also the no-ML fallback).
	assets, err := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID)
	if err != nil {
		s.storeError(w, err)
		return
	}
	for _, asset := range assets {
		if asset.DeletedAt != nil || asset.Visibility != domain.VisibilityTimeline {
			continue // upstream smart search only ranks timeline-visible assets
		}
		if contains(lower(asset.OriginalFileName), q) || contains(lower(asset.OriginalPath), q) {
			entries = append(entries, ranked{asset: asset, score: 1})
			seen[asset.ID] = true
		}
	}

	if mlReady {
		queryVec, err := s.app.ML.EncodeText(r.Context(), req.Query, ml.TextOptions{
			ModelName: s.app.Cfg.MachineLearning.Clip.ModelName,
			Language:  req.Language,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Machine learning query failed: "+err.Error())
			return
		}
		hits, err := s.app.Vectors.SearchSmart(r.Context(), a.User.ID, queryVec, 1000)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Vector search failed: "+err.Error())
			return
		}
		for _, hit := range hits {
			if seen[hit.AssetID] {
				continue
			}
			asset, err := s.app.Store.Assets().Get(r.Context(), hit.AssetID)
			if err != nil || asset.OwnerID != a.User.ID || asset.DeletedAt != nil {
				continue
			}
			entries = append(entries, ranked{asset: asset, score: hit.Score})
			seen[asset.ID] = true
		}
	}

	page, size := pageParams(req.Page, req.Size)
	start := (page - 1) * size
	if start > len(entries) {
		start = len(entries)
	}
	end := start + size
	if end > len(entries) {
		end = len(entries)
	}

	out := make([]AssetResponse, 0, end-start)
	for _, e := range entries[start:end] {
		out = append(out, s.assetResponse(r.Context(), e.asset, req.WithExif))
	}
	writeJSON(w, http.StatusOK, SearchResponse{Albums: []AlbumResponse{}, Assets: out})
}

// filterByTags keeps assets linked to any of the given tags (ANY
// semantics — a single filter value is the common case).
func (s *Server) filterByTags(ctx context.Context, assets []*domain.Asset, tagIDs []string) []*domain.Asset {
	idSet := map[string]bool{}
	byAsset := map[string]bool{}
	for _, id := range tagIDs {
		idSet[id] = true
	}
	ids := make([]string, 0, len(assets))
	for _, a := range assets {
		ids = append(ids, a.ID)
	}
	linked, err := s.app.Store.Tags().ListForAssets(ctx, ids)
	if err != nil {
		return assets
	}
	for assetID, tags := range linked {
		for _, t := range tags {
			if idSet[t.ID] {
				byAsset[assetID] = true
				break
			}
		}
	}
	out := make([]*domain.Asset, 0, len(assets))
	for _, a := range assets {
		if byAsset[a.ID] {
			out = append(out, a)
		}
	}
	return out
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
