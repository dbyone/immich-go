// Package memory provides a concurrency-safe in-memory Store. It exists so
// the server runs (and the test-suite passes) without external services;
// persistence across restarts requires the PostgreSQL implementation.
package memory

import (
	"context"
	"sync"

	"immich-go/internal/domain"
	"immich-go/internal/store"
)

type Memory struct {
	mu       sync.RWMutex
	users    map[string]*domain.User
	sessions map[string]*domain.Session
	apiKeys  map[string]*domain.APIKey
	assets   map[string]*domain.Asset
	albums   map[string]*domain.Album
}

func New() *Memory {
	return &Memory{
		users:    map[string]*domain.User{},
		sessions: map[string]*domain.Session{},
		apiKeys:  map[string]*domain.APIKey{},
		assets:   map[string]*domain.Asset{},
		albums:   map[string]*domain.Album{},
	}
}

func (m *Memory) Close() error { return nil }

func (m *Memory) Users() store.UserStore         { return (*userStore)(m) }
func (m *Memory) Sessions() store.SessionStore   { return (*sessionStore)(m) }
func (m *Memory) APIKeys() store.APIKeyStore     { return (*apiKeyStore)(m) }
func (m *Memory) Assets() store.AssetStore       { return (*assetStore)(m) }
func (m *Memory) Albums() store.AlbumStore       { return (*albumStore)(m) }

// clone helpers keep references handed out of the store independent of the
// stored copies, mirroring how a database returns fresh rows.

func cloneUser(u *domain.User) *domain.User       { c := *u; return &c }
func cloneSession(s *domain.Session) *domain.Session { c := *s; return &c }
func cloneKey(k *domain.APIKey) *domain.APIKey    { c := *k; c.Permissions = append([]string(nil), k.Permissions...); return &c }

func cloneAsset(a *domain.Asset) *domain.Asset {
	c := *a
	if a.Exif != nil {
		e := *a.Exif
		c.Exif = &e
	}
	c.Faces = append([]domain.Face(nil), a.Faces...)
	return &c
}

func cloneAlbum(a *domain.Album) *domain.Album {
	c := *a
	c.AssetIDs = append([]string(nil), a.AssetIDs...)
	c.Users = append([]domain.AlbumUser(nil), a.Users...)
	c.AssetIndex = nil
	return &c
}

func rebuildIndex(a *domain.Album) {
	a.AssetIndex = make(map[string]bool, len(a.AssetIDs))
	for _, id := range a.AssetIDs {
		a.AssetIndex[id] = true
	}
}

// ---- users ----

type userStore Memory

func (s *userStore) Create(_ context.Context, u *domain.User) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.users {
		if existing.Email == u.Email {
			return store.ErrConflict
		}
	}
	m.users[u.ID] = cloneUser(u)
	return nil
}

func (s *userStore) Update(_ context.Context, u *domain.User) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[u.ID]; !ok {
		return store.ErrNotFound
	}
	m.users[u.ID] = cloneUser(u)
	return nil
}

func (s *userStore) Delete(_ context.Context, id string) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[id]; !ok {
		return store.ErrNotFound
	}
	delete(m.users, id)
	return nil
}

func (s *userStore) Get(_ context.Context, id string) (*domain.User, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return cloneUser(u), nil
}

func (s *userStore) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, u := range m.users {
		if u.Email == email {
			return cloneUser(u), nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *userStore) List(_ context.Context) ([]*domain.User, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*domain.User, 0, len(m.users))
	for _, u := range m.users {
		out = append(out, cloneUser(u))
	}
	return out, nil
}

func (s *userStore) Count(_ context.Context) (int, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.users), nil
}

// ---- sessions ----

type sessionStore Memory

func (s *sessionStore) Create(_ context.Context, sess *domain.Session) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sess.ID] = cloneSession(sess)
	return nil
}

func (s *sessionStore) Update(_ context.Context, sess *domain.Session) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sess.ID]; !ok {
		return store.ErrNotFound
	}
	m.sessions[sess.ID] = cloneSession(sess)
	return nil
}

func (s *sessionStore) Delete(_ context.Context, id string) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
	return nil
}

func (s *sessionStore) DeleteAllForUser(_ context.Context, userID string) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, sess := range m.sessions {
		if sess.UserID == userID {
			delete(m.sessions, id)
		}
	}
	return nil
}

func (s *sessionStore) Get(_ context.Context, id string) (*domain.Session, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	sess, ok := m.sessions[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return cloneSession(sess), nil
}

func (s *sessionStore) GetByTokenHash(_ context.Context, hash []byte) (*domain.Session, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, sess := range m.sessions {
		if string(sess.TokenHash) == string(hash) {
			return cloneSession(sess), nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *sessionStore) ListForUser(_ context.Context, userID string) ([]*domain.Session, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*domain.Session
	for _, sess := range m.sessions {
		if sess.UserID == userID {
			out = append(out, cloneSession(sess))
		}
	}
	return out, nil
}

// ---- api keys ----

type apiKeyStore Memory

func (s *apiKeyStore) Create(_ context.Context, k *domain.APIKey) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.apiKeys[k.ID] = cloneKey(k)
	return nil
}

func (s *apiKeyStore) Update(_ context.Context, k *domain.APIKey) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.apiKeys[k.ID]; !ok {
		return store.ErrNotFound
	}
	m.apiKeys[k.ID] = cloneKey(k)
	return nil
}

func (s *apiKeyStore) Delete(_ context.Context, id string) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.apiKeys, id)
	return nil
}

func (s *apiKeyStore) Get(_ context.Context, id string) (*domain.APIKey, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	k, ok := m.apiKeys[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return cloneKey(k), nil
}

func (s *apiKeyStore) GetByKeyHash(_ context.Context, hash []byte) (*domain.APIKey, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, k := range m.apiKeys {
		if string(k.KeyHash) == string(hash) {
			return cloneKey(k), nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *apiKeyStore) ListForUser(_ context.Context, userID string) ([]*domain.APIKey, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*domain.APIKey
	for _, k := range m.apiKeys {
		if k.UserID == userID {
			out = append(out, cloneKey(k))
		}
	}
	return out, nil
}

// ---- assets ----

type assetStore Memory

func (s *assetStore) Create(_ context.Context, a *domain.Asset) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.assets[a.ID] = cloneAsset(a)
	return nil
}

func (s *assetStore) Update(_ context.Context, a *domain.Asset) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.assets[a.ID]; !ok {
		return store.ErrNotFound
	}
	m.assets[a.ID] = cloneAsset(a)
	return nil
}

func (s *assetStore) Delete(_ context.Context, id string) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.assets, id)
	return nil
}

func (s *assetStore) Get(_ context.Context, id string) (*domain.Asset, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.assets[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return cloneAsset(a), nil
}

func (s *assetStore) GetByChecksum(_ context.Context, ownerID string, checksum []byte) (*domain.Asset, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, a := range m.assets {
		if a.OwnerID == ownerID && a.DeletedAt == nil && string(a.Checksum) == string(checksum) {
			return cloneAsset(a), nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *assetStore) List(_ context.Context) ([]*domain.Asset, error) {
	return s.listFiltered(func(*domain.Asset) bool { return true })
}

func (s *assetStore) ListForOwner(_ context.Context, ownerID string) ([]*domain.Asset, error) {
	return s.listFiltered(func(a *domain.Asset) bool { return a.OwnerID == ownerID })
}

func (s *assetStore) listFiltered(match func(*domain.Asset) bool) ([]*domain.Asset, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*domain.Asset, 0, len(m.assets))
	for _, a := range m.assets {
		if match(a) {
			out = append(out, cloneAsset(a))
		}
	}
	return out, nil
}

// ---- albums ----

type albumStore Memory

func (s *albumStore) Create(_ context.Context, a *domain.Album) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	stored := cloneAlbum(a)
	rebuildIndex(stored)
	m.albums[a.ID] = stored
	return nil
}

func (s *albumStore) Update(_ context.Context, a *domain.Album) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.albums[a.ID]; !ok {
		return store.ErrNotFound
	}
	stored := cloneAlbum(a)
	rebuildIndex(stored)
	m.albums[a.ID] = stored
	return nil
}

func (s *albumStore) Delete(_ context.Context, id string) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.albums, id)
	return nil
}

func (s *albumStore) Get(_ context.Context, id string) (*domain.Album, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.albums[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	out := cloneAlbum(a)
	rebuildIndex(out)
	return out, nil
}

func (s *albumStore) List(_ context.Context) ([]*domain.Album, error) {
	return s.listFiltered(func(*domain.Album) bool { return true })
}

func (s *albumStore) ListForOwner(_ context.Context, ownerID string) ([]*domain.Album, error) {
	return s.listFiltered(func(a *domain.Album) bool { return a.OwnerID == ownerID })
}

func (s *albumStore) listFiltered(match func(*domain.Album) bool) ([]*domain.Album, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*domain.Album
	for _, a := range m.albums {
		if match(a) {
			c := cloneAlbum(a)
			rebuildIndex(c)
			out = append(out, c)
		}
	}
	return out, nil
}
