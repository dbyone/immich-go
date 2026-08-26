package api

import (
	"encoding/json"
	"net/http"
	"time"

	"immich-go/internal/crypto"
	"immich-go/internal/domain"
)

// MemoryResponse mirrors the upstream MemoryResponseDto.
type MemoryResponse struct {
	ID        string          `json:"id"`
	OwnerID   string          `json:"ownerId"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	Assets    []AssetResponse `json:"assets"`
	MemoryAt  ISOTime         `json:"memoryAt"`
	ShowAt    *ISOTime        `json:"showAt"`
	HideAt    *ISOTime        `json:"hideAt"`
	SeenAt    *ISOTime        `json:"seenAt"`
	IsSaved   bool            `json:"isSaved"`
	CreatedAt ISOTime         `json:"createdAt"`
	UpdatedAt ISOTime         `json:"updatedAt"`
	DeletedAt *ISOTime        `json:"deletedAt,omitempty"`
}

func (s *Server) memoryResponse(r *http.Request, m *domain.Memory) MemoryResponse {
	resp := MemoryResponse{
		ID:        m.ID,
		OwnerID:   m.OwnerID,
		Type:      m.Type,
		Data:      json.RawMessage(orDefaultJSON(m.Data, "{}")),
		Assets:    []AssetResponse{},
		MemoryAt:  ISOTime(m.MemoryAt),
		ShowAt:    isoTimePtr(m.ShowAt),
		HideAt:    isoTimePtr(m.HideAt),
		SeenAt:    isoTimePtr(m.SeenAt),
		IsSaved:   m.IsSaved,
		CreatedAt: ISOTime(m.CreatedAt),
		UpdatedAt: ISOTime(m.UpdatedAt),
		DeletedAt: isoTimePtr(m.DeletedAt),
	}
	for _, id := range m.AssetIDs {
		if asset, err := s.app.Store.Assets().Get(r.Context(), id); err == nil && asset.OwnerID == m.OwnerID {
			resp.Assets = append(resp.Assets, s.assetResponse(asset, false))
		}
	}
	return resp
}

func orDefaultJSON(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func (s *Server) listMemories(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	memories, err := s.app.Store.Memories().ListForOwner(r.Context(), a.User.ID)
	if err != nil {
		storeError(w, err)
		return
	}
	out := make([]MemoryResponse, 0, len(memories))
	for _, m := range memories {
		out = append(out, s.memoryResponse(r, m))
	}
	writeJSON(w, http.StatusOK, out)
}

type createMemoryRequest struct {
	AssetIDs []string        `json:"assetIds"`
	Data     json.RawMessage `json:"data"`
	Type     string          `json:"type"`
	MemoryAt string          `json:"memoryAt"`
	ShowAt   string          `json:"showAt"`
	HideAt   string          `json:"hideAt"`
	SeenAt   string          `json:"seenAt"`
	IsSaved  bool            `json:"isSaved"`
}

func (s *Server) createMemory(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req createMemoryRequest
	if err := decodeJSON(r, &req); err != nil || req.Type == "" {
		writeError(w, http.StatusBadRequest, "type is required")
		return
	}
	memoryAt, err := parseUploadTime2(req.MemoryAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid memoryAt")
		return
	}
	now := time.Now().UTC()
	m := &domain.Memory{
		ID:        crypto.NewUUID(),
		OwnerID:   a.User.ID,
		Type:      req.Type,
		Data:      orDefaultJSON(string(req.Data), "{}"),
		MemoryAt:  memoryAt,
		IsSaved:   req.IsSaved,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if t, err := parseUploadTime2(req.ShowAt); err == nil {
		m.ShowAt = &t
	}
	if t, err := parseUploadTime2(req.HideAt); err == nil {
		m.HideAt = &t
	}
	if t, err := parseUploadTime2(req.SeenAt); err == nil {
		m.SeenAt = &t
	}
	own := map[string]bool{}
	if assets, err := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID); err == nil {
		for _, asset := range assets {
			own[asset.ID] = true
		}
	}
	for _, id := range req.AssetIDs {
		if own[id] {
			m.AssetIDs = append(m.AssetIDs, id)
		}
	}
	if err := s.app.Store.Memories().Create(r.Context(), m); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.memoryResponse(r, m))
}

func parseUploadTime2(s string) (time.Time, error) {
	if s == "" {
		return time.Now().UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, errBadTime
}

func (s *Server) getMemory(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	m, err := s.app.Store.Memories().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || m.OwnerID != a.User.ID {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	writeJSON(w, http.StatusOK, s.memoryResponse(r, m))
}

type updateMemoryRequest struct {
	IsSaved  *bool   `json:"isSaved"`
	MemoryAt *string `json:"memoryAt"`
	SeenAt   *string `json:"seenAt"`
}

func (s *Server) updateMemory(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	m, err := s.app.Store.Memories().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || m.OwnerID != a.User.ID {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	var req updateMemoryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.IsSaved != nil {
		m.IsSaved = *req.IsSaved
	}
	if req.MemoryAt != nil {
		if t, err := parseUploadTime2(*req.MemoryAt); err == nil {
			m.MemoryAt = t
		}
	}
	if req.SeenAt != nil {
		if t, err := parseUploadTime2(*req.SeenAt); err == nil {
			m.SeenAt = &t
		} else {
			m.SeenAt = nil
		}
	}
	m.UpdatedAt = time.Now().UTC()
	if err := s.app.Store.Memories().Update(r.Context(), m); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.memoryResponse(r, m))
}

func (s *Server) deleteMemory(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	m, err := s.app.Store.Memories().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || m.OwnerID != a.User.ID {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	if err := s.app.Store.Memories().Delete(r.Context(), m.ID); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) memoryAssetsUpdate(add bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := caller(w, r)
		if a == nil {
			return
		}
		m, err := s.app.Store.Memories().Get(r.Context(), chiURLParam(r, "id"))
		if err != nil || m.OwnerID != a.User.ID {
			writeError(w, http.StatusNotFound, "Not found")
			return
		}
		var req struct {
			IDs []string `json:"ids"`
		}
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
		member := map[string]bool{}
		for _, id := range m.AssetIDs {
			member[id] = true
		}
		results := []BulkIDResponse{}
		for _, id := range req.IDs {
			res := BulkIDResponse{ID: id, Success: true}
			switch {
			case !own[id]:
				res.Success = false
				res.Error = "no_permission"
			case add && member[id]:
				res.Success = false
				res.Error = "duplicate"
			case !add && !member[id]:
				res.Success = false
				res.Error = "not_found"
			case add:
				m.AssetIDs = append(m.AssetIDs, id)
				member[id] = true
			default:
				kept := m.AssetIDs[:0]
				for _, existing := range m.AssetIDs {
					if existing != id {
						kept = append(kept, existing)
					}
				}
				m.AssetIDs = kept
			}
			results = append(results, res)
		}
		m.UpdatedAt = time.Now().UTC()
		if err := s.app.Store.Memories().Update(r.Context(), m); err != nil {
			storeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, results)
	}
}

func (s *Server) memoriesStatistics(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	memories, err := s.app.Store.Memories().ListForOwner(r.Context(), a.User.ID)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": len(memories)})
}
