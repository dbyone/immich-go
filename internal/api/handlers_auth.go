package api

import (
	"net/http"
	"strings"
	"time"

	"immich-go/internal/auth"
	"immich-go/internal/crypto"
	"immich-go/internal/domain"
)

const cookieMaxAge = 400 * 24 * time.Hour // mirrors the upstream cookie lifetime

type loginRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	DeviceOS   string `json:"deviceOS"`
	DeviceType string `json:"deviceType"`
}

// isSecureRequest honors X-Forwarded-Proto so cookies stay consistent
// behind TLS-terminating proxies.
func isSecureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// setAuthCookies mirrors src/utils/response.ts setAuthCookies.
func setAuthCookies(w http.ResponseWriter, r *http.Request, token string) {
	secure := isSecureRequest(r)
	setCookie(w, "immich_access_token", token, true, secure)
	setCookie(w, "immich_auth_type", "password", true, secure)
	setCookie(w, "immich_is_authenticated", "true", false, secure)
}

func setCookie(w http.ResponseWriter, name, value string, httpOnly, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(cookieMaxAge.Seconds()),
		HttpOnly: httpOnly,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearAuthCookies(w http.ResponseWriter, r *http.Request) {
	secure := isSecureRequest(r)
	for _, name := range []string{"immich_access_token", "immich_auth_type", "immich_is_authenticated"} {
		http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, Secure: secure, SameSite: http.SameSiteLaxMode})
	}
}

func clientInfo(r *http.Request) (os, deviceType, appVersion string) {
	os = r.UserAgent()
	deviceType = "web"
	if v := r.Header.Get("x-immich-client-version"); v != "" {
		appVersion = v
	}
	return os, deviceType, appVersion
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil || req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	deviceOS, deviceType, appVersion := clientInfo(r)
	res, err := s.app.Auth.Login(r.Context(), auth.LoginInput{
		Email:      strings.ToLower(strings.TrimSpace(req.Email)),
		Password:   req.Password,
		DeviceOS:   deviceOS,
		DeviceType: deviceType,
		AppVersion: appVersion,
	})
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "Incorrect email or password", Error: "Unauthorized", StatusCode: 401})
		return
	}

	setAuthCookies(w, r, res.Token)
	user := res.User
	writeJSON(w, http.StatusCreated, LoginResponse{
		AccessToken:          res.Token,
		UserID:               user.ID,
		UserEmail:            user.Email,
		Name:                 user.Name,
		ProfileImagePath:     user.ProfileImagePath,
		IsAdmin:              user.IsAdmin,
		ShouldChangePassword: user.ShouldChangePassword,
		IsOnboarded:          user.IsOnboarded,
		User:                 userResponsePtr(user),
	})
}

type signUpRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

const adminBootstrapKey = "admin-bootstrapped"

func (s *Server) authAdminSignUp(w http.ResponseWriter, r *http.Request) {
	var req signUpRequest
	if err := decodeJSON(r, &req); err != nil || req.Email == "" || req.Password == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name, email and password are required")
		return
	}

	// The endpoint is only valid while the instance has no users. The
	// atomic sentinel claim closes the TOCTOU window two concurrent
	// sign-ups would otherwise race through.
	if count, _ := s.app.Store.Users().Count(r.Context()); count > 0 {
		writeError(w, http.StatusBadRequest, "The server already has an admin")
		return
	}
	claimed, err := s.app.Store.Metadata().SetIfAbsent(r.Context(), adminBootstrapKey, "1")
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	if !claimed {
		writeError(w, http.StatusBadRequest, "The server already has an admin")
		return
	}

	hash, err := crypto.HashPassword(req.Password)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	now := time.Now().UTC()
	user := &domain.User{
		ID:          crypto.NewUUID(),
		Email:       strings.ToLower(strings.TrimSpace(req.Email)),
		Password:    hash,
		Name:        req.Name,
		IsAdmin:     true,
		AvatarColor: "primary",
		IsOnboarded: true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.app.Store.Users().Create(r.Context(), user); err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, userResponse(user))
}

func (s *Server) authValidateToken(w http.ResponseWriter, r *http.Request) {
	if caller(w, r) == nil {
		return
	}
	writeJSON(w, http.StatusOK, ValidateTokenResponse{AuthStatus: true})
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a != nil && a.Token != "" && !a.IsAPIKey {
		_ = s.app.Auth.Logout(r.Context(), a.Token)
	}
	clearAuthCookies(w, r)
	writeJSON(w, http.StatusOK, LogoutResponse{RedirectURI: "", Successful: true})
}

func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	writeJSON(w, http.StatusOK, AuthStatusResponse{
		IsElevated: true,
		Password:   a.User.Password != "",
		PinCode:    false,
	})
}

type changePasswordRequest struct {
	Password    string `json:"password"`
	NewPassword string `json:"newPassword"`
}

func (s *Server) authChangePassword(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req changePasswordRequest
	if err := decodeJSON(r, &req); err != nil || req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "newPassword is required")
		return
	}
	if !crypto.ComparePassword(req.Password, a.User.Password) {
		writeError(w, http.StatusForbidden, "Wrong password")
		return
	}
	hash, err := crypto.HashPassword(req.NewPassword)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	user := a.User
	user.Password = hash
	user.ShouldChangePassword = false
	user.UpdatedAt = time.Now().UTC()
	if err := s.app.Store.Users().Update(r.Context(), user); err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, userResponse(user))
}

// --- users ---

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "user.read") {
		return
	}
	users, err := s.app.Store.Users().List(r.Context())
	if err != nil {
		s.storeError(w, err)
		return
	}
	out := make([]UserResponse, 0, len(users))
	for _, u := range users {
		out = append(out, userResponse(u))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getMe(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	writeJSON(w, http.StatusOK, userResponse(a.User))
}

type updateMeRequest struct {
	Name  *string `json:"name"`
	Email *string `json:"email"`
}

func (s *Server) updateMe(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req updateMeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	user := a.User
	if req.Name != nil {
		user.Name = *req.Name
	}
	if req.Email != nil && *req.Email != "" {
		user.Email = strings.ToLower(strings.TrimSpace(*req.Email))
	}
	user.UpdatedAt = time.Now().UTC()
	if err := s.app.Store.Users().Update(r.Context(), user); err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, userResponse(user))
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "user.read") {
		return
	}
	u, err := s.app.Store.Users().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, userResponse(u))
}

// --- api keys ---

func (s *Server) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	keys, err := s.app.Store.APIKeys().ListForUser(r.Context(), a.User.ID)
	if err != nil {
		s.storeError(w, err)
		return
	}
	out := make([]APIKeyResponse, 0, len(keys))
	for _, k := range keys {
		out = append(out, apiKeyResponse(k))
	}
	writeJSON(w, http.StatusOK, out)
}

func apiKeyResponse(k *domain.APIKey) APIKeyResponse {
	return APIKeyResponse{
		ID:          k.ID,
		Name:        k.Name,
		Permissions: k.Permissions,
		CreatedAt:   ISOTime(k.CreatedAt),
		UpdatedAt:   ISOTime(k.UpdatedAt),
	}
}

type createAPIKeyRequest struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

func (s *Server) createAPIKey(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var req createAPIKeyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	key, secret, err := s.app.Auth.CreateAPIKey(r.Context(), a.User.ID, req.Name, req.Permissions)
	if err != nil {
		s.storeError(w, err)
		return
	}
	resp := APIKeyCreateResponse{
		ID:          key.ID,
		Name:        key.Name,
		Permissions: key.Permissions,
		Secret:      secret,
		CreatedAt:   ISOTime(key.CreatedAt),
		UpdatedAt:   ISOTime(key.UpdatedAt),
	}
	k := apiKeyResponse(key)
	resp.APIKey = &k
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) getCurrentAPIKey(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil || !a.IsAPIKey || a.APIKey == nil {
		if a == nil {
			return
		}
		writeError(w, http.StatusNotFound, "Not authenticated with an API key")
		return
	}
	writeJSON(w, http.StatusOK, apiKeyResponse(a.APIKey))
}

func (s *Server) getAPIKey(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	key, err := s.app.Store.APIKeys().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || key.UserID != a.User.ID {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	writeJSON(w, http.StatusOK, apiKeyResponse(key))
}

type updateAPIKeyRequest struct {
	Name *string `json:"name"`
}

func (s *Server) updateAPIKey(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	key, err := s.app.Store.APIKeys().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || key.UserID != a.User.ID {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	var req updateAPIKeyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Name != nil {
		key.Name = *req.Name
	}
	key.UpdatedAt = time.Now().UTC()
	if err := s.app.Store.APIKeys().Update(r.Context(), key); err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apiKeyResponse(key))
}

func (s *Server) deleteAPIKey(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	key, err := s.app.Store.APIKeys().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || key.UserID != a.User.ID {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	if err := s.app.Store.APIKeys().Delete(r.Context(), key.ID); err != nil {
		s.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) rotateAPIKey(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	key, secret, err := s.app.Auth.RotateAPIKey(r.Context(), chiURLParam(r, "id"), a.User.ID)
	if err != nil {
		s.storeError(w, err)
		return
	}
	resp := APIKeyCreateResponse{
		ID:          key.ID,
		Name:        key.Name,
		Permissions: key.Permissions,
		Secret:      secret,
		CreatedAt:   ISOTime(key.CreatedAt),
		UpdatedAt:   ISOTime(key.UpdatedAt),
	}
	k := apiKeyResponse(key)
	resp.APIKey = &k
	writeJSON(w, http.StatusCreated, resp)
}

// --- sessions ---

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	sessions, err := s.app.Store.Sessions().ListForUser(r.Context(), a.User.ID)
	if err != nil {
		s.storeError(w, err)
		return
	}
	out := make([]SessionResponse, 0, len(sessions))
	for _, sess := range sessions {
		// API-key callers have no session; guard the comparison.
		current := a.Session != nil && sess.ID == a.Session.ID
		out = append(out, sessionResponse(sess, current))
	}
	writeJSON(w, http.StatusOK, out)
}

func sessionResponse(sess *domain.Session, current bool) SessionResponse {
	var appVersion *string
	if sess.AppVersion != "" {
		v := sess.AppVersion
		appVersion = &v
	}
	return SessionResponse{
		ID:                 sess.ID,
		CreatedAt:          ISOTime(sess.CreatedAt),
		UpdatedAt:          ISOTime(sess.UpdatedAt),
		ExpiresAt:          isoTimePtr(sess.ExpiresAt),
		Current:            current,
		DeviceOS:           sess.DeviceOS,
		DeviceType:         sess.DeviceType,
		AppVersion:         appVersion,
		IsPendingSyncReset: false,
	}
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	sess, err := s.app.Store.Sessions().Get(r.Context(), chiURLParam(r, "id"))
	if err != nil || sess.UserID != a.User.ID {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	_ = s.app.Store.Sessions().Delete(r.Context(), sess.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteAllSessions(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	// Every session of the user except the current one is revoked.
	sessions, err := s.app.Store.Sessions().ListForUser(r.Context(), a.User.ID)
	if err != nil {
		s.storeError(w, err)
		return
	}
	for _, sess := range sessions {
		if a.Session != nil && sess.ID == a.Session.ID {
			continue
		}
		_ = s.app.Store.Sessions().Delete(r.Context(), sess.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}
