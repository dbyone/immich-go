package api

import (
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"time"

	"immich-go/internal/crypto"
	"immich-go/internal/domain"
)

// ---- search long tail ----

type exploreItem struct {
	Value string        `json:"value"`
	Data  AssetResponse `json:"data"`
}

type exploreGroup struct {
	FieldName string        `json:"fieldName"`
	Items     []exploreItem `json:"items"`
}

// searchExplore groups the owner's assets by EXIF city (fallback country),
// one cover asset per bucket.
func (s *Server) searchExplore(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	assets, err := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID)
	if err != nil {
		s.storeError(w, err)
		return
	}
	covers := map[string]*domain.Asset{}
	fieldName := "exifInfo.city"
	order := []string{}
	for i, asset := range assets {
		if asset.DeletedAt != nil || asset.Exif == nil {
			continue
		}
		key := asset.Exif.City
		if key == "" && asset.Exif.Country != "" {
			key = asset.Exif.Country
			fieldName = "exifInfo.country"
		}
		if key == "" {
			continue
		}
		if _, ok := covers[key]; !ok {
			covers[key] = assets[i]
			order = append(order, key)
		}
	}
	group := exploreGroup{FieldName: fieldName, Items: []exploreItem{}}
	for _, key := range order {
		group.Items = append(group.Items, exploreItem{
			Value: key,
			Data:  s.assetResponse(r.Context(), covers[key], false),
		})
	}
	writeJSON(w, http.StatusOK, []exploreGroup{group})
}

// searchRandom returns a random sample matching the metadata filters.
func (s *Server) searchRandom(w http.ResponseWriter, r *http.Request) {
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
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	rng.Shuffle(len(matches), func(i, j int) { matches[i], matches[j] = matches[j], matches[i] })
	_, size := pageParams(req.Page, req.Size)
	if len(matches) > size {
		matches = matches[:size]
	}
	out := make([]AssetResponse, 0, len(matches))
	for _, asset := range matches {
		out = append(out, s.assetResponse(r.Context(), asset, false))
	}
	writeJSON(w, http.StatusOK, out)
}

// searchStatistics counts assets matching the filters.
func (s *Server) searchStatistics(w http.ResponseWriter, r *http.Request) {
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
	stats := AssetStatsResponse{Total: int64(len(matches))}
	for _, asset := range matches {
		if asset.Type == domain.AssetVideo {
			stats.Videos++
		} else {
			stats.Images++
		}
	}
	writeJSON(w, http.StatusOK, stats)
}

// searchCities returns one cover asset per distinct EXIF city.
func (s *Server) searchCities(w http.ResponseWriter, r *http.Request) {
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
	out := []AssetResponse{}
	for i, asset := range assets {
		if asset.DeletedAt != nil || asset.Exif == nil || asset.Exif.City == "" {
			continue
		}
		if seen[asset.Exif.City] {
			continue
		}
		seen[asset.Exif.City] = true
		out = append(out, s.assetResponse(r.Context(), assets[i], false))
	}
	writeJSON(w, http.StatusOK, out)
}

type placeResponse struct {
	Admin1Name string   `json:"admin1name"` // state
	Admin2Name string   `json:"admin2name"` // city
	Latitude   *float64 `json:"latitude"`
	Longitude  *float64 `json:"longitude"`
	Name       string   `json:"name"`
}

// searchPlaces derives distinct places from asset EXIF (we ship no local
// geodata database; known coordinates act as the place anchors).
func (s *Server) searchPlaces(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	q := strings.ToLower(r.URL.Query().Get("name"))
	assets, _ := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID)
	byCity := map[string]*placeResponse{}
	var order []string
	for _, asset := range assets {
		if asset.DeletedAt != nil || asset.Exif == nil || asset.Exif.City == "" {
			continue
		}
		e := asset.Exif
		if _, ok := byCity[e.City]; !ok {
			byCity[e.City] = &placeResponse{
				Admin1Name: e.State, Admin2Name: e.City,
				Latitude: e.Latitude, Longitude: e.Longitude, Name: e.City,
			}
			order = append(order, e.City)
			continue
		}
		// Prefer entries carrying coordinates.
		if p := byCity[e.City]; p.Latitude == nil && e.Latitude != nil {
			p.Latitude, p.Longitude = e.Latitude, e.Longitude
		}
	}
	out := []placeResponse{}
	for _, city := range order {
		p := byCity[city]
		if q == "" || strings.Contains(strings.ToLower(p.Name), q) ||
			strings.Contains(strings.ToLower(p.Admin1Name), q) {
			out = append(out, *p)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// searchPerson finds people by name substring.
func (s *Server) searchPerson(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	q := strings.ToLower(r.URL.Query().Get("name"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	persons, err := s.app.Vectors.ListPersons(r.Context(), a.User.ID)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	out := []PersonDetail{}
	for _, p := range persons {
		if strings.Contains(strings.ToLower(p.Name), q) {
			out = append(out, personDetail(&p))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// searchLargeAssets returns assets above a size threshold (default 100MB).
func (s *Server) searchLargeAssets(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	threshold := int64(100 << 20)
	if v := r.URL.Query().Get("threshold"); v != "" {
		if parsed, err := parseToInt64(v); err == nil && parsed > 0 {
			threshold = parsed
		}
	}
	assets, _ := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID)
	out := []AssetResponse{}
	for _, asset := range assets {
		if asset.DeletedAt != nil || asset.Exif == nil || asset.Exif.FileSize < threshold {
			continue
		}
		out = append(out, s.assetResponse(r.Context(), asset, false))
	}
	writeJSON(w, http.StatusOK, out)
}

// searchSuggestions returns distinct values for a search field
// (?type=country|state|city|make|model|lens-model|camera-model).
func (s *Server) searchSuggestions(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	typ := r.URL.Query().Get("type")
	assets, _ := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID)
	seen := map[string]bool{}
	var out []string
	for _, asset := range assets {
		if asset.DeletedAt != nil || asset.Exif == nil {
			continue
		}
		e := asset.Exif
		var value string
		switch typ {
		case "country":
			value = e.Country
		case "state":
			value = e.State
		case "city":
			value = e.City
		case "make":
			value = e.Make
		case "model":
			value = e.Model
		case "lens-model":
			value = e.LensModel
		default:
			writeError(w, http.StatusBadRequest, "invalid suggestion type")
			return
		}
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	if out == nil {
		out = []string{}
	}
	writeJSON(w, http.StatusOK, out)
}

func parseToInt64(s string) (int64, error) {
	var v int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errBadTime
		}
		v = v*10 + int64(c-'0')
	}
	return v, nil
}

// ---- sessions: create / update / lock ----

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req struct {
		DeviceOS   string `json:"deviceOS"`
		DeviceType string `json:"deviceType"`
		Duration   int64  `json:"duration"` // seconds
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	token := crypto.RandomToken()
	now := time.Now().UTC()
	sess := &domain.Session{
		ID:         crypto.NewUUID(),
		TokenHash:  crypto.HashToken(token),
		UserID:     a.User.ID,
		DeviceOS:   req.DeviceOS,
		DeviceType: req.DeviceType,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if req.Duration > 0 {
		expires := now.Add(time.Duration(req.Duration) * time.Second)
		sess.ExpiresAt = &expires
	}
	if err := s.app.Store.Sessions().Create(r.Context(), sess); err != nil {
		s.storeError(w, err)
		return
	}
	resp := sessionResponse(sess, false)
	resp.Token = token
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) updateSession(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	sess, err := s.app.Store.Sessions().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || sess.UserID != a.User.ID {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	var req struct {
		IsPendingSyncReset *bool `json:"isPendingSyncReset"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	// isPendingSyncReset has no backing behavior yet; the flag is accepted.
	current := a.Session != nil && sess.ID == a.Session.ID
	writeJSON(w, http.StatusOK, sessionResponse(sess, current))
}

func (s *Server) lockSession(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	sess, err := s.app.Store.Sessions().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || sess.UserID != a.User.ID {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	// Elevation locking requires PIN support (not implemented); accepted
	// as a no-op so clients can call it.
	w.WriteHeader(http.StatusNoContent)
}

// ---- stacks ----

type StackResponse struct {
	ID             string          `json:"id"`
	PrimaryAssetID string          `json:"primaryAssetId"`
	AssetCount     int             `json:"assetCount"`
	Assets         []AssetResponse `json:"assets"`
}

func (s *Server) stackResponse(r *http.Request, st *domain.Stack) StackResponse {
	resp := StackResponse{
		ID:             st.ID,
		PrimaryAssetID: st.PrimaryAssetID,
		AssetCount:     len(st.AssetIDs),
		Assets:         []AssetResponse{},
	}
	for _, id := range st.AssetIDs {
		if asset, err := s.app.Store.Assets().Get(r.Context(), id); err == nil {
			resp.Assets = append(resp.Assets, s.assetResponse(r.Context(), asset, false))
		}
	}
	return resp
}

func (s *Server) listStacks(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	stacks, err := s.app.Store.Stacks().ListForOwner(r.Context(), a.User.ID)
	if err != nil {
		s.storeError(w, err)
		return
	}
	out := make([]StackResponse, 0, len(stacks))
	for _, st := range stacks {
		out = append(out, s.stackResponse(r, st))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createStack(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req struct {
		AssetIDs []string `json:"assetIds"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.AssetIDs) < 2 {
		writeError(w, http.StatusBadRequest, "at least two assetIds are required")
		return
	}
	own := map[string]bool{}
	if assets, err := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID); err == nil {
		for _, asset := range assets {
			own[asset.ID] = true
		}
	}
	st := &domain.Stack{
		ID:             crypto.NewUUID(),
		OwnerID:        a.User.ID,
		PrimaryAssetID: req.AssetIDs[0],
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	for _, id := range req.AssetIDs {
		if own[id] {
			st.AssetIDs = append(st.AssetIDs, id)
		}
	}
	if len(st.AssetIDs) < 2 {
		writeError(w, http.StatusBadRequest, "at least two owned assetIds are required")
		return
	}
	if err := s.app.Store.Stacks().Create(r.Context(), st); err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.stackResponse(r, st))
}

func (s *Server) deleteStacksBulk(w http.ResponseWriter, r *http.Request) {
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
		if st, err := s.app.Store.Stacks().Get(r.Context(), id); err == nil && st.OwnerID == a.User.ID {
			_ = s.app.Store.Stacks().Delete(r.Context(), id)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getStack(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	st, err := s.app.Store.Stacks().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || st.OwnerID != a.User.ID {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	writeJSON(w, http.StatusOK, s.stackResponse(r, st))
}

func (s *Server) updateStack(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	st, err := s.app.Store.Stacks().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || st.OwnerID != a.User.ID {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	var req struct {
		PrimaryAssetID string `json:"primaryAssetId"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.PrimaryAssetID != "" {
		st.PrimaryAssetID = req.PrimaryAssetID
	}
	st.UpdatedAt = time.Now().UTC()
	if err := s.app.Store.Stacks().Update(r.Context(), st); err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.stackResponse(r, st))
}

func (s *Server) deleteStack(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	st, err := s.app.Store.Stacks().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || st.OwnerID != a.User.ID {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	if err := s.app.Store.Stacks().Delete(r.Context(), st.ID); err != nil {
		s.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeStackAsset(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	st, err := s.app.Store.Stacks().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || st.OwnerID != a.User.ID {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	assetID := chiURLParam(r, "assetId")
	kept := st.AssetIDs[:0]
	found := false
	for _, id := range st.AssetIDs {
		if id != assetID {
			kept = append(kept, id)
		} else {
			found = true
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "Asset not in stack")
		return
	}
	if len(kept) < 2 {
		_ = s.app.Store.Stacks().Delete(r.Context(), st.ID)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if st.PrimaryAssetID == assetID {
		st.PrimaryAssetID = kept[0]
	}
	st.AssetIDs = kept
	st.UpdatedAt = time.Now().UTC()
	if err := s.app.Store.Stacks().Update(r.Context(), st); err != nil {
		s.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- partners ----

type PartnerResponse struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Email            string `json:"email"`
	AvatarColor      string `json:"avatarColor"`
	ProfileImagePath string `json:"profileImagePath"`
	ProfileChangedAt string `json:"profileChangedAt"`
	InTimeline       bool   `json:"inTimeline"`
}

func (s *Server) partnerResponse(r *http.Request, p *domain.Partner) PartnerResponse {
	u, err := s.app.Store.Users().Get(r.Context(), p.UserID)
	if err != nil {
		return PartnerResponse{ID: p.UserID, InTimeline: p.InTimeline}
	}
	return PartnerResponse{
		ID:               u.ID,
		Name:             u.Name,
		Email:            u.Email,
		AvatarColor:      u.AvatarColor,
		ProfileImagePath: u.ProfileImagePath,
		ProfileChangedAt: isoString(u.UpdatedAt),
		InTimeline:       p.InTimeline,
	}
}

// listPartners returns partners sharing their library WITH the caller
// (shared-by direction); the web client's default view.
func (s *Server) listPartners(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	direction := r.URL.Query().Get("direction")
	var partners []*domain.Partner
	var err error
	if direction == "shared-by" {
		partners, err = s.app.Store.Partners().ListSharedBy(r.Context(), a.User.ID)
	} else {
		partners, err = s.app.Store.Partners().ListSharedWith(r.Context(), a.User.ID)
	}
	if err != nil {
		s.storeError(w, err)
		return
	}
	out := make([]PartnerResponse, 0, len(partners))
	// In both directions the response user is the other party: for
	// shared-with rows that is p.OwnerID, for shared-by rows p.UserID.
	for _, p := range partners {
		if direction == "shared-by" {
			out = append(out, s.partnerResponse(r, p))
			continue
		}
		clone := *p
		clone.UserID = p.OwnerID
		out = append(out, s.partnerResponse(r, &clone))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createPartner(w http.ResponseWriter, r *http.Request, targetUserID string) {
	a := caller(w, r)
	if a == nil {
		return
	}
	if targetUserID == "" {
		var req struct {
			SharedWithID string `json:"sharedWithId"`
		}
		if err := decodeJSON(r, &req); err != nil || req.SharedWithID == "" {
			writeError(w, http.StatusBadRequest, "sharedWithId is required")
			return
		}
		targetUserID = req.SharedWithID
	}
	if targetUserID == a.User.ID {
		writeError(w, http.StatusBadRequest, "Cannot share with yourself")
		return
	}
	if _, err := s.app.Store.Users().Get(r.Context(), targetUserID); err != nil {
		writeError(w, http.StatusBadRequest, "Unknown user")
		return
	}
	p := &domain.Partner{OwnerID: a.User.ID, UserID: targetUserID, InTimeline: true}
	if err := s.app.Store.Partners().Create(r.Context(), p); err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.partnerResponse(r, p))
}

func (s *Server) createPartnerBody(w http.ResponseWriter, r *http.Request) {
	s.createPartner(w, r, "")
}

func (s *Server) createPartnerByID(w http.ResponseWriter, r *http.Request) {
	s.createPartner(w, r, chiURLParam(r, "id"))
}

// partnerOf finds the share row where the path user is involved with the
// caller: either the caller shares with them, or they share with the caller.
func (s *Server) partnerOf(r *http.Request, userID, me string) *domain.Partner {
	rows, _ := s.app.Store.Partners().ListSharedBy(r.Context(), me)
	for _, p := range rows {
		if p.UserID == userID {
			return p
		}
	}
	rows, _ = s.app.Store.Partners().ListSharedWith(r.Context(), me)
	for _, p := range rows {
		if p.OwnerID == userID {
			return p
		}
	}
	return nil
}

func (s *Server) updatePartner(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	userID := chiURLParam(r, "id")
	p := s.partnerOf(r, userID, a.User.ID)
	if p == nil {
		writeError(w, http.StatusNotFound, "Partner not found")
		return
	}
	var req struct {
		InTimeline *bool `json:"inTimeline"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.InTimeline != nil {
		p.InTimeline = *req.InTimeline
	}
	p.UpdatedAt = time.Now().UTC()
	if err := s.app.Store.Partners().Update(r.Context(), p); err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.partnerResponse(r, p))
}

func (s *Server) removePartner(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	userID := chiURLParam(r, "id")
	p := s.partnerOf(r, userID, a.User.ID)
	if p == nil {
		writeError(w, http.StatusNotFound, "Partner not found")
		return
	}
	if err := s.app.Store.Partners().Delete(r.Context(), p.ID); err != nil {
		s.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
