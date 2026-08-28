// Package api exposes the Immich-compatible REST surface under /api.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"immich-go/internal/app"
	"immich-go/internal/auth"
	"immich-go/internal/store"
)

type Server struct {
	app *app.App

	// albumMu serializes read-modify-write membership mutations on albums
	// and memories (the store has no row locking; single-instance guard).
	albumMu sync.Mutex
}

func New(a *app.App) *Server { return &Server{app: a} }

// defaultCtx carries request-scoped cancellation into store calls that do
// not need the request context.
var defaultCtx = context.Background()

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(s.recoverer)
	r.Use(s.bodyLimit)
	r.Use(s.authGuard)

	r.Route("/api", func(r chi.Router) {
		// --- public ---
		r.Get("/server/ping", s.serverPing)
		r.Get("/server/version", s.serverVersion)
		r.Get("/server/version-history", s.serverVersionHistory)
		r.Get("/server/media-types", s.serverMediaTypes)
		r.Get("/public/config", s.getPublicConfig)
		r.Get("/public/config/defaults", s.getPublicConfigDefaults)
		r.Post("/auth/login", s.authLogin)
		r.Post("/auth/admin-sign-up", s.authAdminSignUp)

		// --- authenticated; each route pins its API-key permission ---
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)

			r.Post("/auth/validateToken", s.authValidateToken)
			r.Post("/auth/logout", s.authLogout)
			r.With(s.perm("user.update")).Post("/auth/change-password", s.authChangePassword)
			r.Get("/auth/status", s.authStatus)

			r.With(s.perm("user.read")).Get("/users", s.listUsers)
			r.Get("/users/me", s.getMe)
			r.With(s.perm("user.update")).Patch("/users/me", s.updateMe)
			r.Get("/users/me/preferences", s.getMyPreferences)
			r.With(s.perm("user.update")).Put("/users/me/preferences", s.updateMyPreferences)
			r.With(s.perm("user.read")).Get("/users/{id}", s.getUser)

			// user-facing config + onboarding state
			r.Get("/config", s.getUserConfig)
			r.Get("/config/defaults", s.getUserConfigDefaults)
			r.With(s.perm("adminConfig.read")).Get("/system-metadata/admin-onboarding", s.getAdminOnboarding)
			r.With(s.perm("adminConfig.update")).Post("/system-metadata/admin-onboarding", s.updateAdminOnboarding)
			r.With(s.perm("server.read")).Get("/server/apk-links", s.serverApkLinks)
			r.With(s.perm("server.read")).Get("/server/version-check", s.serverVersionCheck)
			r.With(s.perm("server.read")).Get("/server/about", s.serverAbout)
			r.With(s.perm("server.read")).Get("/server/config", s.serverConfig)
			r.With(s.perm("server.read")).Get("/server/features", s.serverFeatures)
			r.With(s.perm("server.read")).Get("/server/storage", s.serverStorage)
			r.With(s.perm("adminServer.read")).Get("/server/statistics", s.serverStatistics)

			r.With(s.perm("apiKey.read")).Get("/api-keys", s.listAPIKeys)
			r.With(s.perm("apiKey.create")).Post("/api-keys", s.createAPIKey)
			r.With(s.perm("apiKey.read")).Get("/api-keys/me", s.getCurrentAPIKey)
			r.With(s.perm("apiKey.read")).Get("/api-keys/{id}", s.getAPIKey)
			r.With(s.perm("apiKey.update")).Put("/api-keys/{id}", s.updateAPIKey)
			r.With(s.perm("apiKey.delete")).Delete("/api-keys/{id}", s.deleteAPIKey)
			r.With(s.perm("apiKey.rotate")).Post("/api-keys/{id}/rotate", s.rotateAPIKey)

			r.With(s.perm("session.read")).Get("/sessions", s.listSessions)
			r.With(s.perm("session.delete")).Delete("/sessions", s.deleteAllSessions)
			r.With(s.perm("session.delete")).Delete("/sessions/{id}", s.deleteSession)

			r.With(s.perm("asset.create")).Post("/assets", s.uploadAsset)
			r.With(s.perm("asset.read")).Get("/assets/statistics", s.assetStatistics)
			r.With(s.perm("asset.create")).Post("/assets/bulk-upload-check", s.bulkUploadCheck)
			r.With(s.perm("asset.update")).Post("/assets/jobs", s.assetJobs)
			r.With(s.perm("asset.update")).Put("/assets", s.bulkUpdateAssets)
			r.With(s.perm("asset.delete")).Delete("/assets", s.bulkDeleteAssets)
			r.With(s.perm("asset.read")).Get("/assets/{id}", s.getAsset)
			r.With(s.perm("asset.update")).Put("/assets/{id}", s.updateAsset)
			// immich-go extension: re-run the metadata pipeline for one
			// asset (the backend half of MT Photos' per-photo refresh).
			r.With(s.perm("asset.update")).Post("/assets/{id}/refresh", s.refreshAsset)
			// immich-go extension: live zero-shot scene scores for one
			// asset, recomputed from its stored CLIP embedding.
			r.With(s.perm("asset.read")).Get("/assets/{id}/classification", s.assetClassification)
			r.With(s.perm("asset.download")).Get("/assets/{id}/original", s.getAssetOriginal)
			r.With(s.perm("asset.read")).Get("/assets/{id}/thumbnail", s.getAssetThumbnail)
			r.With(s.perm("asset.read")).Get("/assets/{id}/video/playback", s.getAssetVideoPlayback)

			r.With(s.perm("album.read")).Get("/albums", s.listAlbums)
			r.With(s.perm("album.create")).Post("/albums", s.createAlbum)
			r.With(s.perm("album.update")).Put("/albums/assets", s.addAssetsToAlbums)
			r.With(s.perm("album.read")).Get("/albums/statistics", s.albumStatistics)
			r.With(s.perm("album.read")).Get("/albums/{id}", s.getAlbum)
			r.With(s.perm("album.update")).Patch("/albums/{id}", s.updateAlbum)
			r.With(s.perm("album.delete")).Delete("/albums/{id}", s.deleteAlbum)
			r.With(s.perm("album.update")).Put("/albums/{id}/assets", s.addAssetsToAlbum)
			r.With(s.perm("album.update")).Delete("/albums/{id}/assets", s.removeAssetsFromAlbum)
			r.With(s.perm("album.update")).Put("/albums/{id}/users", s.addUsersToAlbum)
			r.With(s.perm("album.update")).Delete("/albums/{id}/user/{userId}", s.removeUserFromAlbum)

			r.With(s.perm("asset.read")).Get("/timeline/buckets", s.timelineBuckets)
			r.With(s.perm("asset.read")).Get("/timeline/bucket", s.timelineBucket)

			r.With(s.perm("asset.read")).Post("/search/metadata", s.searchMetadata)
			r.With(s.perm("asset.read")).Post("/search/smart", s.searchSmart)

			r.With(s.perm("person.read")).Get("/people", s.listPeople)
			r.With(s.perm("person.create")).Post("/people", s.createPerson)
			r.With(s.perm("person.update")).Put("/people", s.updatePeopleBulk)
			r.With(s.perm("person.delete")).Delete("/people", s.deletePeopleBulk)
			r.With(s.perm("person.read")).Get("/people/{id}", s.getPersonDetail)
			r.With(s.perm("person.update")).Put("/people/{id}", s.updatePerson)
			r.With(s.perm("person.delete")).Delete("/people/{id}", s.deletePerson)
			r.With(s.perm("person.update")).Post("/people/{id}/merge", s.mergePerson)
			r.With(s.perm("person.update")).Put("/people/{id}/reassign", s.reassignFaces)
			r.With(s.perm("person.read")).Get("/people/{id}/statistics", s.personStatistics)
			r.With(s.perm("person.read")).Get("/people/{id}/thumbnail", s.personThumbnail)

			r.With(s.perm("asset.read")).Get("/duplicates", s.listDuplicates)
			r.With(s.perm("asset.update")).Post("/duplicates/resolve", s.resolveDuplicates)
			r.With(s.perm("asset.update")).Delete("/duplicates", s.deleteDuplicatesBulk)
			r.With(s.perm("asset.update")).Delete("/duplicates/{id}", s.deleteDuplicateGroup)

			// tags
			r.With(s.perm("tag.read")).Get("/tags", s.listTags)
			r.With(s.perm("tag.create")).Post("/tags", s.createTag)
			r.With(s.perm("tag.create")).Put("/tags", s.upsertTags)
			r.With(s.perm("tag.asset")).Put("/tags/assets", s.bulkTagAssets)
			r.With(s.perm("tag.read")).Get("/tags/{id}", s.getTag)
			r.With(s.perm("tag.update")).Put("/tags/{id}", s.updateTag)
			r.With(s.perm("tag.delete")).Delete("/tags/{id}", s.deleteTag)
			r.With(s.perm("tag.asset")).Put("/tags/{id}/assets", s.tagAssets)
			r.With(s.perm("tag.asset")).Delete("/tags/{id}/assets", s.untagAssets)

			// memories
			r.With(s.perm("memory.read")).Get("/memories", s.listMemories)
			r.With(s.perm("memory.create")).Post("/memories", s.createMemory)
			r.With(s.perm("memory.read")).Get("/memories/statistics", s.memoriesStatistics)
			r.With(s.perm("memory.read")).Get("/memories/{id}", s.getMemory)
			r.With(s.perm("memory.update")).Put("/memories/{id}", s.updateMemory)
			r.With(s.perm("memory.delete")).Delete("/memories/{id}", s.deleteMemory)
			r.With(s.perm("memory.update")).Put("/memories/{id}/assets", s.memoryAssetsUpdate(true))
			r.With(s.perm("memory.update")).Delete("/memories/{id}/assets", s.memoryAssetsUpdate(false))

			// sync (basic)
			r.With(s.perm("sync.stream")).Get("/sync/ack", s.getSyncAck)
			r.With(s.perm("sync.stream")).Post("/sync/ack", s.sendSyncAck)
			r.With(s.perm("sync.stream")).Delete("/sync/ack", s.deleteSyncAck)
			r.With(s.perm("sync.stream")).Post("/sync/stream", s.syncStream)

			// search long tail
			r.With(s.perm("asset.read")).Get("/search/explore", s.searchExplore)
			r.With(s.perm("asset.read")).Post("/search/random", s.searchRandom)
			r.With(s.perm("asset.read")).Post("/search/statistics", s.searchStatistics)
			r.With(s.perm("asset.read")).Get("/search/cities", s.searchCities)
			r.With(s.perm("asset.read")).Get("/search/places", s.searchPlaces)
			r.With(s.perm("person.read")).Get("/search/person", s.searchPerson)
			r.With(s.perm("asset.read")).Post("/search/large-assets", s.searchLargeAssets)
			r.With(s.perm("asset.read")).Get("/search/suggestions", s.searchSuggestions)

			// sessions: create / update / lock
			r.With(s.perm("session.create")).Post("/sessions", s.createSession)
			r.With(s.perm("session.update")).Put("/sessions/{id}", s.updateSession)
			r.With(s.perm("session.update")).Post("/sessions/{id}/lock", s.lockSession)

			// stacks
			r.With(s.perm("asset.read")).Get("/stacks", s.listStacks)
			r.With(s.perm("asset.update")).Post("/stacks", s.createStack)
			r.With(s.perm("asset.update")).Delete("/stacks", s.deleteStacksBulk)
			r.With(s.perm("asset.read")).Get("/stacks/{id}", s.getStack)
			r.With(s.perm("asset.update")).Put("/stacks/{id}", s.updateStack)
			r.With(s.perm("asset.update")).Delete("/stacks/{id}", s.deleteStack)
			r.With(s.perm("asset.update")).Delete("/stacks/{id}/assets/{assetId}", s.removeStackAsset)

			// partners
			r.With(s.perm("partner.read")).Get("/partners", s.listPartners)
			r.With(s.perm("partner.create")).Post("/partners", s.createPartnerBody)
			r.With(s.perm("partner.create")).Post("/partners/{id}", s.createPartnerByID)
			r.With(s.perm("partner.update")).Put("/partners/{id}", s.updatePartner)
			r.With(s.perm("partner.delete")).Delete("/partners/{id}", s.removePartner)

			// search long tail
			r.With(s.perm("asset.read")).Get("/search/explore", s.searchExplore)
			r.With(s.perm("asset.read")).Post("/search/random", s.searchRandom)
			r.With(s.perm("asset.read")).Post("/search/statistics", s.searchStatistics)
			r.With(s.perm("asset.read")).Get("/search/cities", s.searchCities)
			r.With(s.perm("asset.read")).Get("/search/places", s.searchPlaces)
			r.With(s.perm("person.read")).Get("/search/person", s.searchPerson)
			r.With(s.perm("asset.read")).Post("/search/large-assets", s.searchLargeAssets)
			r.With(s.perm("asset.read")).Get("/search/suggestions", s.searchSuggestions)

			// sessions: create / update / lock
			r.With(s.perm("session.create")).Post("/sessions", s.createSession)
			r.With(s.perm("session.update")).Put("/sessions/{id}", s.updateSession)
			r.With(s.perm("session.update")).Post("/sessions/{id}/lock", s.lockSession)

			// stacks
			r.With(s.perm("asset.read")).Get("/stacks", s.listStacks)
			r.With(s.perm("asset.update")).Post("/stacks", s.createStack)
			r.With(s.perm("asset.update")).Delete("/stacks", s.deleteStacksBulk)
			r.With(s.perm("asset.read")).Get("/stacks/{id}", s.getStack)
			r.With(s.perm("asset.update")).Put("/stacks/{id}", s.updateStack)
			r.With(s.perm("asset.update")).Delete("/stacks/{id}", s.deleteStack)
			r.With(s.perm("asset.update")).Delete("/stacks/{id}/assets/{assetId}", s.removeStackAsset)

			// partners
			r.With(s.perm("partner.read")).Get("/partners", s.listPartners)
			r.With(s.perm("partner.create")).Post("/partners", s.createPartnerBody)
			r.With(s.perm("partner.create")).Post("/partners/{id}", s.createPartnerByID)
			r.With(s.perm("partner.update")).Put("/partners/{id}", s.updatePartner)
			r.With(s.perm("partner.delete")).Delete("/partners/{id}", s.removePartner)

			// download / map / folder view
			r.With(s.perm("asset.download")).Post("/download/info", s.downloadInfo)
			r.With(s.perm("asset.download")).Post("/download/archive", s.downloadArchive)
			r.With(s.perm("asset.read")).Get("/map/markers", s.mapMarkers)
			r.With(s.perm("asset.read")).Get("/map/reverse-geocode", s.reverseGeocode)
			r.With(s.perm("asset.read")).Get("/view/folder", s.folderView)
			r.With(s.perm("asset.read")).Get("/view/folder/unique-paths", s.folderUniquePaths)

			r.With(s.perm("asset.update")).Post("/trash/empty", s.emptyTrash)
			r.With(s.perm("asset.update")).Post("/trash/restore", s.restoreTrash)

			r.With(s.perm("adminQueue.read")).Get("/jobs", s.listJobs)
			r.With(s.perm("adminQueue.run")).Post("/jobs", s.createJob)
			r.With(s.perm("adminQueue.update")).Put("/jobs/{name}", s.commandJob)
		})
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "Route not found: "+r.URL.Path)
	})
	return r
}

// --- middleware ---

// jsonBodyLimit bounds non-multipart request bodies (uploads enforce
// their own, larger cap), mirroring the upstream body-parser 10mb limit.
const jsonBodyLimit = 10 << 20

func (s *Server) bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "multipart/") {
			r.Body = http.MaxBytesReader(w, r.Body, jsonBodyLimit)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.app.Log.Error("panic recovered", "err", rec)
				writeError(w, http.StatusInternalServerError, "Internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// authGuard resolves credentials lazily; requireAuth turns missing
// credentials into a 401. Public routes register outside the requireAuth
// group but may still inspect the resolved identity.
func (s *Server) authGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a, err := s.app.Auth.Authenticate(r.Context(), r); err == nil {
			r = r.WithContext(a.WithContext(r.Context()))
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth.FromRequest(r) == nil {
			writeJSON(w, http.StatusUnauthorized, apiError{
				Message: "Invalid token", Error: "Unauthorized", StatusCode: 401,
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// perm enforces the API-key scope for one route centrally; session
// callers always pass (they hold every permission). Handlers may keep
// their own checks as defense in depth.
func (s *Server) perm(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			a := auth.FromRequest(r)
			if a == nil {
				writeJSON(w, http.StatusUnauthorized, apiError{Message: "Invalid token", Error: "Unauthorized", StatusCode: 401})
				return
			}
			if !a.HasPermission(permission) {
				writeJSON(w, http.StatusForbidden, apiError{Message: "Missing permission: " + permission, Error: "Forbidden", StatusCode: 403})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// caller returns the resolved auth context or writes a 401.
func caller(w http.ResponseWriter, r *http.Request) *auth.AuthContext {
	a := auth.FromRequest(r)
	if a == nil {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "Invalid token", Error: "Unauthorized", StatusCode: 401})
	}
	return a
}

// requirePermission enforces API-key scopes; session callers always pass.
func (s *Server) requirePermission(w http.ResponseWriter, r *http.Request, permission string) bool {
	a := auth.FromRequest(r)
	if a == nil {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "Invalid token", Error: "Unauthorized", StatusCode: 401})
		return false
	}
	if !a.HasPermission(permission) {
		writeJSON(w, http.StatusForbidden, apiError{Message: "Missing permission: " + permission, Error: "Forbidden", StatusCode: 403})
		return false
	}
	return true
}

// --- response helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	reason := http.StatusText(status)
	writeJSON(w, status, apiError{Message: msg, Error: reason, StatusCode: status})
}

// writeInternal logs the real error and answers with a generic message —
// DuckDB errors, paths and ML URLs never reach the client.
func (s *Server) writeInternal(w http.ResponseWriter, err error) {
	s.app.Log.Error("internal error", "err", err)
	writeError(w, http.StatusInternalServerError, "Internal error")
}

func (s *Server) storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "Not found")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "Conflict")
	default:
		s.writeInternal(w, err)
	}
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	return dec.Decode(v)
}
