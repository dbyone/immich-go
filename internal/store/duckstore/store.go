// Package duckstore persists the server's entity metadata (users,
// sessions, API keys, assets, albums) in DuckDB — the same embedded
// database that holds the vector store. One file (immich.duckdb) is the
// complete durable state of the server; no external database is required.
package duckstore

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"

	"immich-go/internal/store"
)

// Store implements store.Store on top of a DuckDB database. It can share
// the *sql.DB with the vector store (New) or own a dedicated file (Open).
//
// db is the single-writer connection pool every mutation goes through.
// ro is a (possibly identical) pool serving reads: for file-backed
// databases it may hold several concurrent snapshot readers (DuckDB
// connections share one database instance per path), so logins and
// listings stop queueing behind long write transactions. For :memory:
// databases ro must be the same pool — separate opens would create
// separate empty databases.
type Store struct {
	db   *sql.DB
	ro   *sql.DB
	owns bool
	// writeMu serializes every mutation: DuckDB's optimistic concurrency
	// aborts overlapping write transactions (write-write conflict).
	writeMu sync.Mutex
	// roMax caches the reader-pool size so conflict cleanup can restore it.
	roMax int
}

// OpenDB opens (or creates) a DuckDB database file constrained to a
// single connection, matching DuckDB's embedded single-writer model.
func OpenDB(path string) (*sql.DB, error) {
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// Open opens its own database file and closes it on Close.
func Open(path string) (*Store, error) {
	db, err := OpenDB(path)
	if err != nil {
		return nil, err
	}
	s, err := New(db)
	if err != nil {
		db.Close()
		return nil, err
	}
	s.owns = true
	return s, nil
}

// New attaches the entity store to an existing database connection.
func New(db *sql.DB) (*Store, error) {
	return NewWithReadPool(db, db)
}

// NewWithReadPool attaches with a separate read pool (file-backed
// databases only; pass db itself for :memory:).
func NewWithReadPool(db, ro *sql.DB) (*Store, error) {
	s := &Store{db: db, ro: ro}
	if err := s.init(); err != nil {
		return nil, err
	}
	// Startup self-heal: a failed concurrent COMMIT can leave phantom
	// transaction versions inside a UNIQUE index (DuckDB does not fully
	// roll back index entries of a conflicted transaction). Every later
	// write to that key then fails with a persistent "write-write
	// conflict" that survives restarts. Probing with a no-op rewrite of
	// the first row reproduces the failure cheaply; rebuilding the index
	// drops the phantom versions and cures it.
	if err := s.selfHealUniqueIndex("users", "users_email_idx", "email"); err != nil {
		log.Printf("[duckstore] unique-index self-heal: %v", err)
	}
	// Ghost entries also land in NON-unique indexes, where they fail
	// silently: index scans skip rows (the photos timeline returned an
	// empty list while a sequential scan saw every asset). A write probe
	// cannot detect that, so the hot read-path indexes are rebuilt
	// unconditionally at boot — cheap on small libraries, seconds on
	// large ones, and the only reliable cure.
	s.rebuildGhostProneIndexes()
	return s, nil
}

// rebuildGhostProneIndexes recreates the secondary indexes the timeline
// and auth read paths scan, clearing any phantom entries left behind by
// conflicted transactions.
func (s *Store) rebuildGhostProneIndexes() {
	for _, idx := range [][3]string{
		{"assets", "assets_owner_idx", "owner_id"},
		{"assets", "assets_owner_checksum_idx", "owner_id, checksum"},
		{"sessions", "sessions_user_idx", "user_id"},
		{"api_keys", "api_keys_user_idx", "user_id"},
		{"albums", "albums_owner_idx", "owner_id"},
	} {
		table, name, cols := idx[0], idx[1], idx[2]
		if _, err := s.db.Exec("DROP INDEX IF EXISTS " + name); err != nil {
			log.Printf("[duckstore] index rebuild drop %s: %v", name, err)
			continue
		}
		if _, err := s.db.Exec("CREATE INDEX " + name + " ON " + table + " (" + cols + ")"); err != nil {
			log.Printf("[duckstore] index rebuild create %s: %v", name, err)
		}
	}
}

// SetReaderPoolSize records the configured reader count for conflict
// cleanup (restores the pool after a forced close).
func (s *Store) SetReaderPoolSize(n int) { s.roMax = n }

// Checkpoint flushes the write-ahead log into the database file and
// truncates it. Running it periodically (and on graceful shutdown) keeps
// the WAL tiny, so a hard kill loses at most one interval of writes and
// leaves little replay surface for corruption.
func (s *Store) Checkpoint() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := s.db.Exec(`CHECKPOINT`); err != nil {
		return fmt.Errorf("checkpoint: %w", err)
	}
	return nil
}

// IsWALReplayError reports whether err came from DuckDB failing to
// replay the write-ahead log at open time (garbage or a torn write from
// a hard kill).
func IsWALReplayError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "replaying WAL") ||
		strings.Contains(msg, "buffered appends") ||
		strings.Contains(msg, "WAL file")
}

// MoveWALAside renames the write-ahead log next to the database to a
// timestamped backup so the database can open without replaying it. The
// timestamp keeps every backup (os.Rename on Windows cannot overwrite an
// existing file). The caller loses only writes after the last
// checkpoint — the alternative is a server that never boots again.
func MoveWALAside(dbPath string) (backup string, err error) {
	wal := dbPath + ".wal"
	backup = fmt.Sprintf("%s.corrupt-%d", wal, time.Now().Unix())
	if err := os.Rename(wal, backup); err != nil {
		return "", fmt.Errorf("move WAL %s aside: %w", wal, err)
	}
	return backup, nil
}

// selfHealUniqueIndex probes an index for phantom conflict versions by
// rewriting the first row's indexed column to itself; on a write-write
// conflict it rebuilds the index, which clears the stale versions.
func (s *Store) selfHealUniqueIndex(table, index, column string) error {
	probe := "UPDATE " + table + " SET " + column + " = " + column +
		" WHERE " + column + " = (SELECT min(" + column + ") FROM " + table + ")"
	if _, err := s.db.Exec(probe); err == nil || !isWriteConflict(err) {
		return nil
	}
	log.Printf("[duckstore] stale versions detected in %s; rebuilding", index)
	if _, err := s.db.Exec("DROP INDEX IF EXISTS " + index); err != nil {
		return err
	}
	if _, err := s.db.Exec("CREATE UNIQUE INDEX " + index + " ON " + table + " (" + column + ")"); err != nil {
		return err
	}
	if _, err := s.db.Exec(probe); err != nil {
		return fmt.Errorf("rebuild did not cure the conflict: %w", err)
	}
	return nil
}

func (s *Store) Close() error {
	if s.owns {
		return s.db.Close()
	}
	return nil
}

func (s *Store) Users() store.UserStore        { return (*userStore)(s) }
func (s *Store) Sessions() store.SessionStore  { return (*sessionStore)(s) }
func (s *Store) APIKeys() store.APIKeyStore    { return (*apiKeyStore)(s) }
func (s *Store) Assets() store.AssetStore      { return (*assetStore)(s) }
func (s *Store) Albums() store.AlbumStore      { return (*albumStore)(s) }
func (s *Store) Memories() store.MemoryStore   { return (*memoryStore)(s) }
func (s *Store) SyncAcks() store.SyncAckStore  { return (*syncAckStore)(s) }
func (s *Store) Metadata() store.MetadataStore { return (*metadataStore)(s) }
func (s *Store) Stacks() store.StackStore      { return (*stackStore)(s) }
func (s *Store) Partners() store.PartnerStore  { return (*partnerStore)(s) }
func (s *Store) Tags() store.TagStore          { return (*tagStore)(s) }
func (s *Store) Sync() store.SyncStore         { return (*syncStore)(s) }

func (s *Store) init() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id VARCHAR PRIMARY KEY,
			email VARCHAR NOT NULL,
			password VARCHAR NOT NULL DEFAULT '',
			name VARCHAR NOT NULL,
			is_admin BOOLEAN NOT NULL DEFAULT FALSE,
			should_change_password BOOLEAN NOT NULL DEFAULT FALSE,
			avatar_color VARCHAR NOT NULL DEFAULT 'primary',
			profile_image_path VARCHAR NOT NULL DEFAULT '',
			storage_label VARCHAR NOT NULL DEFAULT '',
			is_onboarded BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			deleted_at TIMESTAMP
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS users_email_idx ON users (email)`,
		// Databases created before per-user preferences existed.
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS preferences VARCHAR`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id VARCHAR PRIMARY KEY,
			token_hash BLOB NOT NULL,
			user_id VARCHAR NOT NULL,
			device_os VARCHAR NOT NULL DEFAULT '',
			device_type VARCHAR NOT NULL DEFAULT '',
			app_version VARCHAR NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			expires_at TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS sessions_user_idx ON sessions (user_id)`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id VARCHAR PRIMARY KEY,
			name VARCHAR NOT NULL DEFAULT '',
			key_hash BLOB NOT NULL,
			user_id VARCHAR NOT NULL,
			permissions VARCHAR NOT NULL DEFAULT 'all',
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS api_keys_user_idx ON api_keys (user_id)`,
		`CREATE TABLE IF NOT EXISTS assets (
			id VARCHAR PRIMARY KEY,
			owner_id VARCHAR NOT NULL,
			type VARCHAR NOT NULL,
			original_path VARCHAR NOT NULL,
			thumbnail_path VARCHAR NOT NULL DEFAULT '',
			preview_path VARCHAR NOT NULL DEFAULT '',
			original_file_name VARCHAR NOT NULL DEFAULT '',
			original_mime_type VARCHAR NOT NULL DEFAULT '',
			file_created_at TIMESTAMP NOT NULL,
			file_modified_at TIMESTAMP NOT NULL,
			local_datetime TIMESTAMP NOT NULL,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			deleted_at TIMESTAMP,
			is_favorite BOOLEAN NOT NULL DEFAULT FALSE,
			duration BIGINT,
			checksum BLOB NOT NULL,
			checksum_b64 VARCHAR NOT NULL DEFAULT '',
			width INTEGER,
			height INTEGER,
			visibility VARCHAR NOT NULL DEFAULT 'timeline',
			library_id VARCHAR,
			live_photo_video_id VARCHAR,
			duplicate_id VARCHAR,
			thumbhash VARCHAR NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS assets_owner_idx ON assets (owner_id)`,
		`CREATE INDEX IF NOT EXISTS assets_owner_checksum_idx ON assets (owner_id, checksum)`,
		`CREATE TABLE IF NOT EXISTS asset_exifs (
			asset_id VARCHAR PRIMARY KEY,
			make VARCHAR NOT NULL DEFAULT '',
			model VARCHAR NOT NULL DEFAULT '',
			lens_model VARCHAR NOT NULL DEFAULT '',
			file_size BIGINT,
			exif_width INTEGER,
			exif_height INTEGER,
			date_time_original TIMESTAMP,
			latitude DOUBLE,
			longitude DOUBLE,
			city VARCHAR NOT NULL DEFAULT '',
			state VARCHAR NOT NULL DEFAULT '',
			country VARCHAR NOT NULL DEFAULT '',
			description VARCHAR NOT NULL DEFAULT '',
			rating INTEGER,
			fps DOUBLE
		)`,
		// Databases created before the fps column existed.
		`ALTER TABLE asset_exifs ADD COLUMN IF NOT EXISTS fps DOUBLE`,
		`CREATE TABLE IF NOT EXISTS albums (
			id VARCHAR PRIMARY KEY,
			owner_id VARCHAR NOT NULL,
			album_name VARCHAR NOT NULL,
			description VARCHAR NOT NULL DEFAULT '',
			album_thumbnail_asset_id VARCHAR,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			deleted_at TIMESTAMP,
			is_activity_enabled BOOLEAN NOT NULL DEFAULT TRUE,
			sort_order VARCHAR NOT NULL DEFAULT 'asc'
		)`,
		`CREATE INDEX IF NOT EXISTS albums_owner_idx ON albums (owner_id)`,
		`CREATE TABLE IF NOT EXISTS album_assets (
			album_id VARCHAR NOT NULL,
			asset_id VARCHAR NOT NULL,
			position INTEGER NOT NULL,
			PRIMARY KEY (album_id, asset_id)
		)`,
		`CREATE TABLE IF NOT EXISTS album_users (
			album_id VARCHAR NOT NULL,
			user_id VARCHAR NOT NULL,
			role VARCHAR NOT NULL DEFAULT 'editor',
			PRIMARY KEY (album_id, user_id)
		)`,
		`CREATE TABLE IF NOT EXISTS memories (
			id VARCHAR PRIMARY KEY,
			owner_id VARCHAR NOT NULL,
			type VARCHAR NOT NULL DEFAULT 'on_this_day',
			data VARCHAR NOT NULL DEFAULT '{}',
			memory_at TIMESTAMP NOT NULL,
			show_at TIMESTAMP,
			hide_at TIMESTAMP,
			seen_at TIMESTAMP,
			is_saved BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			deleted_at TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS memories_owner_idx ON memories (owner_id)`,
		`CREATE TABLE IF NOT EXISTS memory_assets (
			memory_id VARCHAR NOT NULL,
			asset_id VARCHAR NOT NULL,
			position INTEGER NOT NULL,
			PRIMARY KEY (memory_id, asset_id)
		)`,
		`CREATE TABLE IF NOT EXISTS sync_acks (
			user_id VARCHAR NOT NULL,
			type VARCHAR NOT NULL,
			ack VARCHAR NOT NULL,
			PRIMARY KEY (user_id, type, ack)
		)`,
		`CREATE TABLE IF NOT EXISTS system_metadata (
			key VARCHAR PRIMARY KEY,
			value VARCHAR NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS stacks (
			id VARCHAR PRIMARY KEY,
			owner_id VARCHAR NOT NULL,
			primary_asset_id VARCHAR,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS stack_assets (
			stack_id VARCHAR NOT NULL,
			asset_id VARCHAR NOT NULL,
			position INTEGER NOT NULL,
			PRIMARY KEY (stack_id, asset_id)
		)`,
		`CREATE TABLE IF NOT EXISTS partners (
			id VARCHAR PRIMARY KEY,
			owner_id VARCHAR NOT NULL,
			user_id VARCHAR NOT NULL,
			in_timeline BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS partners_pair_idx ON partners (owner_id, user_id)`,
		`CREATE TABLE IF NOT EXISTS tags (
			id VARCHAR PRIMARY KEY,
			user_id VARCHAR NOT NULL,
			name VARCHAR NOT NULL,
			value VARCHAR NOT NULL,
			parent_id VARCHAR,
			color VARCHAR,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			update_id BIGINT NOT NULL DEFAULT 0
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS tags_user_value_idx ON tags (user_id, value)`,
		`CREATE INDEX IF NOT EXISTS tags_parent_idx ON tags (parent_id)`,
		`CREATE TABLE IF NOT EXISTS tag_assets (
			tag_id VARCHAR NOT NULL,
			asset_id VARCHAR NOT NULL,
			attached_at TIMESTAMP,
			PRIMARY KEY (tag_id, asset_id)
		)`,
		`CREATE INDEX IF NOT EXISTS tag_assets_asset_idx ON tag_assets (asset_id)`,
		`CREATE TABLE IF NOT EXISTS sync_deletes (
			entity_type VARCHAR NOT NULL,
			entity_id VARCHAR NOT NULL,
			update_id BIGINT NOT NULL,
			PRIMARY KEY (entity_type, entity_id)
		)`,
		// Incremental sync watermarks.
		`CREATE SEQUENCE IF NOT EXISTS update_id_seq START 1`,
		`ALTER TABLE assets ADD COLUMN IF NOT EXISTS update_id BIGINT DEFAULT 0`,
		`ALTER TABLE albums ADD COLUMN IF NOT EXISTS update_id BIGINT DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS update_id BIGINT DEFAULT 0`,
		`ALTER TABLE memories ADD COLUMN IF NOT EXISTS update_id BIGINT DEFAULT 0`,
	}
	for _, stmt := range statements {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("duckstore init: %w", err)
		}
	}
	return nil
}

// joinPermissions packs the permission list into a VARCHAR column
// (permission names never contain commas).
func joinPermissions(perms []string) string {
	out := ""
	for i, p := range perms {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}

func splitPermissions(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return out
}

// tx runs fn inside a transaction on the shared single connection.
// All writes serialize on writeMu (DuckDB's embedded single-writer model)
// and a write-write conflict retries after clearing any transaction the
// failed COMMIT may have stranded on the connection.
func (s *Store) tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var lastErr error
	for range 3 {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := fn(tx); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			lastErr = err
			if !isWriteConflict(err) {
				return err
			}
			s.rollbackStranded(ctx)
			continue
		}
		return nil
	}
	return lastErr
}

// exec is the autocommit write path: serialized like tx and retried on a
// write-write conflict after clearing the connection.
func (s *Store) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var res sql.Result
	var lastErr error
	for range 3 {
		var err error
		res, err = s.db.ExecContext(ctx, query, args...)
		if err == nil {
			return res, nil
		}
		lastErr = err
		if !isWriteConflict(err) {
			return nil, err
		}
		s.rollbackStranded(ctx)
	}
	return nil, lastErr
}

// rollbackStranded clears a transaction the failed COMMIT may have left
// open on the writer connection, and recycles the reader pool (its
// connections are full DuckDB connections where a stranded write
// transaction would also survive).
func (s *Store) rollbackStranded(ctx context.Context) {
	if _, err := s.db.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`); err != nil {
		log.Printf("[duckstore] conflict cleanup rollback (writer): %v", err)
	}
	if s.ro != nil && s.ro != s.db {
		max := s.roMax
		if max <= 0 {
			max = 4
		}
		s.ro.SetMaxOpenConns(0)
		s.ro.SetMaxOpenConns(max)
	}
}

// isWriteConflict matches DuckDB's optimistic-concurrency abort message.
func isWriteConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "write-write conflict")
}

var _ store.Store = (*Store)(nil)
