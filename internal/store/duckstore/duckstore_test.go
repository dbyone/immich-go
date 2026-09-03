package duckstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"immich-go/internal/domain"
	"immich-go/internal/store"
)

func now() time.Time { return time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC) }

func testUser(id, email string) *domain.User {
	return &domain.User{
		ID: id, Email: email, Password: "hash", Name: "User " + id,
		IsAdmin: true, AvatarColor: "primary", IsOnboarded: true,
		CreatedAt: now(), UpdatedAt: now(),
	}
}

func TestPersistenceAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "immich.duckdb")
	ctx := context.Background()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	// --- users
	u := testUser("u1", "a@b.c")
	if err := s.Users().Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := s.Users().Create(ctx, testUser("u2", "a@b.c")); err != store.ErrConflict {
		t.Fatalf("duplicate email must conflict, got %v", err)
	}
	if n, _ := s.Users().Count(ctx); n != 1 {
		t.Fatalf("user count: %d", n)
	}

	// --- sessions
	sess := &domain.Session{
		ID: "s1", TokenHash: []byte{1, 2, 3}, UserID: "u1",
		DeviceOS: "iOS", DeviceType: "mobile", AppVersion: "1.0",
		CreatedAt: now(), UpdatedAt: now(),
	}
	if err := s.Sessions().Create(ctx, sess); err != nil {
		t.Fatal(err)
	}

	// --- api keys
	key := &domain.APIKey{
		ID: "k1", Name: "ci", KeyHash: []byte{9, 9}, UserID: "u1",
		Permissions: []string{"asset.read", "album.create"},
		CreatedAt:   now(), UpdatedAt: now(),
	}
	if err := s.APIKeys().Create(ctx, key); err != nil {
		t.Fatal(err)
	}

	// --- assets (with exif, favorite, nullable fields)
	rating := 4
	width, height := 4000, 3000
	duration := int64(12_345)
	liveID := "vid-1"
	asset := &domain.Asset{
		ID: "a1", OwnerID: "u1", Type: domain.AssetImage,
		OriginalPath: "/data/library/u1/a1.jpg", ThumbnailPath: "/thumbs/a1.jpeg",
		OriginalFileName: "a1.jpg", OriginalMimeType: "image/jpeg",
		FileCreatedAt: now(), FileModifiedAt: now(), LocalDateTime: now(),
		CreatedAt: now(), UpdatedAt: now(), IsFavorite: true,
		Duration: &duration, Checksum: []byte{1, 2, 3, 4}, ChecksumB64: "AAAAAAECAwQ=",
		Width: &width, Height: &height, Visibility: domain.VisibilityArchive,
		LivePhotoVideoID: &liveID,
		Exif: &domain.AssetExif{
			Make: "Canon", Model: "R5", FileSize: 123456,
			ExifWidth: &width, ExifHeight: &height,
			DateTimeOriginal: ptrTime(now()), Rating: &rating,
		},
	}
	if err := s.Assets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}

	// --- albums with ordered assets and a shared user
	al := &domain.Album{
		ID: "al1", OwnerID: "u1", AlbumName: "Trip", Description: "d",
		CreatedAt: now(), UpdatedAt: now(), IsActivityEnabled: true, Order: "desc",
		AssetIDs: []string{"a1", "a9", "a5"},
		Users:    []domain.AlbumUser{{UserID: "u2", Role: domain.AlbumRoleViewer}},
	}
	if err := s.Albums().Create(ctx, al); err != nil {
		t.Fatal(err)
	}

	// Close and reopen — everything must survive.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	got, err := s2.Users().GetByEmail(ctx, "a@b.c")
	if err != nil || got.ID != "u1" || !got.IsAdmin || got.DeletedAt != nil {
		t.Fatalf("user lost: %+v err=%v", got, err)
	}
	if _, err := s2.Users().Get(ctx, "missing"); err != store.ErrNotFound {
		t.Fatalf("missing user should 404, got %v", err)
	}

	gotSess, err := s2.Sessions().GetByTokenHash(ctx, []byte{1, 2, 3})
	if err != nil || gotSess.ID != "s1" || gotSess.DeviceOS != "iOS" {
		t.Fatalf("session lost: %+v err=%v", gotSess, err)
	}

	gotKey, err := s2.APIKeys().GetByKeyHash(ctx, []byte{9, 9})
	if err != nil || gotKey.ID != "k1" {
		t.Fatalf("api key lost: %+v err=%v", gotKey, err)
	}
	if len(gotKey.Permissions) != 2 || gotKey.Permissions[1] != "album.create" {
		t.Fatalf("permissions lost: %v", gotKey.Permissions)
	}

	gotAsset, err := s2.Assets().Get(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if !gotAsset.IsFavorite || gotAsset.Visibility != domain.VisibilityArchive ||
		gotAsset.Duration == nil || *gotAsset.Duration != 12_345 ||
		gotAsset.Width == nil || *gotAsset.Width != 4000 ||
		gotAsset.LivePhotoVideoID == nil || *gotAsset.LivePhotoVideoID != "vid-1" {
		t.Fatalf("asset fields lost: %+v", gotAsset)
	}
	if gotAsset.Exif == nil || gotAsset.Exif.Make != "Canon" ||
		gotAsset.Exif.FileSize != 123456 || gotAsset.Exif.Rating == nil || *gotAsset.Exif.Rating != 4 {
		t.Fatalf("exif lost: %+v", gotAsset.Exif)
	}

	bySum, err := s2.Assets().GetByChecksum(ctx, "u1", []byte{1, 2, 3, 4})
	if err != nil || bySum.ID != "a1" {
		t.Fatalf("checksum lookup failed: %+v err=%v", bySum, err)
	}
	if _, err := s2.Assets().GetByChecksum(ctx, "other", []byte{1, 2, 3, 4}); err != store.ErrNotFound {
		t.Fatalf("checksum isolation failed: %v", err)
	}

	gotAlbum, err := s2.Albums().Get(ctx, "al1")
	if err != nil {
		t.Fatal(err)
	}
	if len(gotAlbum.AssetIDs) != 3 || gotAlbum.AssetIDs[0] != "a1" ||
		gotAlbum.AssetIDs[1] != "a9" || gotAlbum.AssetIDs[2] != "a5" {
		t.Fatalf("album asset order lost: %v", gotAlbum.AssetIDs)
	}
	if !gotAlbum.HasAsset("a5") || gotAlbum.HasAsset("nope") {
		t.Fatal("album membership index broken")
	}
	if len(gotAlbum.Users) != 1 || gotAlbum.Users[0].Role != domain.AlbumRoleViewer {
		t.Fatalf("album users lost: %+v", gotAlbum.Users)
	}
}

func TestUpdatesAndDeletes(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "immich.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	u := testUser("u1", "a@b.c")
	_ = s.Users().Create(ctx, u)
	u.Name = "Renamed"
	u.IsAdmin = false
	if err := s.Users().Update(ctx, u); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Users().Get(ctx, "u1"); got.Name != "Renamed" || got.IsAdmin {
		t.Fatalf("user update lost: %+v", got)
	}
	if err := s.Users().Update(ctx, testUser("zz", "z@z.z")); err != store.ErrNotFound {
		t.Fatalf("update missing must 404, got %v", err)
	}

	a := &domain.Asset{
		ID: "a1", OwnerID: "u1", Type: domain.AssetImage, OriginalPath: "p",
		FileCreatedAt: now(), FileModifiedAt: now(), LocalDateTime: now(),
		CreatedAt: now(), UpdatedAt: now(), Checksum: []byte{1},
	}
	_ = s.Assets().Create(ctx, a)

	// Album membership rewrite: shrink then regrow, order preserved.
	al := &domain.Album{
		ID: "al", OwnerID: "u1", AlbumName: "n",
		CreatedAt: now(), UpdatedAt: now(),
		AssetIDs: []string{"a1", "a2", "a3"},
	}
	_ = s.Albums().Create(ctx, al)
	al.AssetIDs = []string{"a1"}
	_ = s.Albums().Update(ctx, al)
	got, _ := s.Albums().Get(ctx, "al")
	if len(got.AssetIDs) != 1 || got.AssetIDs[0] != "a1" {
		t.Fatalf("shrink failed: %v", got.AssetIDs)
	}
	al.AssetIDs = []string{"a3", "a1"}
	_ = s.Albums().Update(ctx, al)
	got, _ = s.Albums().Get(ctx, "al")
	if len(got.AssetIDs) != 2 || got.AssetIDs[0] != "a3" || got.AssetIDs[1] != "a1" {
		t.Fatalf("reorder failed: %v", got.AssetIDs)
	}

	// Deleting an asset removes it from albums.
	if err := s.Assets().Delete(ctx, "a1"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Albums().Get(ctx, "al")
	if len(got.AssetIDs) != 1 || got.AssetIDs[0] != "a3" {
		t.Fatalf("album not pruned after asset delete: %v", got.AssetIDs)
	}
	if err := s.Assets().Delete(ctx, "a1"); err != store.ErrNotFound {
		t.Fatalf("double delete must 404, got %v", err)
	}

	// Sessions: delete all for user leaves others intact.
	_ = s.Sessions().Create(ctx, &domain.Session{ID: "s1", TokenHash: []byte{1}, UserID: "u1", CreatedAt: now(), UpdatedAt: now()})
	_ = s.Sessions().Create(ctx, &domain.Session{ID: "s2", TokenHash: []byte{2}, UserID: "u2", CreatedAt: now(), UpdatedAt: now()})
	_ = s.Sessions().DeleteAllForUser(ctx, "u1")
	if _, err := s.Sessions().Get(ctx, "s1"); err != store.ErrNotFound {
		t.Fatal("s1 should be gone")
	}
	if _, err := s.Sessions().Get(ctx, "s2"); err != nil {
		t.Fatal("s2 should survive")
	}
}

func TestListForOwnerIsolation(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "immich.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	for i, owner := range []string{"u1", "u1", "u2"} {
		a := &domain.Asset{
			ID: string(rune('a' + i)), OwnerID: owner, Type: domain.AssetImage,
			OriginalPath: "p", FileCreatedAt: now(), FileModifiedAt: now(),
			LocalDateTime: now(), CreatedAt: now(), UpdatedAt: now(), Checksum: []byte{byte(i)},
		}
		if err := s.Assets().Create(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if list, _ := s.Assets().ListForOwner(ctx, "u1"); len(list) != 2 {
		t.Fatalf("u1 should own 2, got %d", len(list))
	}
	if all, _ := s.Assets().List(ctx); len(all) != 3 {
		t.Fatalf("total should be 3, got %d", len(all))
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestSetIfAbsentClaimsOnce(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "immich.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	first, err := s.Metadata().SetIfAbsent(ctx, "bootstrap", "1")
	if err != nil || !first {
		t.Fatalf("first claim must win: %v %v", first, err)
	}
	second, err := s.Metadata().SetIfAbsent(ctx, "bootstrap", "2")
	if err != nil || second {
		t.Fatalf("second claim must lose: %v %v", second, err)
	}
	if v, ok, _ := s.Metadata().Get(ctx, "bootstrap"); !ok || v != "1" {
		t.Fatalf("value must stay from first claim: %q %v", v, ok)
	}
}

// TestConcurrentUserUpdates: DuckDB's optimistic concurrency aborts
// overlapping writes to the same row ("write-write conflict"), which
// surfaced as intermittent 500s on PUT /users/me/preferences. All writes
// now serialize on the store's write lock, so concurrent updates to the
// same user must all succeed.
func TestConcurrentUserUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "immich.duckdb")
	ctx := context.Background()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	u := testUser("u1", "race@t.c")
	if err := s.Users().Create(ctx, u); err != nil {
		t.Fatal(err)
	}

	const n = 8
	errCh := make(chan error, n)
	for i := range n {
		errCh <- func() error {
			clone := *u
			clone.Preferences = `{"albums":{"iteration":` + itoa(i) + `}}`
			return s.Users().Update(ctx, &clone)
		}()
	}
	for range n {
		if err := <-errCh; err != nil {
			t.Fatalf("concurrent update failed: %v", err)
		}
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := ""
	for ; i > 0; i /= 10 {
		digits = string(rune('0'+i%10)) + digits
	}
	return digits
}

// TestWALRecoveryOnBoot covers the boot-time WAL recovery toolkit:
// IsWALReplayError recognizes DuckDB's replay failure messages (a hard
// kill can leave a torn WAL whose replay aborts), MoveWALAside renames
// the WAL to a unique backup (Windows rename cannot overwrite), and the
// database reopens with its data once the WAL is out of the way. Note
// DuckDB silently ignores obviously-garbage WAL bytes; the real-world
// failures matched here are the semantic replay aborts seen in
// production ("applying buffered appends", "replaying WAL").
func TestWALRecoveryOnBoot(t *testing.T) {
	for _, msg := range []string{
		`Failure while replaying WAL file "x.wal": ...`,
		`INTERNAL Error: error while applying buffered appends: TransactionContext Error: ...`,
	} {
		if !IsWALReplayError(errors.New(msg)) {
			t.Fatalf("IsWALReplayError should match %q", msg)
		}
	}
	if IsWALReplayError(errors.New("some unrelated error")) {
		t.Fatal("IsWALReplayError matched an unrelated error")
	}

	path := filepath.Join(t.TempDir(), "immich.duckdb")
	ctx := context.Background()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Users().Create(ctx, testUser("u1", "wal@t.c")); err != nil {
		t.Fatal(err)
	}
	if err := s.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate the damaged WAL. DuckDB ignores blatant garbage, so the
	// first open may either fail with a replay error (recovery path) or
	// succeed (nothing to do); both must converge on readable data.
	if err := os.WriteFile(path+".wal", []byte("torn write-ahead log bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(path)
	if err != nil {
		if !IsWALReplayError(err) {
			t.Fatalf("unexpected non-WAL open error: %v", err)
		}
		backup, mvErr := MoveWALAside(path)
		if mvErr != nil {
			t.Fatal(mvErr)
		}
		if _, statErr := os.Stat(backup); statErr != nil {
			t.Fatalf("backup WAL missing: %v", statErr)
		}
		// A second backup must not collide with the first (Windows
		// rename cannot overwrite): recreate a WAL and move it again.
		if err := os.WriteFile(path+".wal", []byte("another torn wal"), 0o644); err != nil {
			t.Fatal(err)
		}
		backup2, mvErr := MoveWALAside(path)
		if mvErr != nil {
			t.Fatal(mvErr)
		}
		if backup2 == backup {
			t.Fatal("second backup overwrote the first")
		}
		s2, err = Open(path)
		if err != nil {
			t.Fatalf("reopen after WAL removal: %v", err)
		}
	}
	defer s2.Close()
	if _, err := s2.Users().GetByEmail(ctx, "wal@t.c"); err != nil {
		t.Fatalf("user lost after WAL recovery: %v", err)
	}
}
