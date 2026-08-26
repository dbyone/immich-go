// Package store defines the persistence boundary of the server. The
// in-memory implementation ships with this repository; a PostgreSQL
// implementation backed by the same interface is the natural next step
// (see docs/architecture-analysis.md).
package store

import (
	"context"
	"errors"

	"immich-go/internal/domain"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("conflict")
	ErrForbidden = errors.New("forbidden")
)

type UserStore interface {
	Create(ctx context.Context, u *domain.User) error
	Update(ctx context.Context, u *domain.User) error
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (*domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	List(ctx context.Context) ([]*domain.User, error)
	Count(ctx context.Context) (int, error)
}

type SessionStore interface {
	Create(ctx context.Context, s *domain.Session) error
	Update(ctx context.Context, s *domain.Session) error
	Delete(ctx context.Context, id string) error
	DeleteAllForUser(ctx context.Context, userID string) error
	Get(ctx context.Context, id string) (*domain.Session, error)
	GetByTokenHash(ctx context.Context, hash []byte) (*domain.Session, error)
	ListForUser(ctx context.Context, userID string) ([]*domain.Session, error)
}

type APIKeyStore interface {
	Create(ctx context.Context, k *domain.APIKey) error
	Update(ctx context.Context, k *domain.APIKey) error
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (*domain.APIKey, error)
	GetByKeyHash(ctx context.Context, hash []byte) (*domain.APIKey, error)
	ListForUser(ctx context.Context, userID string) ([]*domain.APIKey, error)
}

type AssetStore interface {
	Create(ctx context.Context, a *domain.Asset) error
	Update(ctx context.Context, a *domain.Asset) error
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (*domain.Asset, error)
	// GetByChecksum finds a live (non-trashed) asset of the owner with a
	// matching SHA-1 checksum — the upload de-duplication check.
	GetByChecksum(ctx context.Context, ownerID string, checksum []byte) (*domain.Asset, error)
	List(ctx context.Context) ([]*domain.Asset, error)
	ListForOwner(ctx context.Context, ownerID string) ([]*domain.Asset, error)
}

type AlbumStore interface {
	Create(ctx context.Context, a *domain.Album) error
	Update(ctx context.Context, a *domain.Album) error
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (*domain.Album, error)
	List(ctx context.Context) ([]*domain.Album, error)
	ListForOwner(ctx context.Context, ownerID string) ([]*domain.Album, error)
}

type MemoryStore interface {
	Create(ctx context.Context, m *domain.Memory) error
	Update(ctx context.Context, m *domain.Memory) error
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (*domain.Memory, error)
	ListForOwner(ctx context.Context, ownerID string) ([]*domain.Memory, error)
}

type SyncAckStore interface {
	List(ctx context.Context, userID string) ([]domain.SyncAck, error)
	Put(ctx context.Context, userID string, acks []domain.SyncAck) error
	DeleteTypes(ctx context.Context, userID string, types []string) error
}

type MetadataStore interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string) error
}

type Store interface {
	Users() UserStore
	Sessions() SessionStore
	APIKeys() APIKeyStore
	Assets() AssetStore
	Albums() AlbumStore
	Memories() MemoryStore
	SyncAcks() SyncAckStore
	Metadata() MetadataStore

	// Close releases resources held by the store.
	Close() error
}
