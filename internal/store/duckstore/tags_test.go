package duckstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"immich-go/internal/domain"
	"immich-go/internal/store"
)

func newTagsTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "tags.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Users().Create(context.Background(), testUser("u1", "tags@t.c")); err != nil {
		t.Fatal(err)
	}
	return s
}

func tagsAsset(id string) *domain.Asset {
	n := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	return &domain.Asset{
		ID: id, OwnerID: "u1", Type: domain.AssetImage,
		OriginalPath: "upload/" + id + ".jpg", OriginalFileName: id + ".jpg",
		FileCreatedAt: n, FileModifiedAt: n, LocalDateTime: n,
		CreatedAt: n, UpdatedAt: n, Visibility: domain.VisibilityTimeline,
		Checksum: []byte("sum-" + id), ChecksumB64: "sum-" + id,
	}
}

func TestTagStoreUpsertHierarchy(t *testing.T) {
	s := newTagsTestStore(t)
	ctx := context.Background()

	leaf, err := s.Tags().UpsertValue(ctx, "u1", "场景/海滩")
	if err != nil {
		t.Fatal(err)
	}
	if leaf.Value != "场景/海滩" || leaf.Name != "海滩" || leaf.ParentID == nil {
		t.Fatalf("leaf = %+v", leaf)
	}
	parent, err := s.Tags().Get(ctx, *leaf.ParentID)
	if err != nil || parent.Value != "场景" {
		t.Fatalf("parent = %+v err = %v", parent, err)
	}
	// Idempotent second upsert returns the same row.
	again, err := s.Tags().UpsertValue(ctx, "u1", "场景/海滩")
	if err != nil || again.ID != leaf.ID {
		t.Fatalf("re-upsert = %+v err = %v", again, err)
	}
	// Another user's namespace is separate.
	other, err := s.Tags().UpsertValue(ctx, "ghost", "场景/海滩")
	if err != nil || other.ID == leaf.ID {
		t.Fatalf("cross-user leak: %+v err = %v", other, err)
	}
}

func TestTagStoreAttachDetachAndSyncBump(t *testing.T) {
	s := newTagsTestStore(t)
	ctx := context.Background()

	if err := s.Assets().Create(ctx, tagsAsset("a1")); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Assets().Get(ctx, "a1")

	tag, err := s.Tags().UpsertValue(ctx, "u1", "场景/雪景")
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.Tags().Attach(ctx, tag.ID, []string{"a1"})
	if err != nil || n != 1 {
		t.Fatalf("attach = %d err = %v", n, err)
	}
	// Re-attach is a no-op.
	n, _ = s.Tags().Attach(ctx, tag.ID, []string{"a1"})
	if n != 0 {
		t.Fatalf("re-attach added %d", n)
	}

	// Tag links bump the asset's sync watermark.
	after, _ := s.Assets().Get(ctx, "a1")
	if after.UpdateID <= before.UpdateID {
		t.Fatalf("update_id not bumped: %d <= %d", after.UpdateID, before.UpdateID)
	}

	tags, err := s.Tags().ListForAsset(ctx, "a1")
	if err != nil || len(tags) != 1 || tags[0].Value != "场景/雪景" {
		t.Fatalf("list for asset = %+v err = %v", tags, err)
	}

	// Bulk load.
	bulk, err := s.Tags().ListForAssets(ctx, []string{"a1", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(bulk["a1"]) != 1 {
		t.Fatalf("bulk = %+v", bulk)
	}
	if _, ok := bulk["missing"]; ok {
		t.Fatal("missing asset must be absent from bulk map")
	}

	n, err = s.Tags().Detach(ctx, tag.ID, []string{"a1"})
	if err != nil || n != 1 {
		t.Fatalf("detach = %d err = %v", n, err)
	}
	tags, _ = s.Tags().ListForAsset(ctx, "a1")
	if len(tags) != 0 {
		t.Fatalf("tags after detach = %+v", tags)
	}
}

func TestTagStoreDelete(t *testing.T) {
	s := newTagsTestStore(t)
	ctx := context.Background()

	leaf, err := s.Tags().UpsertValue(ctx, "u1", "旅行/2026")
	if err != nil {
		t.Fatal(err)
	}
	all, _ := s.Tags().ListForUser(ctx, "u1")
	if len(all) != 2 {
		t.Fatalf("want 旅行 + 旅行/2026, got %+v", all)
	}
	// Deleting the parent cascades to its children.
	if err := s.Tags().Delete(ctx, *leaf.ParentID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Tags().Get(ctx, leaf.ID); err != store.ErrNotFound {
		t.Fatalf("child must go with the parent, got %v", err)
	}
	all, _ = s.Tags().ListForUser(ctx, "u1")
	if len(all) != 0 {
		t.Fatalf("delete must cascade, got %+v", all)
	}

	// Deleting a leaf keeps its parent.
	leaf2, err := s.Tags().UpsertValue(ctx, "u1", "旅行/2027")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tags().Delete(ctx, leaf2.ID); err != nil {
		t.Fatal(err)
	}
	all, _ = s.Tags().ListForUser(ctx, "u1")
	if len(all) != 1 || all[0].Value != "旅行" {
		t.Fatalf("parent must survive leaf delete, got %+v", all)
	}
}
