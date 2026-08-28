// Sync protocol, incremental edition. Every synced entity row carries an
// update_id drawn from one global sequence; acks are "<Type>:<updateId>"
// watermarks per type. The stream returns entities whose update_id exceeds
// the client's watermark plus tombstones (AssetDeleteV1, AlbumDeleteV1,
// UserDeleteV1) recorded on hard deletes. A type without an ack streams a
// full snapshot first; reset=true or deleting acks restarts a type.
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"immich-go/internal/domain"
)

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

// watermark extracts the highest acknowledged update_id for each type.
func watermark(acks []domain.SyncAck) map[string]int64 {
	out := map[string]int64{}
	for _, a := range acks {
		parts := strings.SplitN(a.Ack, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if id, err := strconv.ParseInt(parts[1], 10, 64); err == nil && id > out[a.Type] {
			out[a.Type] = id
		}
	}
	return out
}

// syncStream emits NDJSON lines {"ack","type","data"}.
//
// Supported request types: AuthUsersV1 (self), UsersV1, AssetsV1/AssetsV2
// (upserts + AssetDeleteV1 tombstones), AlbumsV1 (AlbumV1 upserts with
// member assetIds + AlbumDeleteV1). Unknown types are ignored.
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
	wanted := map[string]bool{}
	for _, t := range req.Types {
		wanted[t] = true
	}

	since := map[string]int64{}
	if !req.Reset {
		if acks, err := s.app.Store.SyncAcks().List(r.Context(), a.User.ID); err == nil {
			since = watermark(acks)
		}
	}

	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)

	write := func(typ, ack string, data any) {
		line := struct {
			Ack  string `json:"ack"`
			Type string `json:"type"`
			Data any    `json:"data"`
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

	const limit = 1000

	// The canonical request type is AuthUsersV1; accept the singular
	// spelling older clients used too. Honors the watermark like any
	// other type so an acked self stops resending.
	if wanted["AuthUsersV1"] || wanted["AuthUserV1"] {
		if a.User.UpdateID > since["AuthUserV1"] {
			write("AuthUserV1", "AuthUserV1:"+strconv.FormatInt(a.User.UpdateID, 10), userResponse(a.User))
		}
	}

	if wanted["UsersV1"] {
		users, err := s.app.Store.Sync().UsersSince(r.Context(), since["UserV1"], limit)
		if err == nil {
			for _, u := range users {
				write("UserV1", "UserV1:"+strconv.FormatInt(u.UpdateID, 10), userResponse(u))
			}
		}
		if dels, err := s.app.Store.Sync().DeletesSince(r.Context(), []string{"UserDeleteV1"}, since["UserDeleteV1"], limit); err == nil {
			for _, d := range dels {
				write(d.Type, d.Type+":"+strconv.FormatInt(d.UpdateID, 10), map[string]any{"userId": d.EntityID})
			}
		}
	}

	if wanted["AssetsV1"] || wanted["AssetsV2"] {
		assetType := "AssetV1"
		if wanted["AssetsV2"] {
			assetType = "AssetV2"
		}
		assets, err := s.app.Store.Sync().AssetsSince(r.Context(), a.User.ID, since[assetType], limit)
		if err == nil {
			for _, asset := range assets {
				write(assetType, assetType+":"+strconv.FormatInt(asset.UpdateID, 10), s.syncAsset(asset))
			}
		}
		if dels, err := s.app.Store.Sync().DeletesSince(r.Context(), []string{"AssetDeleteV1"}, since["AssetDeleteV1"], limit); err == nil {
			for _, d := range dels {
				write(d.Type, d.Type+":"+strconv.FormatInt(d.UpdateID, 10), map[string]any{"assetId": d.EntityID})
			}
		}
	}

	if wanted["AlbumsV1"] || wanted["AlbumsV2"] {
		albumType := "AlbumV1"
		if wanted["AlbumsV2"] {
			albumType = "AlbumV2"
		}
		albums, err := s.app.Store.Sync().AlbumsSince(r.Context(), a.User.ID, since[albumType], limit)
		if err == nil {
			for _, al := range albums {
				write(albumType, albumType+":"+strconv.FormatInt(al.UpdateID, 10), map[string]any{
					"id":        al.ID,
					"ownerId":   al.OwnerID,
					"albumName": al.AlbumName,
					"assetIds":  al.AssetIDs,
					"createdAt": ISOTime(al.CreatedAt),
					"updatedAt": ISOTime(al.UpdatedAt),
				})
			}
		}
		if dels, err := s.app.Store.Sync().DeletesSince(r.Context(), []string{"AlbumDeleteV1"}, since["AlbumDeleteV1"], limit); err == nil {
			for _, d := range dels {
				write(d.Type, d.Type+":"+strconv.FormatInt(d.UpdateID, 10), map[string]any{"albumId": d.EntityID})
			}
		}
	}
}

// syncAsset is the v1 wire shape for synced assets.
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
