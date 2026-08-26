// Package vectordb is the vector store of immich-go, replacing the
// upstream pgvector/VectorChord layer with an embedded DuckDB database.
//
// It persists CLIP embeddings (smart_search), face embeddings
// (face_search) and clustered people (person), and offers SQL-level
// cosine similarity search, DBSCAN face clustering and near-duplicate
// detection — all inside the server binary with zero external services.
package vectordb

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"
)

// SmartHit is one ranked search result.
type SmartHit struct {
	AssetID string
	Score   float64
}

// Person is a cluster of face embeddings (Immich "person").
type Person struct {
	ID               string    `json:"id"`
	OwnerID          string    `json:"-"`
	Name             string    `json:"name"`
	BirthDate        string    `json:"birthDate,omitempty"` // YYYY-MM-DD or ""
	Color            string    `json:"color,omitempty"`
	FaceCount        int       `json:"faceCount"`
	ThumbnailAssetID string    `json:"thumbnailAssetId"`
	IsHidden         bool      `json:"isHidden"`
	IsFavorite       bool      `json:"isFavorite"`
	CreatedAt        time.Time `json:"-"`
	UpdatedAt        time.Time `json:"-"`
}

// FaceRow couples a detected face with its embedding.
type FaceRow struct {
	AssetID  string
	FaceIdx  int
	PersonID string // empty when unassigned
	Box      [4]int // x1, y1, x2, y2
	Vec      []float32
}

// Store wraps the DuckDB database. A single connection serializes access,
// which matches DuckDB's embedded single-writer nature and keeps our
// small critical sections free of transaction conflicts. When attached to
// a shared database (Attach) the store does not own — and never closes —
// the connection; entity metadata lives in the same file.
type Store struct {
	db  *sql.DB // single-writer pool (all mutations and transactions)
	ro  *sql.DB // read pool (may equal db; file-backed DBs get real parallelism)
	dim int
	owns bool

	mu          sync.Mutex // guards clustering/dedup recomputation
	hasCosineFn bool
}

// Open creates/opens a dedicated DuckDB file at path (":memory:" for
// testing) with vectors of the given dimension; Close closes it.
func Open(path string, dim int) (*Store, error) {
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s, err := AttachReadWrite(db, db, dim)
	if err != nil {
		db.Close()
		return nil, err
	}
	s.owns = true
	return s, nil
}

// Attach initializes the vector tables on a shared *sql.DB (single
// connection expected). The caller stays responsible for closing it.
func Attach(db *sql.DB, dim int) (*Store, error) {
	return AttachReadWrite(db, db, dim)
}

// AttachReadWrite is Attach with a dedicated read pool (file-backed
// databases only; pass db for :memory:).
func AttachReadWrite(db, ro *sql.DB, dim int) (*Store, error) {
	if dim <= 0 {
		dim = 512
	}
	s := &Store{db: db, ro: ro, dim: dim}
	if err := s.init(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s.owns {
		return s.db.Close()
	}
	return nil
}

func (s *Store) Dim() int { return s.dim }

// HasSQLCosine reports whether DuckDB-native array_cosine_similarity is
// available (false → Go-side ranking fallback).
func (s *Store) HasSQLCosine() bool { return s.hasCosineFn }

func (s *Store) init() error {
	statements := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS smart_search (
			asset_id VARCHAR PRIMARY KEY,
			owner_id VARCHAR NOT NULL,
			model VARCHAR NOT NULL,
			embedding FLOAT[%d] NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`, s.dim),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS face_search (
			asset_id VARCHAR NOT NULL,
			face_idx INTEGER NOT NULL,
			owner_id VARCHAR NOT NULL,
			person_id VARCHAR,
			x1 INTEGER NOT NULL,
			y1 INTEGER NOT NULL,
			x2 INTEGER NOT NULL,
			y2 INTEGER NOT NULL,
			embedding FLOAT[%d] NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			PRIMARY KEY (asset_id, face_idx)
		)`, s.dim),
		`CREATE TABLE IF NOT EXISTS person (
			id VARCHAR PRIMARY KEY,
			owner_id VARCHAR NOT NULL,
			name VARCHAR NOT NULL DEFAULT '',
			is_hidden BOOLEAN NOT NULL DEFAULT FALSE,
			is_favorite BOOLEAN NOT NULL DEFAULT FALSE,
			face_count INTEGER NOT NULL DEFAULT 0,
			thumbnail_asset_id VARCHAR,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`,
		// Columns added after the first release.
		`ALTER TABLE person ADD COLUMN IF NOT EXISTS birth_date VARCHAR`,
		`ALTER TABLE person ADD COLUMN IF NOT EXISTS color VARCHAR`,
		`CREATE INDEX IF NOT EXISTS smart_search_owner_idx ON smart_search (owner_id)`,
		`CREATE INDEX IF NOT EXISTS face_search_owner_idx ON face_search (owner_id)`,
	}
	for _, stmt := range statements {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("init vectordb: %w", err)
		}
	}
	s.hasCosineFn = s.probeCosine()
	return nil
}

// probeCosine checks whether array_cosine_similarity exists in this
// DuckDB build; otherwise queries fall back to Go-side ranking.
func (s *Store) probeCosine() bool {
	var sim float64
	err := s.db.QueryRow(`SELECT array_cosine_similarity(
		TRY_CAST('[1.0,0.0]' AS FLOAT[2]),
		TRY_CAST('[1.0,0.0]' AS FLOAT[2]))`).Scan(&sim)
	return err == nil && sim > 0.99
}

// vecLiteral renders a vector as the DuckDB array-literal string.
func vecLiteral(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// parseVecLiteral parses "[1.0,2.0,...]" back into []float32.
func parseVecLiteral(s string) ([]float32, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if s == "" {
		return nil, fmt.Errorf("empty vector literal")
	}
	parts := strings.Split(s, ",")
	out := make([]float32, len(parts))
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, err
		}
		out[i] = float32(f)
	}
	return out, nil
}

func (s *Store) checkDim(v []float32) error {
	if len(v) != s.dim {
		return fmt.Errorf("vector dimension mismatch: got %d, want %d", len(v), s.dim)
	}
	return nil
}

// --- smart search (CLIP image embeddings) ---

func (s *Store) UpsertSmartSearch(ctx context.Context, assetID, ownerID, model string, vec []float32) error {
	if err := s.checkDim(vec); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO smart_search (asset_id, owner_id, model, embedding, updated_at)
		VALUES (?, ?, ?, TRY_CAST(? AS FLOAT[`+fmt.Sprint(s.dim)+`]), now())
		ON CONFLICT (asset_id) DO UPDATE SET
			owner_id = excluded.owner_id,
			model = excluded.model,
			embedding = excluded.embedding,
			updated_at = excluded.updated_at`,
		assetID, ownerID, model, vecLiteral(vec))
	return err
}

func (s *Store) DeleteSmartSearch(ctx context.Context, assetID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM smart_search WHERE asset_id = ?`, assetID)
	return err
}

// SearchSmart returns the owner's assets ranked by cosine similarity to
// the query vector, computed inside DuckDB.
func (s *Store) SearchSmart(ctx context.Context, ownerID string, query []float32, limit int) ([]SmartHit, error) {
	if err := s.checkDim(query); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 250
	}
	if s.hasCosineFn {
		rows, err := s.ro.QueryContext(ctx, `
			SELECT asset_id, array_cosine_similarity(embedding, TRY_CAST(? AS FLOAT[`+fmt.Sprint(s.dim)+`])) AS score
			FROM smart_search
			WHERE owner_id = ?
			ORDER BY score DESC
			LIMIT ?`,
			vecLiteral(query), ownerID, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []SmartHit
		for rows.Next() {
			var h SmartHit
			if err := rows.Scan(&h.AssetID, &h.Score); err != nil {
				return nil, err
			}
			out = append(out, h)
		}
		return out, rows.Err()
	}

	// Fallback: rank in Go when the SQL function is unavailable.
	entries, err := s.loadSmart(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	type pair struct {
		id string
		vec []float32
	}
	items := make([]pair, 0, len(entries))
	for id, v := range entries {
		items = append(items, pair{id, v})
	}
	hits := RankByCosine(query, items, func(p pair) (string, []float32) { return p.id, p.vec })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func (s *Store) loadSmart(ctx context.Context, ownerID string) (map[string][]float32, error) {
	rows, err := s.ro.QueryContext(ctx,
		`SELECT asset_id, CAST(embedding AS VARCHAR) FROM smart_search WHERE owner_id = ?`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]float32{}
	for rows.Next() {
		var id, lit string
		if err := rows.Scan(&id, &lit); err != nil {
			return nil, err
		}
		v, err := parseVecLiteral(lit)
		if err != nil {
			continue
		}
		out[id] = v
	}
	return out, rows.Err()
}

// --- faces ---

// UpsertFaces replaces the stored faces of one asset (or a set of assets
// when the rows carry their own AssetID). DuckDB rejects deleting and
// re-inserting the same primary key inside one transaction, so rows are
// upserted and only surplus indexes are deleted.
func (s *Store) UpsertFaces(ctx context.Context, ownerID, assetID string, faces []FaceRow) error {
	for _, f := range faces {
		if err := s.checkDim(f.Vec); err != nil {
			return fmt.Errorf("face %s#%d: %w", assetID, f.FaceIdx, err)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Rows without an explicit AssetID fall back to the call's assetID.
	maxIdx := map[string]int{}
	assets := map[string]bool{}
	if len(faces) == 0 {
		assets[assetID] = true
	}
	for _, f := range faces {
		id := f.AssetID
		if id == "" {
			id = assetID
		}
		assets[id] = true
		if f.FaceIdx > maxIdx[id] {
			maxIdx[id] = f.FaceIdx
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO face_search (asset_id, face_idx, owner_id, person_id, x1, y1, x2, y2, embedding, updated_at)
			VALUES (?, ?, ?, NULL, ?, ?, ?, ?, TRY_CAST(? AS FLOAT[`+fmt.Sprint(s.dim)+`]), now())
			ON CONFLICT (asset_id, face_idx) DO UPDATE SET
				owner_id = excluded.owner_id,
				person_id = NULL,
				x1 = excluded.x1,
				y1 = excluded.y1,
				x2 = excluded.x2,
				y2 = excluded.y2,
				embedding = excluded.embedding,
				updated_at = excluded.updated_at`,
			id, f.FaceIdx, ownerID, f.Box[0], f.Box[1], f.Box[2], f.Box[3], vecLiteral(f.Vec)); err != nil {
			return err
		}
	}
	for id := range assets {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM face_search WHERE asset_id = ? AND face_idx > ?`, id, maxIdx[id]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteFaces(ctx context.Context, assetID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM face_search WHERE asset_id = ?`, assetID)
	return err
}

func (s *Store) LoadFaces(ctx context.Context, ownerID string) ([]FaceRow, error) {
	rows, err := s.ro.QueryContext(ctx, `
		SELECT asset_id, face_idx, COALESCE(person_id, ''), x1, y1, x2, y2, CAST(embedding AS VARCHAR)
		FROM face_search WHERE owner_id = ? ORDER BY asset_id, face_idx`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FaceRow
	for rows.Next() {
		var f FaceRow
		var lit string
		if err := rows.Scan(&f.AssetID, &f.FaceIdx, &f.PersonID,
			&f.Box[0], &f.Box[1], &f.Box[2], &f.Box[3], &lit); err != nil {
			return nil, err
		}
		v, err := parseVecLiteral(lit)
		if err != nil {
			continue
		}
		f.Vec = v
		out = append(out, f)
	}
	return out, rows.Err()
}

// ListPersons returns the owner's people ordered by face count.
func (s *Store) ListPersons(ctx context.Context, ownerID string) ([]Person, error) {
	rows, err := s.ro.QueryContext(ctx, `
		SELECT id, name, is_hidden, is_favorite, face_count, COALESCE(thumbnail_asset_id, ''),
			created_at, updated_at, COALESCE(birth_date, ''), COALESCE(color, '')
		FROM person WHERE owner_id = ?
		ORDER BY face_count DESC, created_at ASC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Person
	for rows.Next() {
		var p Person
		p.OwnerID = ownerID
		if err := rows.Scan(&p.ID, &p.Name, &p.IsHidden, &p.IsFavorite,
			&p.FaceCount, &p.ThumbnailAssetID, &p.CreatedAt, &p.UpdatedAt,
			&p.BirthDate, &p.Color); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Counts reports table sizes for observability.
func (s *Store) Counts(ctx context.Context) (smart, faces, persons int64, err error) {
	err = s.ro.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM smart_search),
			(SELECT COUNT(*) FROM face_search),
			(SELECT COUNT(*) FROM person)`).Scan(&smart, &faces, &persons)
	return
}
