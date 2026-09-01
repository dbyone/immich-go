package api

import (
	"net/http"
	"time"

	"immich-go/internal/auth"
	"immich-go/internal/crypto"
	"immich-go/internal/domain"
)

// --- album response assembly ---

func (s *Server) albumResponse(r *http.Request, al *domain.Album, withAssets bool) AlbumResponse {
	ctx := r.Context()
	resp := AlbumResponse{
		ID:                    al.ID,
		AlbumName:             al.AlbumName,
		AlbumThumbnailAssetID: al.AlbumThumbnailAssetID,
		AlbumUsers:            []AlbumUserResponse{},
		AssetCount:            len(al.AssetIDs),
		CreatedAt:             ISOTime(al.CreatedAt),
		UpdatedAt:             ISOTime(al.UpdatedAt),
		Description:           al.Description,
		HasSharedLink:         false,
		IsActivityEnabled:     al.IsActivityEnabled,
		Shared:                len(al.Users) > 0,
		Order:                 al.Order,
	}
	if resp.Order == "" {
		resp.Order = "asc"
	}

	// Upstream keeps the owner as an album_user row with role=owner
	// (unique partial index album_user_unique_owner); the web client reads
	// albumUsers[0].user.id as the owner test and computes "shared" as
	// albumUsers.length > 1. Mirror that shape: owner first, then sharees.
	if owner, err := s.app.Store.Users().Get(ctx, al.OwnerID); err == nil {
		resp.Owner = userResponsePtr(owner)
		resp.AlbumUsers = append(resp.AlbumUsers, AlbumUserResponse{Role: domain.AlbumRoleOwner, User: userResponse(owner)})
	}
	for _, au := range al.Users {
		if u, err := s.app.Store.Users().Get(ctx, au.UserID); err == nil {
			resp.AlbumUsers = append(resp.AlbumUsers, AlbumUserResponse{Role: au.Role, User: userResponse(u)})
		}
	}

	// startDate/endDate from member assets (ordered as stored).
	for _, id := range al.AssetIDs {
		asset, err := s.app.Store.Assets().Get(ctx, id)
		if err != nil {
			continue
		}
		if resp.StartDate == nil {
			resp.StartDate = isoTimePtr(&asset.FileCreatedAt)
		}
		resp.EndDate = isoTimePtr(&asset.FileCreatedAt)
		if withAssets {
			resp.Assets = append(resp.Assets, s.assetResponse(r.Context(), asset, false))
		}
	}
	if resp.EndDate != nil {
		resp.LastModifiedAssetTimestamp = resp.EndDate
	}
	return resp
}

func (s *Server) canSeeAlbum(r *http.Request, al *domain.Album) bool {
	a := auth.FromRequest(r)
	if a == nil {
		return false
	}
	if al.OwnerID == a.User.ID {
		return true
	}
	for _, u := range al.Users {
		if u.UserID == a.User.ID {
			return true
		}
	}
	return false
}

// canEditAlbum: the owner or an editor may mutate album contents;
// viewers are read-only (upstream AlbumUserRole semantics).
func (s *Server) canEditAlbum(r *http.Request, al *domain.Album) bool {
	a := auth.FromRequest(r)
	if a == nil {
		return false
	}
	if al.OwnerID == a.User.ID {
		return true
	}
	for _, u := range al.Users {
		if u.UserID == a.User.ID && u.Role != domain.AlbumRoleViewer {
			return true
		}
	}
	return false
}

// --- albums ---

func (s *Server) listAlbums(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	// Upstream filters via query params; the web albums page asks for the
	// owned list and the shared-with-me list separately and renders both.
	// Returning unfiltered albums for each call duplicates every album
	// across the two lists and crashes the client (Svelte each_key_duplicate).
	q := r.URL.Query()
	isOwned := q.Get("isOwned") == "true"
	isShared := q.Get("isShared") == "true" || q.Get("shared") == "true"
	assetID := q.Get("assetId")

	albums, err := s.app.Store.Albums().List(r.Context())
	if err != nil {
		s.storeError(w, err)
		return
	}
	out := []AlbumResponse{}
	for _, al := range albums {
		if !s.canSeeAlbum(r, al) {
			continue
		}
		if isOwned && al.OwnerID != a.User.ID {
			continue
		}
		if isShared {
			shared := false
			for _, u := range al.Users {
				if u.UserID == a.User.ID {
					shared = true
					break
				}
			}
			if !shared {
				continue
			}
		}
		if assetID != "" {
			found := false
			for _, id := range al.AssetIDs {
				if id == assetID {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		out = append(out, s.albumResponse(r, al, false))
	}
	writeJSON(w, http.StatusOK, out)
}

type createAlbumRequest struct {
	AlbumName   string   `json:"albumName"`
	Description string   `json:"description"`
	AssetIDs    []string `json:"assetIds"`
	AlbumUsers  []struct {
		Role   string `json:"role"`
		UserID string `json:"userId"`
	} `json:"albumUsers"`
}

func (s *Server) createAlbum(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	if !s.requirePermission(w, r, "album.create") {
		return
	}
	// Upstream validates albumName as a plain z.string() — an empty name
	// is legal there (the web client creates albums with albumName: ""
	// from the quick-create flow and renders them as "unnamed album");
	// rejecting it broke album creation with a 400.
	var req createAlbumRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	now := time.Now().UTC()
	al := &domain.Album{
		ID:                crypto.NewUUID(),
		OwnerID:           a.User.ID,
		AlbumName:         req.AlbumName,
		Description:       req.Description,
		CreatedAt:         now,
		UpdatedAt:         now,
		IsActivityEnabled: true,
		Order:             "asc",
	}
	// Only own assets can be added.
	own := map[string]bool{}
	if assets, err := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID); err == nil {
		for _, asset := range assets {
			own[asset.ID] = true
		}
	}
	for _, id := range req.AssetIDs {
		if own[id] {
			al.AssetIDs = append(al.AssetIDs, id)
		}
	}
	// Upstream seeds the album cover from the first asset; without this the
	// web album list renders the no-thumbnail placeholder for every album.
	if len(al.AssetIDs) > 0 {
		al.AlbumThumbnailAssetID = &al.AssetIDs[0]
	}
	for _, au := range req.AlbumUsers {
		if au.UserID != "" {
			role := au.Role
			if role == "" {
				role = domain.AlbumRoleEditor
			}
			al.Users = append(al.Users, domain.AlbumUser{UserID: au.UserID, Role: role})
		}
	}
	if err := s.app.Store.Albums().Create(r.Context(), al); err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.albumResponse(r, al, false))
}

func (s *Server) getAlbum(w http.ResponseWriter, r *http.Request) {
	al, err := s.app.Store.Albums().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || !s.canSeeAlbum(r, al) {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	writeJSON(w, http.StatusOK, s.albumResponse(r, al, true))
}

type updateAlbumRequest struct {
	AlbumName             *string `json:"albumName"`
	Description           *string `json:"description"`
	AlbumThumbnailAssetID *string `json:"albumThumbnailAssetId"`
	IsActivityEnabled     *bool   `json:"isActivityEnabled"`
	Order                 *string `json:"order"`
}

func (s *Server) updateAlbum(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	al, err := s.app.Store.Albums().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || al.OwnerID != a.User.ID {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	var req updateAlbumRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.AlbumName != nil {
		al.AlbumName = *req.AlbumName
	}
	if req.Description != nil {
		al.Description = *req.Description
	}
	if req.AlbumThumbnailAssetID != nil {
		al.AlbumThumbnailAssetID = req.AlbumThumbnailAssetID
	}
	if req.IsActivityEnabled != nil {
		al.IsActivityEnabled = *req.IsActivityEnabled
	}
	if req.Order != nil {
		al.Order = *req.Order
	}
	al.UpdatedAt = time.Now().UTC()
	if err := s.app.Store.Albums().Update(r.Context(), al); err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.albumResponse(r, al, false))
}

func (s *Server) deleteAlbum(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	al, err := s.app.Store.Albums().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || al.OwnerID != a.User.ID {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	if err := s.app.Store.Albums().Delete(r.Context(), al.ID); err != nil {
		s.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type addAssetsRequest struct {
	AssetIDs []string `json:"assetIds"`
}

func (s *Server) addAssetsToAlbum(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	al, err := s.app.Store.Albums().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || !s.canSeeAlbum(r, al) {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	if !s.canEditAlbum(r, al) {
		writeError(w, http.StatusForbidden, "Viewers cannot modify this album")
		return
	}
	var req addAssetsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	own := map[string]bool{}
	if assets, err := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID); err == nil {
		for _, asset := range assets {
			own[asset.ID] = true
		}
	}
	results := []BulkIDResponse{}
	s.albumMu.Lock()
	al, err = s.app.Store.Albums().Get(r.Context(), al.ID)
	if err != nil {
		s.albumMu.Unlock()
		s.storeError(w, err)
		return
	}
	for _, id := range req.AssetIDs {
		res := BulkIDResponse{ID: id, Success: true}
		switch {
		case al.HasAsset(id):
			res.Success = false
			res.Error = "duplicate"
		case !own[id]:
			res.Success = false
			res.Error = "no_permission"
		default:
			al.AssetIDs = append(al.AssetIDs, id)
		}
		results = append(results, res)
	}
	// Seed the cover the first time assets land in the album (upstream
	// behavior); an explicit PATCH albumThumbnailAssetId still wins later.
	if al.AlbumThumbnailAssetID == nil && len(al.AssetIDs) > 0 {
		al.AlbumThumbnailAssetID = &al.AssetIDs[0]
	}
	al.UpdatedAt = time.Now().UTC()
	err = s.app.Store.Albums().Update(r.Context(), al)
	s.albumMu.Unlock()
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) removeAssetsFromAlbum(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	al, err := s.app.Store.Albums().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || !s.canSeeAlbum(r, al) {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	if !s.canEditAlbum(r, al) {
		writeError(w, http.StatusForbidden, "Viewers cannot modify this album")
		return
	}
	var req addAssetsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	remove := map[string]bool{}
	for _, id := range req.AssetIDs {
		remove[id] = true
	}
	results := []BulkIDResponse{}
	s.albumMu.Lock()
	al, err = s.app.Store.Albums().Get(r.Context(), al.ID)
	if err != nil {
		s.albumMu.Unlock()
		s.storeError(w, err)
		return
	}
	for _, id := range req.AssetIDs {
		res := BulkIDResponse{ID: id, Success: true}
		if !al.HasAsset(id) {
			res.Success = false
			res.Error = "not_found"
		}
		results = append(results, res)
	}
	kept := al.AssetIDs[:0]
	for _, id := range al.AssetIDs {
		if !remove[id] {
			kept = append(kept, id)
		}
	}
	al.AssetIDs = kept
	// Keep the cover valid when its asset leaves the album: fall back to
	// the first remaining asset, or clear it for an empty album.
	if al.AlbumThumbnailAssetID != nil && !al.HasAsset(*al.AlbumThumbnailAssetID) {
		if len(al.AssetIDs) > 0 {
			al.AlbumThumbnailAssetID = &al.AssetIDs[0]
		} else {
			al.AlbumThumbnailAssetID = nil
		}
	}
	al.UpdatedAt = time.Now().UTC()
	err = s.app.Store.Albums().Update(r.Context(), al)
	s.albumMu.Unlock()
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, results)
}

type addAssetsToAlbumsRequest struct {
	AlbumIDs []string `json:"albumIds"`
	AssetIDs []string `json:"assetIds"`
}

// addAssetsToAlbums is the multi-album variant used by bulk actions.
func (s *Server) addAssetsToAlbums(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req addAssetsToAlbumsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	// Only the caller's own assets may be attached — the same rule the
	// single-album endpoint enforces.
	own := map[string]bool{}
	if assets, err := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID); err == nil {
		for _, asset := range assets {
			own[asset.ID] = true
		}
	}
	results := []AlbumsAddAssetsResponse{}
	for _, albumID := range req.AlbumIDs {
		res := AlbumsAddAssetsResponse{Success: true}
		al, err := s.app.Store.Albums().Get(r.Context(), albumID)
		if err != nil || !s.canSeeAlbum(r, al) {
			res.Success = false
			res.Error = "not_found"
		} else if !s.canEditAlbum(r, al) {
			res.Success = false
			res.Error = "no_permission"
		} else {
			s.albumMu.Lock()
			al, err = s.app.Store.Albums().Get(r.Context(), albumID)
			if err != nil {
				s.albumMu.Unlock()
				res.Success = false
				res.Error = "unknown"
			} else {
				for _, assetID := range req.AssetIDs {
					if !al.HasAsset(assetID) && own[assetID] {
						al.AssetIDs = append(al.AssetIDs, assetID)
					}
				}
				al.UpdatedAt = time.Now().UTC()
				if err := s.app.Store.Albums().Update(r.Context(), al); err != nil {
					res.Success = false
					res.Error = "unknown"
				}
			}
			s.albumMu.Unlock()
		}
		results = append(results, res)
	}
	writeJSON(w, http.StatusOK, results)
}

type addUsersRequest struct {
	AlbumUsers []struct {
		Role   string `json:"role"`
		UserID string `json:"userId"`
	} `json:"albumUsers"`
}

func (s *Server) addUsersToAlbum(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	al, err := s.app.Store.Albums().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || al.OwnerID != a.User.ID {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	var req addUsersRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	for _, au := range req.AlbumUsers {
		if au.UserID == "" {
			continue
		}
		role := au.Role
		if role == "" {
			role = domain.AlbumRoleEditor
		}
		duplicate := false
		for _, existing := range al.Users {
			if existing.UserID == au.UserID {
				duplicate = true
				break
			}
		}
		if !duplicate {
			al.Users = append(al.Users, domain.AlbumUser{UserID: au.UserID, Role: role})
		}
	}
	al.UpdatedAt = time.Now().UTC()
	if err := s.app.Store.Albums().Update(r.Context(), al); err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.albumResponse(r, al, false))
}

func (s *Server) removeUserFromAlbum(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	al, err := s.app.Store.Albums().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || al.OwnerID != a.User.ID {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	userID := chiURLParam(r, "userId")
	kept := al.Users[:0]
	for _, u := range al.Users {
		if u.UserID != userID {
			kept = append(kept, u)
		}
	}
	al.Users = kept
	al.UpdatedAt = time.Now().UTC()
	if err := s.app.Store.Albums().Update(r.Context(), al); err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.albumResponse(r, al, false))
}

func (s *Server) albumStatistics(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	stats := AlbumStatisticsResponse{}
	if albums, err := s.app.Store.Albums().ListForOwner(r.Context(), a.User.ID); err == nil {
		stats.Owned = int64(len(albums))
		for _, al := range albums {
			if len(al.Users) > 0 {
				stats.Shared++
			} else {
				stats.NotShared++
			}
		}
	}
	writeJSON(w, http.StatusOK, stats)
}
