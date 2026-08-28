package memory

import (
	"context"
	"testing"
	"time"

	"immich-go/internal/domain"
	"immich-go/internal/store"
)

func TestMemoryTags(t *testing.T) {
	m := New()
	ctx := context.Background()

	leaf, err := m.Tags().UpsertValue(ctx, "u1", "场景/海滩")
	if err != nil {
		t.Fatal(err)
	}
	if leaf.Value != "场景/海滩" || leaf.ParentID == nil {
		t.Fatalf("leaf = %+v", leaf)
	}
	parent, err := m.Tags().Get(ctx, *leaf.ParentID)
	if err != nil || parent.Value != "场景" {
		t.Fatalf("parent = %+v err = %v", parent, err)
	}

	n := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	asset := &domain.Asset{ID: "a1", OwnerID: "u1", Type: domain.AssetImage,
		FileCreatedAt: n, FileModifiedAt: n, LocalDateTime: n, CreatedAt: n, UpdatedAt: n}
	if err := m.Assets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}

	if added, err := m.Tags().Attach(ctx, leaf.ID, []string{"a1"}); err != nil || added != 1 {
		t.Fatalf("attach = %d err = %v", added, err)
	}
	tags, err := m.Tags().ListForAsset(ctx, "a1")
	if err != nil || len(tags) != 1 {
		t.Fatalf("list = %+v err = %v", tags, err)
	}
	bulk, err := m.Tags().ListForAssets(ctx, []string{"a1"})
	if err != nil || len(bulk["a1"]) != 1 {
		t.Fatalf("bulk = %+v err = %v", bulk, err)
	}

	if removed, err := m.Tags().Detach(ctx, leaf.ID, []string{"a1"}); err != nil || removed != 1 {
		t.Fatalf("detach = %d err = %v", removed, err)
	}
	if err := m.Tags().Delete(ctx, parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Tags().Get(ctx, leaf.ID); err != store.ErrNotFound {
		t.Fatalf("child must cascade with parent, got %v", err)
	}
}
