package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"immich-go/internal/domain"
)

// Sync protocol, basic edition. Acks encode as "<Type>:<entityId>"; the
// stream serves a full snapshot per requested type unless the client has
// acknowledged that type before, in which case nothing is resent (the
// client can reset by deleting acks or sending reset=true).

type syncAckEntry struct {
	Ack  string `json:"ack"`
	Type string `json:"type"`
}

func (s *Server) getSyncAck(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	acks, err := s.app.Store.SyncAcks().List(r.Context(), a.User.ID)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	out := make([]syncAckEntry, 0, len(acks))
	for _, ack := range acks {
		out = append(out, syncAckEntry{Ack: ack.Ack, Type: ack.Type})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) sendSyncAck(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req struct {
		Acks []string `json:"acks"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var entries []domain.SyncAck
	for _, ack := range req.Acks {
		parts := strings.SplitN(ack, ":", 2)
		if len(parts) != 2 {
			continue
		}
		entries = append(entries, domain.SyncAck{Type: parts[0], Ack: ack})
	}
	if err := s.app.Store.SyncAcks().Put(r.Context(), a.User.ID, entries); err != nil {
		s.writeInternal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteSyncAck(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req struct {
		Types []string `json:"types"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.app.Store.SyncAcks().DeleteTypes(r.Context(), a.User.ID, req.Types); err != nil {
		s.writeInternal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// syncStream emits NDJSON lines {"ack","type","data"}. Supported types:
// AuthUserV1/UserV1 (users), AssetV1/AssetV2 (owner assets),
// AlbumsV1 (albums), AlbumToAssetsV1 (album membership).
func (s *Server) syncStream(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req struct {
		Reset bool     `json:"reset"`
		Types []string `json:"types"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	ackedTypes := map[string]bool{}
	if !req.Reset {
		if acks, err := s.app.Store.SyncAcks().List(r.Context(), a.User.ID); err == nil {
			for _, ack := range acks {
				ackedTypes[ack.Type] = true
			}
		}
	}

	wanted := map[string]bool{}
	for _, t := range req.Types {
		wanted[t] = true
	}

	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)

	write := func(typ, ack string, data any) {
		line := struct {
			Ack  string       `json:"ack"`
			Type string       `json:"type"`
			Data any          `json:"data"`
		}{Ack: ack, Type: typ, Data: data}
		b, err := json.Marshal(line)
		if err != nil {
			return
		}
		w.Write(append(b, '\n'))
		if flusher != nil {
			flusher.Flush()
		}
	}
	synced := map[string]bool{}

	emitUsers := func(typ string) {
		if ackedTypes[typ] {
			return
		}
		if typ == "AuthUserV1" {
			write(typ, typ+":"+a.User.ID, userResponse(a.User))
			return
		}
		users, err := s.app.Store.Users().List(r.Context())
		if err != nil {
			return
		}
		for _, u := range users {
			write(typ, typ+":"+u.ID, userResponse(u))
		}
	}

	emitAssets := func(typ string) {
		if ackedTypes[typ] {
			return
		}
		assets, err := s.app.Store.Assets().ListForOwner(r.Context(), a.User.ID)
		if err != nil {
			return
		}
		for _, asset := range assets {
			if asset.DeletedAt == nil {
				write(typ, typ+":"+asset.ID, s.syncAsset(asset))
			}
		}
	}

	emitAlbums := func(typ string) {
		if ackedTypes[typ] {
			return
		}
		albums, err := s.app.Store.Albums().ListForOwner(r.Context(), a.User.ID)
		if err != nil {
			return
		}
		for _, al := range albums {
			resp := s.albumResponse(r, al, false)
			if typ == "AlbumToAssetsV1" {
				write(typ, typ+":"+al.ID, map[string]any{
					"albumId":  al.ID,
					"assetIds": al.AssetIDs,
				})
				continue
			}
			write(typ, typ+":"+al.ID, resp)
		}
	}

	for _, typ := range []string{
		"AuthUserV1", "UserV1", "UsersV1",
		"AssetV1", "AssetV2", "AssetsV1",
		"AlbumsV1", "AlbumV1", "AlbumToAssetsV1",
	} {
		if !wanted[typ] || synced[typ] {
			continue
		}
		switch {
		case typ == "AuthUserV1":
			emitUsers(typ)
			synced[typ] = true
		case typ == "UserV1" || typ == "UsersV1":
			emitUsers(typ)
			synced[typ] = true
		case typ == "AssetV1" || typ == "AssetV2" || typ == "AssetsV1":
			emitAssets(typ)
			synced[typ] = true
		default:
			emitAlbums(typ)
			synced[typ] = true
		}
	}
}

// syncAsset is the v1 wire shape for synced assets (subset of
// AssetResponseDto the mobile sync consumer needs).
func (s *Server) syncAsset(asset *domain.Asset) map[string]any {
	return map[string]any{
		"id":               asset.ID,
		"ownerId":          asset.OwnerID,
		"type":             asset.Type,
		"originalPath":     asset.OriginalPath,
		"originalFileName": asset.OriginalFileName,
		"fileCreatedAt":    ISOTime(asset.FileCreatedAt),
		"fileModifiedAt":   ISOTime(asset.FileModifiedAt),
		"localDateTime":    ISOTime(asset.LocalDateTime),
		"createdAt":        ISOTime(asset.CreatedAt),
		"updatedAt":        ISOTime(asset.UpdatedAt),
		"isFavorite":       asset.IsFavorite,
		"isArchived":       asset.Visibility == domain.VisibilityArchive,
		"isTrashed":        asset.DeletedAt != nil,
		"duration":         asset.Duration,
		"width":            asset.Width,
		"height":           asset.Height,
		"visibility":       asset.Visibility,
		"checksum":         asset.ChecksumB64,
	}
}
