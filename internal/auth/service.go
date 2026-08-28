// Package auth implements the Immich session model: opaque bearer tokens
// (SHA-256 at rest), bcrypt passwords and hashed API keys, with the same
// credential precedence the upstream AuthGuard uses.
package auth

import (
	"context"
	"net/http"
	"time"

	"immich-go/internal/crypto"
	"immich-go/internal/domain"
	"immich-go/internal/store"
)

// Header/cookie/query names — mirrors of ImmichHeader/ImmichCookie/ImmichQuery.
const (
	HeaderSessionToken = "x-immich-session-token"
	HeaderUserToken    = "x-immich-user-token"
	HeaderAPIKey       = "x-api-key"
	QuerySessionKey    = "sessionKey"
	QueryAPIKey        = "apiKey"
	CookieAccessToken  = "immich_access_token"

	PermissionAll = "all"
)

// dummyHash is a real bcrypt hash used to equalize login timing for
// unknown users (upstream LOGIN_DUMMY_HASH serves the same purpose).
var dummyHash, _ = crypto.HashPassword("invalid-password-placeholder")

type Service struct {
	store store.Store
	ttl   time.Duration // 0 = sessions never expire
}

func NewService(s store.Store) *Service { return &Service{store: s} }

// SetSessionTTL overrides the session lifetime (used from config).
func (s *Service) SetSessionTTL(ttl time.Duration) { s.ttl = ttl }

type LoginInput struct {
	Email      string
	Password   string
	DeviceOS   string
	DeviceType string
	AppVersion string
}

type LoginResult struct {
	Token      string
	SessionID  string
	User       *domain.User
	NewSession *domain.Session
}

// Login verifies credentials and opens a session, returning the opaque
// access token exactly once.
func (s *Service) Login(ctx context.Context, in LoginInput) (*LoginResult, error) {
	user, err := s.store.Users().GetByEmail(ctx, in.Email)
	if err != nil {
		// Burn the same bcrypt work as a real comparison.
		crypto.ComparePassword(in.Password, dummyHash)
		return nil, store.ErrForbidden
	}
	if !crypto.ComparePassword(in.Password, user.Password) {
		return nil, store.ErrForbidden
	}

	token := crypto.RandomToken()
	now := time.Now().UTC()
	sess := &domain.Session{
		ID:         crypto.NewUUID(),
		TokenHash:  crypto.HashToken(token),
		UserID:     user.ID,
		DeviceOS:   in.DeviceOS,
		DeviceType: in.DeviceType,
		AppVersion: in.AppVersion,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if s.ttl > 0 {
		expires := now.Add(s.ttl)
		sess.ExpiresAt = &expires
	}
	if err := s.store.Sessions().Create(ctx, sess); err != nil {
		return nil, err
	}
	return &LoginResult{Token: token, SessionID: sess.ID, User: user, NewSession: sess}, nil
}

// Logout revokes one session by token.
func (s *Service) Logout(ctx context.Context, token string) error {
	sess, err := s.store.Sessions().GetByTokenHash(ctx, crypto.HashToken(token))
	if err != nil {
		return nil // logout is idempotent
	}
	return s.store.Sessions().Delete(ctx, sess.ID)
}

func (s *Service) CreateAPIKey(ctx context.Context, userID, name string, permissions []string) (*domain.APIKey, string, error) {
	if len(permissions) == 0 {
		permissions = []string{PermissionAll}
	}
	secret := crypto.RandomToken()
	key := &domain.APIKey{
		ID:          crypto.NewUUID(),
		Name:        name,
		KeyHash:     crypto.HashToken(secret),
		UserID:      userID,
		Permissions: permissions,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := s.store.APIKeys().Create(ctx, key); err != nil {
		return nil, "", err
	}
	return key, secret, nil
}

// RotateAPIKey replaces the secret of an existing key.
func (s *Service) RotateAPIKey(ctx context.Context, id, userID string) (*domain.APIKey, string, error) {
	key, err := s.store.APIKeys().Get(ctx, id)
	if err != nil || key.UserID != userID {
		return nil, "", store.ErrNotFound
	}
	secret := crypto.RandomToken()
	key.KeyHash = crypto.HashToken(secret)
	key.UpdatedAt = time.Now().UTC()
	if err := s.store.APIKeys().Update(ctx, key); err != nil {
		return nil, "", err
	}
	return key, secret, nil
}

// AuthContext is the resolved caller identity attached to requests.
type AuthContext struct {
	User        *domain.User
	Session     *domain.Session
	APIKey      *domain.APIKey
	Permissions map[string]bool
	IsAPIKey    bool
	Token       string
}

// HasPermission reports whether the caller may perform a scoped action.
// Session callers hold every permission; API keys are limited to their
// granted list ("all" widens the scope).
func (a *AuthContext) HasPermission(p string) bool {
	if !a.IsAPIKey {
		return true
	}
	return a.Permissions[PermissionAll] || a.Permissions[p]
}

type ctxKey struct{}

// WithContext attaches the resolved auth context to r's context.
func (a *AuthContext) WithContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKey{}, a)
}

// FromRequest resolves the caller previously stored by the auth middleware.
func FromRequest(r *http.Request) *AuthContext {
	if a, ok := r.Context().Value(ctxKey{}).(*AuthContext); ok {
		return a
	}
	return nil
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && (h[:7] == "Bearer " || h[:7] == "bearer ") {
		return h[7:], true
	}
	return "", false
}

// extractToken applies the credential precedence of AuthService.validate:
// session token headers/query/bearer/cookie first, then API key.
func extractToken(r *http.Request) (session string, apiKey string) {
	if v := r.Header.Get(HeaderUserToken); v != "" {
		session = v
	} else if v := r.Header.Get(HeaderSessionToken); v != "" {
		session = v
	} else if v := r.URL.Query().Get(QuerySessionKey); v != "" {
		session = v
	} else if v, ok := bearerToken(r); ok {
		session = v
	} else if c, err := r.Cookie(CookieAccessToken); err == nil && c.Value != "" {
		session = c.Value
	}

	if v := r.Header.Get(HeaderAPIKey); v != "" {
		apiKey = v
	} else if v := r.URL.Query().Get(QueryAPIKey); v != "" {
		apiKey = v
	}
	return session, apiKey
}

// Authenticate resolves the caller of an incoming request.
func (s *Service) Authenticate(ctx context.Context, r *http.Request) (*AuthContext, error) {
	sessionToken, apiKey := extractToken(r)

	if sessionToken != "" {
		sess, err := s.store.Sessions().GetByTokenHash(ctx, crypto.HashToken(sessionToken))
		if err == nil {
			if sess.ExpiresAt != nil && sess.ExpiresAt.Before(time.Now()) {
				return nil, store.ErrForbidden
			}
			user, err := s.store.Users().Get(ctx, sess.UserID)
			if err != nil || user.DeletedAt != nil {
				return nil, store.ErrForbidden
			}
			return &AuthContext{
				User:    user,
				Session: sess,
				Token:   sessionToken,
			}, nil
		}
	}

	if apiKey != "" {
		key, err := s.store.APIKeys().GetByKeyHash(ctx, crypto.HashToken(apiKey))
		if err == nil {
			user, err := s.store.Users().Get(ctx, key.UserID)
			if err != nil || user.DeletedAt != nil {
				return nil, store.ErrForbidden
			}
			perms := map[string]bool{}
			for _, p := range key.Permissions {
				perms[p] = true
			}
			return &AuthContext{
				User:        user,
				APIKey:      key,
				Permissions: perms,
				IsAPIKey:    true,
				Token:       apiKey,
			}, nil
		}
	}

	return nil, store.ErrForbidden
}
