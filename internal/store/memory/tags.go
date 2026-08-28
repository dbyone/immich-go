package memory

import (
	"context"
	"sort"
	"strings"
	"time"

	"immich-go/internal/crypto"
	"immich-go/internal/domain"
	"immich-go/internal/store"
)

type tagStore Memory

func (m *Memory) Tags() store.TagStore { return (*tagStore)(m) }

// tagLinks mirrors the tag_assets join table: "tagID|assetID" -> attached.
func tagLinkKey(tagID, assetID string) string { return tagID + "|" + assetID }

func (s *tagStore) Create(ctx context.Context, t *domain.Tag) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tags[t.ID]; ok {
		return store.ErrConflict
	}
	t.UpdateID = s.updateSeq.Add(1)
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	t.UpdatedAt = t.CreatedAt
	s.tags[t.ID] = cloneTag(t)
	return nil
}

func (s *tagStore) Update(ctx context.Context, t *domain.Tag) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tags[t.ID]; !ok {
		return store.ErrNotFound
	}
	t.UpdateID = s.updateSeq.Add(1)
	t.UpdatedAt = time.Now().UTC()
	s.tags[t.ID] = cloneTag(t)
	return nil
}

func (s *tagStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tags[id]; !ok {
		return store.ErrNotFound
	}
	// Remove the tag, its direct children and every link.
	children := map[string]bool{}
	for _, t := range s.tags {
		if t.ParentID != nil && *t.ParentID == id {
			children[t.ID] = true
		}
	}
	delete(s.tags, id)
	for c := range children {
		delete(s.tags, c)
	}
	for k := range s.tagLinks {
		parts := strings.SplitN(k, "|", 2)
		if parts[0] == id || children[parts[0]] {
			delete(s.tagLinks, k)
		}
	}
	uid := s.updateSeq.Add(1)
	s.deletes = append(s.deletes, domain.SyncDelete{Type: "TagDeleteV1", EntityID: id, UpdateID: uid})
	return nil
}

func (s *tagStore) Get(ctx context.Context, id string) (*domain.Tag, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tags[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return cloneTag(t), nil
}

func (s *tagStore) GetByValue(ctx context.Context, userID, value string) (*domain.Tag, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.tags {
		if t.UserID == userID && t.Value == value {
			return cloneTag(t), nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *tagStore) ListForUser(ctx context.Context, userID string) ([]*domain.Tag, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.Tag
	for _, t := range s.tags {
		if t.UserID == userID {
			out = append(out, cloneTag(t))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Value < out[j].Value })
	return out, nil
}

func (s *tagStore) UpsertValue(ctx context.Context, userID, value string) (*domain.Tag, error) {
	value = strings.Trim(value, "/")
	if value == "" {
		return nil, store.ErrNotFound
	}
	var parent *domain.Tag
	for _, part := range strings.Split(value, "/") {
		if part == "" {
			continue
		}
		childValue := part
		if parent != nil {
			childValue = parent.Value + "/" + part
		}
		existing, err := s.GetByValue(ctx, userID, childValue)
		if err == nil {
			parent = existing
			continue
		}
		if err != store.ErrNotFound {
			return nil, err
		}
		tag := &domain.Tag{
			ID:        crypto.NewUUID(),
			UserID:    userID,
			Name:      part,
			Value:     childValue,
			ParentID:  idPtrMem(parent),
			CreatedAt: time.Now().UTC(),
		}
		if err := s.Create(ctx, tag); err != nil {
			return nil, err
		}
		parent = tag
	}
	return parent, nil
}

func idPtrMem(t *domain.Tag) *string {
	if t == nil {
		return nil
	}
	return &t.ID
}

func (s *tagStore) ListForAsset(ctx context.Context, assetID string) ([]*domain.Tag, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.Tag
	for k := range s.tagLinks {
		parts := strings.SplitN(k, "|", 2)
		if parts[1] == assetID {
			if t, ok := s.tags[parts[0]]; ok {
				out = append(out, cloneTag(t))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Value < out[j].Value })
	return out, nil
}

func (s *tagStore) ListForAssets(ctx context.Context, assetIDs []string) (map[string][]*domain.Tag, error) {
	out := map[string][]*domain.Tag{}
	for _, id := range assetIDs {
		tags, err := s.ListForAsset(ctx, id)
		if err != nil {
			return nil, err
		}
		if len(tags) > 0 {
			out[id] = tags
		}
	}
	return out, nil
}

func (s *tagStore) Attach(ctx context.Context, tagID string, assetIDs []string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tags[tagID]; !ok {
		return 0, store.ErrNotFound
	}
	added := 0
	for _, assetID := range assetIDs {
		k := tagLinkKey(tagID, assetID)
		if _, exists := s.tagLinks[k]; !exists {
			s.tagLinks[k] = time.Now().UTC()
			added++
		}
	}
	s.bumpAssetsLocked(assetIDs)
	return added, nil
}

func (s *tagStore) Detach(ctx context.Context, tagID string, assetIDs []string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for _, assetID := range assetIDs {
		k := tagLinkKey(tagID, assetID)
		if _, exists := s.tagLinks[k]; exists {
			delete(s.tagLinks, k)
			removed++
		}
	}
	s.bumpAssetsLocked(assetIDs)
	return removed, nil
}

// bumpAssetsLocked stamps a fresh update_id on the linked assets so sync
// redelivers them. Callers hold the write lock.
func (s *tagStore) bumpAssetsLocked(assetIDs []string) {
	for _, id := range assetIDs {
		if a, ok := s.assets[id]; ok {
			a.UpdateID = s.updateSeq.Add(1)
		}
	}
}

func cloneTag(t *domain.Tag) *domain.Tag {
	out := *t
	if t.ParentID != nil {
		p := *t.ParentID
		out.ParentID = &p
	}
	if t.Color != nil {
		c := *t.Color
		out.Color = &c
	}
	return &out
}
