package memory

import (
	"context"
	"strings"
	"sync/atomic"

	"immich-go/internal/crypto"
	"immich-go/internal/domain"
	"immich-go/internal/store"
)

// nextUpdateID: in-memory backends keep their own monotonic counter.
func (m *Memory) nextUpdateID() int64 { return m.updateSeq.Add(1) }

func cloneStack(s *domain.Stack) *domain.Stack {
	c := *s
	c.AssetIDs = append([]string(nil), s.AssetIDs...)
	return &c
}

// ---- stacks ----

type stackStore Memory

func (s *stackStore) Create(_ context.Context, st *domain.Stack) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stacks[st.ID] = cloneStack(st)
	return nil
}

func (s *stackStore) Update(_ context.Context, st *domain.Stack) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.stacks[st.ID]; !ok {
		return store.ErrNotFound
	}
	m.stacks[st.ID] = cloneStack(st)
	return nil
}

func (s *stackStore) Delete(_ context.Context, id string) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.stacks[id]; !ok {
		return store.ErrNotFound
	}
	delete(m.stacks, id)
	m.recordDeleteMem("StackDeleteV1", id)
	return nil
}

func (s *stackStore) Get(_ context.Context, id string) (*domain.Stack, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.stacks[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return cloneStack(st), nil
}

func (s *stackStore) ListForOwner(_ context.Context, ownerID string) ([]*domain.Stack, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*domain.Stack
	for _, st := range m.stacks {
		if st.OwnerID == ownerID {
			out = append(out, cloneStack(st))
		}
	}
	return out, nil
}

// ---- partners ----

type partnerStore Memory

func (s *partnerStore) Create(_ context.Context, p *domain.Partner) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.partners {
		if existing.OwnerID == p.OwnerID && existing.UserID == p.UserID {
			return store.ErrConflict
		}
	}
	c := *p
	m.partners[p.ID] = &c
	return nil
}

func (s *partnerStore) Update(_ context.Context, p *domain.Partner) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.partners[p.ID]; !ok {
		return store.ErrNotFound
	}
	c := *p
	m.partners[p.ID] = &c
	return nil
}

func (s *partnerStore) Delete(_ context.Context, id string) error {
	m := (*Memory)(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.partners[id]; !ok {
		return store.ErrNotFound
	}
	delete(m.partners, id)
	return nil
}

func (s *partnerStore) Get(_ context.Context, id string) (*domain.Partner, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.partners[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	c := *p
	return &c, nil
}

func (s *partnerStore) ListSharedBy(_ context.Context, userID string) ([]*domain.Partner, error) {
	return s.list("owner_id", userID)
}

func (s *partnerStore) ListSharedWith(_ context.Context, userID string) ([]*domain.Partner, error) {
	return s.list("user_id", userID)
}

func (s *partnerStore) list(col, userID string) ([]*domain.Partner, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*domain.Partner
	for _, p := range m.partners {
		match := p.OwnerID == userID
		if col == "user_id" {
			match = p.UserID == userID
		}
		if match {
			c := *p
			out = append(out, &c)
		}
	}
	return out, nil
}

// ---- incremental sync ----

type syncStore Memory

func (s *syncStore) AssetsSince(_ context.Context, ownerID string, since int64, limit int) ([]*domain.Asset, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*domain.Asset
	for _, a := range m.assets {
		if a.OwnerID == ownerID && a.UpdateID > since && a.DeletedAt == nil {
			if len(out) < limit {
				out = append(out, cloneAsset(a))
			}
		}
	}
	return out, nil
}

func (s *syncStore) AlbumsSince(_ context.Context, ownerID string, since int64, limit int) ([]*domain.Album, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*domain.Album
	for _, al := range m.albums {
		if al.OwnerID == ownerID && al.UpdateID > since {
			if len(out) < limit {
				c := cloneAlbum(al)
				rebuildIndex(c)
				out = append(out, c)
			}
		}
	}
	return out, nil
}

func (s *syncStore) UsersSince(_ context.Context, since int64, limit int) ([]*domain.User, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*domain.User
	for _, u := range m.users {
		if u.UpdateID > since {
			if len(out) < limit {
				out = append(out, cloneUser(u))
			}
		}
	}
	return out, nil
}

func (s *syncStore) DeletesSince(_ context.Context, types []string, since int64, limit int) ([]domain.SyncDelete, error) {
	m := (*Memory)(s)
	m.mu.RLock()
	defer m.mu.RUnlock()
	want := map[string]bool{}
	for _, t := range types {
		want[t] = true
	}
	var out []domain.SyncDelete
	for _, d := range m.deletes {
		if d.UpdateID > since && want[d.Type] {
			if len(out) < limit {
				out = append(out, d)
			}
		}
	}
	return out, nil
}

func (s *syncStore) LatestUpdateID(_ context.Context) (int64, error) {
	m := (*Memory)(s)
	return m.updateSeq.Load(), nil
}

func (m *Memory) recordDeleteMem(entityType, entityID string) {
	m.deletes = append(m.deletes, domain.SyncDelete{
		Type:     entityType,
		EntityID: entityID,
		UpdateID: m.nextUpdateID(),
	})
}

// stampUpdate bumps the watermark; called by the memory entity stores.
var _ = atomic.Int64{}
var _ = strings.SplitN
var _ = crypto.NewUUID
