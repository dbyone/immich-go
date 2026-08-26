// Package api exposes the Immich-compatible REST surface under /api.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"immich-go/internal/app"
	"immich-go/internal/auth"
	"immich-go/internal/store"
)

type Server struct {
	app *app.App
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
	r.Use(s.authGuard)

	r.Route("/api", func(r chi.Router) {
		// --- public ---
		r.Get("/server/ping", s.serverPing)
		r.Get("/server/version", s.serverVersion)
		r.Get("/server/version-history", s.serverVersionHistory)
		r.Get("/server/media-types", s.serverMediaTypes)
		r.Post("/auth/login", s.authLogin)
		r.Post("/auth/admin-sign-up", s.authAdminSignUp)

		// --- authenticated ---
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)

			r.Post("/auth/validateToken", s.authValidateToken)
			r.Post("/auth/logout", s.authLogout)
			r.Post("/auth/change-password", s.authChangePassword)
			r.Get("/auth/status", s.authStatus)

			r.Get("/users", s.listUsers)
			r.Get("/users/me", s.getMe)
			r.Patch("/users/me", s.updateMe)
			r.Get("/users/{id}", s.getUser)

			r.Get("/api-keys", s.listAPIKeys)
			r.Post("/api-keys", s.createAPIKey)
			r.Get("/api-keys/me", s.getCurrentAPIKey)
			r.Get("/api-keys/{id}", s.getAPIKey)
			r.Put("/api-keys/{id}", s.updateAPIKey)
			r.Delete("/api-keys/{id}", s.deleteAPIKey)
			r.Post("/api-keys/{id}/rotate", s.rotateAPIKey)

			r.Get("/sessions", s.listSessions)
			r.Delete("/sessions", s.deleteAllSessions)
			r.Delete("/sessions/{id}", s.deleteSession)

			r.Post("/assets", s.uploadAsset)
			r.Get("/assets/statistics", s.assetStatistics)
			r.Post("/assets/bulk-upload-check", s.bulkUploadCheck)
			r.Post("/assets/jobs", s.assetJobs)
			r.Put("/assets", s.bulkUpdateAssets)
			r.Delete("/assets", s.bulkDeleteAssets)
			r.Get("/assets/{id}", s.getAsset)
			r.Put("/assets/{id}", s.updateAsset)
			r.Get("/assets/{id}/original", s.getAssetOriginal)
			r.Get("/assets/{id}/thumbnail", s.getAssetThumbnail)
			r.Get("/assets/{id}/video/playback", s.getAssetVideoPlayback)

			r.Get("/albums", s.listAlbums)
			r.Post("/albums", s.createAlbum)
			r.Put("/albums/assets", s.addAssetsToAlbums)
			r.Get("/albums/statistics", s.albumStatistics)
			r.Get("/albums/{id}", s.getAlbum)
			r.Patch("/albums/{id}", s.updateAlbum)
			r.Delete("/albums/{id}", s.deleteAlbum)
			r.Put("/albums/{id}/assets", s.addAssetsToAlbum)
			r.Delete("/albums/{id}/assets", s.removeAssetsFromAlbum)
			r.Put("/albums/{id}/users", s.addUsersToAlbum)
			r.Delete("/albums/{id}/user/{userId}", s.removeUserFromAlbum)

			r.Get("/timeline/buckets", s.timelineBuckets)
			r.Get("/timeline/bucket", s.timelineBucket)

			r.Post("/search/metadata", s.searchMetadata)
			r.Post("/search/smart", s.searchSmart)

			r.Post("/trash/empty", s.emptyTrash)
			r.Post("/trash/restore", s.restoreTrash)

			r.Get("/jobs", s.listJobs)
			r.Post("/jobs", s.createJob)
			r.Put("/jobs/{name}", s.commandJob)

			r.Get("/server/about", s.serverAbout)
			r.Get("/server/config", s.serverConfig)
			r.Get("/server/features", s.serverFeatures)
			r.Get("/server/storage", s.serverStorage)
			r.Get("/server/statistics", s.serverStatistics)
		})
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "Route not found: "+r.URL.Path)
	})
	return r
}

// --- middleware ---

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

func storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "Not found")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "Conflict")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	return dec.Decode(v)
}
