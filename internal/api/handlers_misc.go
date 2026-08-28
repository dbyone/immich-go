package api

import (
	"archive/zip"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"immich-go/internal/domain"
	"immich-go/internal/store"
)

// ---- duplicates resolution ----

func (s *Server) resolveDuplicates(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req struct {
		Groups []struct {
			DuplicateID   string   `json:"duplicateId"`
			KeepAssetIDs  []string `json:"keepAssetIds"`
			TrashAssetIDs []string `json:"trashAssetIds"`
		} `json:"groups"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	now := time.Now().UTC()
	results := []BulkIDResponse{}
	for _, g := range req.Groups {
		keep := map[string]bool{}
		for _, id := range g.KeepAssetIDs {
			keep[id] = true
		}
		for _, id := range g.TrashAssetIDs {
			res := BulkIDResponse{ID: id, Success: true}
			if keep[id] {
				// A kept asset must not be trashed; the overlap is an
				// error, not a silent no-op.
				res.Success = false
				res.Error = "duplicate"
			} else {
				_, err := s.app.UpdateAsset(r.Context(), id, func(asset *domain.Asset) error {
					if asset.OwnerID != a.User.ID {
						return store.ErrForbidden
					}
					asset.DeletedAt = &now
					asset.UpdatedAt = now
					return nil
				})
				if err != nil {
					res.Success = false
					res.Error = "not_found"
				}
			}
			results = append(results, res)
		}
		// Dissolve the group for everyone once resolved.
		assets, _ := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID)
		for _, asset := range assets {
			if asset.DuplicateID != nil && *asset.DuplicateID == g.DuplicateID {
				_, _ = s.app.UpdateAsset(r.Context(), asset.ID, func(fresh *domain.Asset) error {
					fresh.DuplicateID = nil
					fresh.UpdatedAt = now
					return nil
				})
			}
		}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) deleteDuplicatesBulk(w http.ResponseWriter, r *http.Request) {
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
	s.deleteDuplicateGroups(w, r, a.User.ID, req.IDs)
}

func (s *Server) deleteDuplicateGroup(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	s.deleteDuplicateGroups(w, r, a.User.ID, []string{chiURLParam(r, "id")})
}

// deleteDuplicateGroups dissolves duplicate groups (clears duplicateId)
// without touching the assets themselves.
func (s *Server) deleteDuplicateGroups(w http.ResponseWriter, r *http.Request, ownerID string, groupIDs []string) {
	drop := map[string]bool{}
	for _, id := range groupIDs {
		drop[id] = true
	}
	now := time.Now().UTC()
	assets, _ := s.app.Store.Assets().ListForOwner(r.Context(), ownerID)
	for _, asset := range assets {
		if asset.DuplicateID != nil && drop[*asset.DuplicateID] {
			_, _ = s.app.UpdateAsset(r.Context(), asset.ID, func(fresh *domain.Asset) error {
				fresh.DuplicateID = nil
				fresh.UpdatedAt = now
				return nil
			})
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- download ----

func (s *Server) downloadInfo(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req struct {
		AlbumID     string   `json:"albumId"`
		AssetIDs    []string `json:"assetIds"`
		ArchiveSize int64    `json:"archiveSize"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	// Resolve either explicit ids or the album contents (owner-visible).
	var assets []*domain.Asset
	if len(req.AssetIDs) > 0 {
		for _, id := range req.AssetIDs {
			if asset, err := s.app.Store.Assets().Get(r.Context(), id); err == nil && asset.OwnerID == a.User.ID {
				assets = append(assets, asset)
			}
		}
	} else if req.AlbumID != "" {
		if album, err := s.app.Store.Albums().Get(r.Context(), req.AlbumID); err == nil && s.canSeeAlbum(r, album) {
			for _, id := range album.AssetIDs {
				if asset, err := s.app.Store.Assets().Get(r.Context(), id); err == nil {
					assets = append(assets, asset)
				}
			}
		}
	}
	const defaultArchiveSize = 4 << 30
	limit := req.ArchiveSize
	if limit <= 0 {
		limit = defaultArchiveSize
	}
	type archive struct {
		AssetIDs []string `json:"assetIds"`
		Size     int64    `json:"size"`
	}
	var archives []archive
	var current archive
	for _, asset := range assets {
		var size int64
		if asset.Exif != nil {
			size = asset.Exif.FileSize
		}
		if size <= 0 && asset.OriginalPath != "" {
			if fi, err := os.Stat(asset.OriginalPath); err == nil {
				size = fi.Size()
			}
		}
		if current.Size+size > limit && len(current.AssetIDs) > 0 {
			archives = append(archives, current)
			current = archive{}
		}
		current.AssetIDs = append(current.AssetIDs, asset.ID)
		current.Size += size
	}
	if len(current.AssetIDs) > 0 {
		archives = append(archives, current)
	}
	if archives == nil {
		archives = []archive{}
	}
	var total int64
	for _, arc := range archives {
		total += arc.Size
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"archives":  archives,
		"totalSize": total,
	})
}

// downloadArchive streams a zip of the requested assets.
func (s *Server) downloadArchive(w http.ResponseWriter, r *http.Request) {
	authCtx := caller(w, r)
	if authCtx == nil {
		return
	}
	var req struct {
		ArchiveName string   `json:"archiveName"`
		AssetIDs    []string `json:"assetIds"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.AssetIDs) == 0 {
		writeError(w, http.StatusBadRequest, "assetIds is required")
		return
	}
	var assets []*domain.Asset
	for _, id := range req.AssetIDs {
		if asset, err := s.app.Store.Assets().Get(r.Context(), id); err == nil && asset.OwnerID == authCtx.User.ID {
			assets = append(assets, asset)
		}
	}
	if len(assets) == 0 {
		writeError(w, http.StatusBadRequest, "No valid assets to download")
		return
	}
	name := req.ArchiveName
	if name == "" {
		name = "immich-download"
	}
	w.Header().Set("Content-Type", "application/zip")
	// mime.FormatMediaType escapes the client-supplied name safely.
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": name + ".zip"})
	w.Header().Set("Content-Disposition", disposition)
	w.WriteHeader(http.StatusOK)

	zw := zip.NewWriter(w)
	defer zw.Close()
	for _, asset := range assets {
		f, err := s.app.Storage.Open(asset.OriginalPath)
		if err != nil {
			continue
		}
		entry, err := zw.Create(filepath.Base(asset.OriginalFileName))
		if err != nil {
			f.Close()
			continue
		}
		io.Copy(entry, f)
		f.Close()
	}
}

// ---- map ----

type mapMarker struct {
	ID      string  `json:"id"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	City    string  `json:"city"`
	State   string  `json:"state"`
	Country string  `json:"country"`
}

func (s *Server) mapMarkers(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	assets, err := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID)
	if err != nil {
		s.storeError(w, err)
		return
	}
	out := []mapMarker{}
	for _, asset := range assets {
		if asset.DeletedAt != nil || asset.Exif == nil ||
			asset.Exif.Latitude == nil || asset.Exif.Longitude == nil {
			continue
		}
		e := asset.Exif
		out = append(out, mapMarker{
			ID: asset.ID, Lat: *e.Latitude, Lon: *e.Longitude,
			City: e.City, State: e.State, Country: e.Country,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// reverseGeocode would consult local geodata upstream; without it we
// answer an empty list so clients degrade gracefully.
func (s *Server) reverseGeocode(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []map[string]string{})
}

// ---- folder view ----
//
// Contract mirrors upstream ViewService: the tree comes from distinct
// directory paths of timeline-visible assets, and a folder query returns
// the direct children only (no deeper descendants), ordered by file
// name. Paths are normalized to forward slashes on every platform.

// folderEligible matches the upstream visibility filter: timeline
// visibility, not trashed, timestamps complete.
func folderEligible(a *domain.Asset) bool {
	return a.DeletedAt == nil &&
		a.Visibility == domain.VisibilityTimeline &&
		!a.FileCreatedAt.IsZero() &&
		!a.FileModifiedAt.IsZero() &&
		!a.LocalDateTime.IsZero()
}

// normFolderPath converts to forward slashes and strips outer slashes so
// "/upload/a/b" and "upload/a/b" compare equal.
func normFolderPath(p string) string {
	return strings.Trim(filepath.ToSlash(p), "/")
}

func (s *Server) folderView(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	// The web SDK sends ?path=; accept the legacy originalPath alias for
	// older consumers.
	prefix := r.URL.Query().Get("path")
	if prefix == "" {
		prefix = r.URL.Query().Get("originalPath")
	}
	base := normFolderPath(prefix)
	assets, err := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID)
	if err != nil {
		s.storeError(w, err)
		return
	}
	type named struct {
		name  string
		asset *domain.Asset
	}
	matches := []named{}
	for _, asset := range assets {
		if !folderEligible(asset) {
			continue
		}
		rel := normFolderPath(asset.OriginalPath)
		var rest string
		if base == "" {
			rest = rel
		} else if strings.HasPrefix(rel, base+"/") {
			rest = strings.TrimPrefix(rel, base+"/")
		} else {
			continue
		}
		if strings.Contains(rest, "/") { // deeper descendant
			continue
		}
		matches = append(matches, named{name: rest, asset: asset})
	}
	// Order by file name, the upstream regexp_replace basename sort.
	for i := 1; i < len(matches); i++ {
		for j := i; j > 0 && matches[j].name < matches[j-1].name; j-- {
			matches[j], matches[j-1] = matches[j-1], matches[j]
		}
	}
	out := []AssetResponse{}
	for _, m := range matches {
		out = append(out, s.assetResponse(r.Context(), m.asset, false))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) folderUniquePaths(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	assets, err := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID)
	if err != nil {
		s.storeError(w, err)
		return
	}
	seen := map[string]bool{}
	out := []string{}
	for _, asset := range assets {
		if !folderEligible(asset) {
			continue
		}
		slash := filepath.ToSlash(asset.OriginalPath)
		dir := strings.TrimRight(path.Dir(slash), "/")
		if dir == "." || dir == "" {
			continue
		}
		if !seen[dir] {
			seen[dir] = true
			out = append(out, dir)
		}
	}
	// Ascending, exactly like the upstream ORDER BY directoryPath.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	writeJSON(w, http.StatusOK, out)
}
