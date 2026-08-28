package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"immich-go/internal/crypto"
	"immich-go/internal/domain"
	"immich-go/internal/store"
)

func nowUTC() time.Time { return time.Now().UTC() }

// ---- tags ----
//
// Wire contract mirrors the upstream tag controller: hierarchical values
// joined with "/" (PUT /tags upserts whole paths), color is an optional
// hex string, bulk operations report the number of affected pairs.

type tagUpsertRequest struct {
	Tags []string `json:"tags"`
}

func (s *Server) listTags(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	tags, err := s.app.Store.Tags().ListForUser(r.Context(), a.User.ID)
	if err != nil {
		s.storeError(w, err)
		return
	}
	out := []TagResponse{}
	for _, t := range tags {
		out = append(out, tagResponse(t))
	}
	writeJSON(w, http.StatusOK, out)
}

type tagCreateRequest struct {
	Name     string  `json:"name"`
	Color    *string `json:"color"`
	ParentID *string `json:"parentId"`
}

func (s *Server) createTag(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req tagCreateRequest
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.Contains(req.Name, "/") {
		writeError(w, http.StatusBadRequest, "name must not contain '/'")
		return
	}
	value := req.Name
	if req.ParentID != nil && *req.ParentID != "" {
		parent, err := s.app.Store.Tags().Get(r.Context(), *req.ParentID)
		if err != nil || parent.UserID != a.User.ID {
			writeError(w, http.StatusBadRequest, "invalid parentId")
			return
		}
		value = parent.Value + "/" + req.Name
	}
	// The (user, value) pair is unique; a repeat create is a conflict,
	// not a 500 from the storage layer's unique index.
	if _, err := s.app.Store.Tags().GetByValue(r.Context(), a.User.ID, value); err == nil {
		writeError(w, http.StatusConflict, "tag already exists")
		return
	} else if err != store.ErrNotFound {
		s.storeError(w, err)
		return
	}
	tag := &domain.Tag{
		ID:        crypto.NewUUID(),
		UserID:    a.User.ID,
		Name:      req.Name,
		Value:     value,
		Color:     req.Color,
		ParentID:  req.ParentID,
		CreatedAt: nowUTC(),
	}
	if err := s.app.Store.Tags().Create(r.Context(), tag); err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, tagResponse(tag))
}

// upsertTags creates (or returns) every tag in the request, creating the
// missing path segments above each one — upstream's upsertTags helper.
func (s *Server) upsertTags(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req tagUpsertRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	out := []TagResponse{}
	for _, name := range req.Tags {
		tag, err := s.app.Store.Tags().UpsertValue(r.Context(), a.User.ID, name)
		if err != nil {
			s.storeError(w, err)
			return
		}
		out = append(out, tagResponse(tag))
	}
	writeJSON(w, http.StatusOK, out)
}

type tagBulkAssetsRequest struct {
	AssetIDs []string `json:"assetIds"`
	TagIDs   []string `json:"tagIds"`
}

func (s *Server) bulkTagAssets(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req tagBulkAssetsRequest
	if err := decodeJSON(r, &req); err != nil || len(req.AssetIDs) == 0 || len(req.TagIDs) == 0 {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	// Validate every id up front — upstream rejects the whole request on
	// unknown tags or foreign assets instead of silently skipping.
	for _, tagID := range req.TagIDs {
		tag, err := s.app.Store.Tags().Get(r.Context(), tagID)
		if err != nil || tag.UserID != a.User.ID {
			writeError(w, http.StatusBadRequest, "invalid tagId")
			return
		}
	}
	for _, assetID := range req.AssetIDs {
		asset, err := s.app.Store.Assets().Get(r.Context(), assetID)
		if err != nil || asset.OwnerID != a.User.ID {
			writeError(w, http.StatusBadRequest, "invalid assetId")
			return
		}
	}
	tagged := map[string]bool{}
	for _, tagID := range req.TagIDs {
		for _, assetID := range req.AssetIDs {
			if n, err := s.app.Store.Tags().Attach(r.Context(), tagID, []string{assetID}); err == nil && n > 0 {
				tagged[assetID] = true
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": len(tagged)})
}

func (s *Server) getTag(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	tag, err := s.ownedTag(w, r, a.User.ID)
	if err != nil {
		return
	}
	writeJSON(w, http.StatusOK, tagResponse(tag))
}

func (s *Server) ownedTag(w http.ResponseWriter, r *http.Request, userID string) (*domain.Tag, error) {
	tag, err := s.app.Store.Tags().Get(r.Context(), r.PathValue("id"))
	if err == store.ErrNotFound || (err == nil && tag.UserID != userID) {
		writeError(w, http.StatusNotFound, "tag not found")
		return nil, store.ErrNotFound
	}
	if err != nil {
		s.storeError(w, err)
		return nil, err
	}
	return tag, nil
}

type tagUpdateRequest struct {
	Name  string  `json:"name"`
	Color *string `json:"color"`
}

func (s *Server) updateTag(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	tag, err := s.ownedTag(w, r, a.User.ID)
	if err != nil {
		return
	}
	var req tagUpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Name != "" {
		if strings.Contains(req.Name, "/") {
			writeError(w, http.StatusBadRequest, "name must not contain '/'")
			return
		}
		tag.Name = req.Name
		if tag.ParentID != nil && *tag.ParentID != "" {
			parent, err := s.app.Store.Tags().Get(r.Context(), *tag.ParentID)
			if err == nil && parent.UserID == a.User.ID {
				tag.Value = parent.Value + "/" + tag.Name
			}
		} else {
			tag.Value = tag.Name
		}
		// Renaming a branch relabels the whole subtree, like upstream.
		if err := s.retagDescendants(r.Context(), a.User.ID, tag); err != nil {
			s.storeError(w, err)
			return
		}
	}
	if req.Color != nil {
		tag.Color = req.Color
	}
	if err := s.app.Store.Tags().Update(r.Context(), tag); err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tagResponse(tag))
}

// retagDescendants recomputes descendant values after a rename (parent
// value changes ripple down the path prefix).
func (s *Server) retagDescendants(ctx context.Context, userID string, tag *domain.Tag) error {
	all, err := s.app.Store.Tags().ListForUser(ctx, userID)
	if err != nil {
		return err
	}
	byParent := map[string][]*domain.Tag{}
	oldValue := ""
	for _, t := range all {
		if t.ParentID != nil {
			byParent[*t.ParentID] = append(byParent[*t.ParentID], t)
		}
		if t.ID == tag.ID {
			oldValue = t.Value
		}
	}
	if oldValue == "" || oldValue == tag.Value {
		return nil
	}
	var walk func(parent *domain.Tag) error
	walk = func(parent *domain.Tag) error {
		for _, child := range byParent[parent.ID] {
			if len(child.Value) > len(oldValue) && strings.HasPrefix(child.Value, oldValue+"/") {
				child.Value = tag.Value + strings.TrimPrefix(child.Value, oldValue)
				if err := s.app.Store.Tags().Update(ctx, child); err != nil {
					return err
				}
			}
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(tag)
}

func (s *Server) deleteTag(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	if _, err := s.ownedTag(w, r, a.User.ID); err != nil {
		return
	}
	if err := s.app.Store.Tags().Delete(r.Context(), r.PathValue("id")); err != nil {
		s.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type tagAssetsRequest struct {
	AssetIDs []string `json:"assetIds"`
}

func (s *Server) tagAssets(w http.ResponseWriter, r *http.Request) {
	s.attachTagAssets(w, r, true)
}

func (s *Server) untagAssets(w http.ResponseWriter, r *http.Request) {
	s.attachTagAssets(w, r, false)
}

func (s *Server) attachTagAssets(w http.ResponseWriter, r *http.Request, attach bool) {
	a := caller(w, r)
	if a == nil {
		return
	}
	tag, err := s.ownedTag(w, r, a.User.ID)
	if err != nil {
		return
	}
	var req tagAssetsRequest
	if err := decodeJSON(r, &req); err != nil || len(req.AssetIDs) == 0 {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var count int
	for _, assetID := range req.AssetIDs {
		asset, err := s.app.Store.Assets().Get(r.Context(), assetID)
		if err != nil || asset.OwnerID != a.User.ID {
			continue
		}
		var n int
		if attach {
			n, err = s.app.Store.Tags().Attach(r.Context(), tag.ID, []string{assetID})
		} else {
			n, err = s.app.Store.Tags().Detach(r.Context(), tag.ID, []string{assetID})
		}
		if err != nil {
			s.storeError(w, err)
			return
		}
		if n > 0 {
			count++
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
}
