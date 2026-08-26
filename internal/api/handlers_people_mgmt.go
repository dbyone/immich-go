package api

import (
	"net/http"
	"time"

	"immich-go/internal/crypto"
	"immich-go/internal/media"
	"immich-go/internal/vectordb"
)

func isoString(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// PersonDetail is the PersonResponseDto shape (full, per spec).
type PersonDetail struct {
	BirthDate     *string `json:"birthDate"`
	Color         string  `json:"color"`
	ID            string  `json:"id"`
	IsFavorite    bool    `json:"isFavorite"`
	IsHidden      bool    `json:"isHidden"`
	Name          string  `json:"name"`
	ThumbnailPath string  `json:"thumbnailPath"`
	UpdatedAt     string  `json:"updatedAt"`
}

func personDetail(p *vectordb.Person) PersonDetail {
	var birth *string
	if p.BirthDate != "" {
		b := p.BirthDate
		birth = &b
	}
	return PersonDetail{
		BirthDate:  birth,
		Color:      p.Color,
		ID:         p.ID,
		IsFavorite: p.IsFavorite,
		IsHidden:   p.IsHidden,
		Name:       p.Name,
		ThumbnailPath: "/api/people/" + p.ID + "/thumbnail",
		UpdatedAt:  isoString(p.UpdatedAt),
	}
}

// loadOwnPerson resolves the path person, enforcing ownership; nil means
// the response was already written.
func (s *Server) loadOwnPerson(w http.ResponseWriter, r *http.Request, perm string) *vectordb.Person {
	a := caller(w, r)
	if a == nil {
		return nil
	}
	if !s.requirePermission(w, r, perm) {
		return nil
	}
	p, err := s.app.Vectors.GetPerson(r.Context(), chiURLParam(r, "id"))
	if err != nil || p.OwnerID != a.User.ID {
		writeError(w, http.StatusNotFound, "Person not found")
		return nil
	}
	return p
}

func (s *Server) createPerson(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req struct {
		BirthDate  *string `json:"birthDate"`
		Color      string  `json:"color"`
		IsFavorite bool    `json:"isFavorite"`
		IsHidden   bool    `json:"isHidden"`
		Name       string  `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	p := &vectordb.Person{
		ID: crypto.NewUUID(), OwnerID: a.User.ID, Name: req.Name,
		Color: req.Color, IsFavorite: req.IsFavorite, IsHidden: req.IsHidden,
	}
	if req.BirthDate != nil {
		p.BirthDate = *req.BirthDate
	}
	if err := s.app.Vectors.CreatePerson(r.Context(), p); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, personDetail(p))
}

func (s *Server) getPersonDetail(w http.ResponseWriter, r *http.Request) {
	p := s.loadOwnPerson(w, r, "person.read")
	if p == nil {
		return
	}
	writeJSON(w, http.StatusOK, personDetail(p))
}

func (s *Server) updatePerson(w http.ResponseWriter, r *http.Request) {
	p := s.loadOwnPerson(w, r, "person.update")
	if p == nil {
		return
	}
	var req struct {
		BirthDate          *string `json:"birthDate"`
		Color              *string `json:"color"`
		FeatureFaceAssetID *string `json:"featureFaceAssetId"`
		IsFavorite         *bool   `json:"isFavorite"`
		IsHidden           *bool   `json:"isHidden"`
		Name               *string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.BirthDate != nil {
		p.BirthDate = *req.BirthDate
	}
	if req.Color != nil {
		p.Color = *req.Color
	}
	if req.FeatureFaceAssetID != nil && *req.FeatureFaceAssetID != "" {
		p.ThumbnailAssetID = *req.FeatureFaceAssetID
	}
	if req.IsFavorite != nil {
		p.IsFavorite = *req.IsFavorite
	}
	if req.IsHidden != nil {
		p.IsHidden = *req.IsHidden
	}
	if req.Name != nil {
		p.Name = *req.Name
	}
	if err := s.app.Vectors.UpdatePerson(r.Context(), p); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, personDetail(p))
}

func (s *Server) deletePerson(w http.ResponseWriter, r *http.Request) {
	p := s.loadOwnPerson(w, r, "person.delete")
	if p == nil {
		return
	}
	if err := s.app.Vectors.DeletePersons(r.Context(), p.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) updatePeopleBulk(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req struct {
		People []struct {
			ID                 string  `json:"id"`
			Name               *string `json:"name"`
			IsHidden           *bool   `json:"isHidden"`
			IsFavorite         *bool   `json:"isFavorite"`
			BirthDate          *string `json:"birthDate"`
			Color              *string `json:"color"`
			FeatureFaceAssetID *string `json:"featureFaceAssetId"`
		} `json:"people"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	results := []BulkIDResponse{}
	for _, item := range req.People {
		res := BulkIDResponse{ID: item.ID, Success: true}
		p, err := s.app.Vectors.GetPerson(r.Context(), item.ID)
		if err != nil || p.OwnerID != a.User.ID {
			res.Success = false
			res.Error = "not_found"
		} else {
			if item.Name != nil {
				p.Name = *item.Name
			}
			if item.IsHidden != nil {
				p.IsHidden = *item.IsHidden
			}
			if item.IsFavorite != nil {
				p.IsFavorite = *item.IsFavorite
			}
			if item.BirthDate != nil {
				p.BirthDate = *item.BirthDate
			}
			if item.Color != nil {
				p.Color = *item.Color
			}
			if item.FeatureFaceAssetID != nil && *item.FeatureFaceAssetID != "" {
				p.ThumbnailAssetID = *item.FeatureFaceAssetID
			}
			if err := s.app.Vectors.UpdatePerson(r.Context(), p); err != nil {
				res.Success = false
				res.Error = "unknown"
			}
		}
		results = append(results, res)
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) deletePeopleBulk(w http.ResponseWriter, r *http.Request) {
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
	for _, id := range req.IDs {
		if p, err := s.app.Vectors.GetPerson(r.Context(), id); err == nil && p.OwnerID == a.User.ID {
			_ = s.app.Vectors.DeletePersons(r.Context(), id)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) mergePerson(w http.ResponseWriter, r *http.Request) {
	target := s.loadOwnPerson(w, r, "person.update")
	if target == nil {
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	results := []BulkIDResponse{}
	for _, id := range req.IDs {
		res := BulkIDResponse{ID: id, Success: true}
		if id == target.ID {
			res.Success = false
			res.Error = "duplicate"
		} else {
			src, err := s.app.Vectors.GetPerson(r.Context(), id)
			if err != nil || src.OwnerID != target.OwnerID {
				res.Success = false
				res.Error = "not_found"
			} else {
				if err := s.app.Vectors.MergePersons(r.Context(), target.ID, []string{id}); err != nil {
					res.Success = false
					res.Error = "unknown"
				}
			}
		}
		results = append(results, res)
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) reassignFaces(w http.ResponseWriter, r *http.Request) {
	p := s.loadOwnPerson(w, r, "person.update")
	if p == nil {
		return
	}
	var req struct {
		Data []struct {
			AssetID  string  `json:"assetId"`
			PersonID *string `json:"personId"`
		} `json:"data"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	entries := make([]vectordb.ReassignEntry, 0, len(req.Data))
	for _, d := range req.Data {
		dest := ""
		if d.PersonID != nil {
			dest = *d.PersonID
		}
		entries = append(entries, vectordb.ReassignEntry{AssetID: d.AssetID, PersonID: dest})
	}
	if err := s.app.Vectors.ReassignFaces(r.Context(), p.ID, entries); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, err := s.app.Vectors.GetPerson(r.Context(), p.ID)
	if err != nil {
		updated = p
	}
	writeJSON(w, http.StatusOK, []PersonDetail{personDetail(updated)})
}

func (s *Server) personStatistics(w http.ResponseWriter, r *http.Request) {
	p := s.loadOwnPerson(w, r, "person.read")
	if p == nil {
		return
	}
	assets, err := s.app.Vectors.PersonStats(r.Context(), p.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"assets": assets})
}

// personThumbnail crops the person's representative face out of the
// source image.
func (s *Server) personThumbnail(w http.ResponseWriter, r *http.Request) {
	p := s.loadOwnPerson(w, r, "person.read")
	if p == nil {
		return
	}
	face, err := s.app.Vectors.PersonFace(r.Context(), p.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "No face available")
		return
	}
	asset, err := s.app.Store.Assets().Get(r.Context(), face.AssetID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Source asset missing")
		return
	}
	jpegData, err := media.CropFace(asset.OriginalPath, face.Box, media.ThumbnailMax)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.WriteHeader(http.StatusOK)
	w.Write(jpegData)
}
