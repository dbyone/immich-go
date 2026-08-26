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
)

// ---- duplicates resolution ----

func (s *Server) resolveDuplicates(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req struct {
		Groups []struct {
			DuplicateID    string   `json:"duplicateId"`
			KeepAssetIDs   []string `json:"keepAssetIds"`
			TrashAssetIDs  []string `json:"trashAssetIds"`
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
			asset, err := s.app.Store.Assets().Get(r.Context(), id)
			if err != nil || asset.OwnerID != a.User.ID {
				res.Success = false
				res.Error = "not_found"
			} else {
				asset.DeletedAt = &now
				asset.UpdatedAt = now
				if keep[id] {
					// A kept asset must not be trashed; ignore the overlap.
					res.Error = "duplicate"
				} else if err := s.app.Store.Assets().Update(r.Context(), asset); err != nil {
					res.Success = false
					res.Error = "unknown"
				}
			}
			results = append(results, res)
		}
		// Dissolve the group for everyone once resolved.
		assets, _ := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID)
		for _, asset := range assets {
			if asset.DuplicateID != nil && *asset.DuplicateID == g.DuplicateID {
				asset.DuplicateID = nil
				asset.UpdatedAt = now
				_ = s.app.Store.Assets().Update(r.Context(), asset)
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
			asset.DuplicateID = nil
			asset.UpdatedAt = now
			_ = s.app.Store.Assets().Update(r.Context(), asset)
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
		storeError(w, err)
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

func (s *Server) folderView(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	prefix := r.URL.Query().Get("originalPath")
	assets, err := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID)
	if err != nil {
		storeError(w, err)
		return
	}
	out := []AssetResponse{}
	for _, asset := range assets {
		if asset.DeletedAt == nil && strings.HasPrefix(filepath.ToSlash(asset.OriginalPath), prefix) {
			out = append(out, s.assetResponse(asset, false))
		}
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
		storeError(w, err)
		return
	}
	seen := map[string]bool{}
	var out []string
	for _, asset := range assets {
		if asset.DeletedAt != nil {
			continue
		}
		// Normalize to forward slashes so the paths round-trip through the
		// folder view endpoint on every platform.
		dir := path.Dir(filepath.ToSlash(asset.OriginalPath))
		if !seen[dir] {
			seen[dir] = true
			out = append(out, dir)
		}
	}
	if out == nil {
		out = []string{}
	}
	writeJSON(w, http.StatusOK, out)
}

