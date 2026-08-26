package duckstore

import (
	"context"
	"database/sql"
	"strings"

	"immich-go/internal/domain"
	"immich-go/internal/store"
)

// ---- memories ----

type memoryStore Store

const memoryColumns = `id, owner_id, type, data, memory_at, show_at, hide_at, seen_at,
	is_saved, created_at, updated_at, deleted_at`

type rowScanner2 interface{ Scan(...any) error }

func scanMemory(scanner rowScanner2) (*domain.Memory, error) {
	var m domain.Memory
	var show, hide, seen, deleted sql.NullTime
	if err := scanner.Scan(&m.ID, &m.OwnerID, &m.Type, &m.Data, &m.MemoryAt,
		&show, &hide, &seen, &m.IsSaved, &m.CreatedAt, &m.UpdatedAt, &deleted); err != nil {
		return nil, err
	}
	if show.Valid {
		t := show.Time
		m.ShowAt = &t
	}
	if hide.Valid {
		t := hide.Time
		m.HideAt = &t
	}
	if seen.Valid {
		t := seen.Time
		m.SeenAt = &t
	}
	if deleted.Valid {
		t := deleted.Time
		m.DeletedAt = &t
	}
	return &m, nil
}

func (s *memoryStore) Create(ctx context.Context, m *domain.Memory) error {
	return (*Store)(s).tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO memories (`+memoryColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, m.OwnerID, m.Type, m.Data, m.MemoryAt, m.ShowAt, m.HideAt, m.SeenAt,
			m.IsSaved, m.CreatedAt, m.UpdatedAt, m.DeletedAt); err != nil {
			return err
		}
		return writeMemoryAssets(ctx, tx, m)
	})
}

func (s *memoryStore) Update(ctx context.Context, m *domain.Memory) error {
	return (*Store)(s).tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE memories SET
				type = ?, data = ?, memory_at = ?, show_at = ?, hide_at = ?, seen_at = ?,
				is_saved = ?, updated_at = ?, deleted_at = ?
			WHERE id = ?`,
			m.Type, m.Data, m.MemoryAt, m.ShowAt, m.HideAt, m.SeenAt,
			m.IsSaved, m.UpdatedAt, m.DeletedAt, m.ID)
		if err != nil {
			return err
		}
		if err := rowsAffected(res, 1); err != nil {
			return err
		}
		return writeMemoryAssets(ctx, tx, m)
	})
}

// writeMemoryAssets upserts the ordered asset list, pruning removed ids
// (same-transaction upsert-then-prune pattern as album membership).
func writeMemoryAssets(ctx context.Context, tx *sql.Tx, m *domain.Memory) error {
	for i, id := range m.AssetIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO memory_assets (memory_id, asset_id, position)
			VALUES (?, ?, ?)
			ON CONFLICT (memory_id, asset_id) DO UPDATE SET position = excluded.position`,
			m.ID, id, i); err != nil {
			return err
		}
	}
	if len(m.AssetIDs) == 0 {
		_, err := tx.ExecContext(ctx, `DELETE FROM memory_assets WHERE memory_id = ?`, m.ID)
		return err
	}
	marks := strings.TrimSuffix(strings.Repeat("?, ", len(m.AssetIDs)), ", ")
	args := make([]any, 0, len(m.AssetIDs)+1)
	args = append(args, m.ID)
	for _, id := range m.AssetIDs {
		args = append(args, id)
	}
	_, err := tx.ExecContext(ctx,
		`DELETE FROM memory_assets WHERE memory_id = ? AND asset_id NOT IN (`+marks+`)`, args...)
	return err
}

func (s *memoryStore) Delete(ctx context.Context, id string) error {
	return (*Store)(s).tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM memory_assets WHERE memory_id = ?`, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM memories WHERE id = ?`, id)
		if err != nil {
			return err
		}
		return rowsAffected(res, 1)
	})
}

func (s *memoryStore) Get(ctx context.Context, id string) (*domain.Memory, error) {
	row := s.ro.QueryRowContext(ctx, `SELECT `+memoryColumns+` FROM memories WHERE id = ?`, id)
	m, err := scanMemory(row)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.loadAssets(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *memoryStore) loadAssets(ctx context.Context, m *domain.Memory) error {
	rows, err := s.ro.QueryContext(ctx,
		`SELECT asset_id FROM memory_assets WHERE memory_id = ? ORDER BY position, asset_id`, m.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		m.AssetIDs = append(m.AssetIDs, id)
	}
	return rows.Err()
}

func (s *memoryStore) ListForOwner(ctx context.Context, ownerID string) ([]*domain.Memory, error) {
	rows, err := s.ro.QueryContext(ctx,
		`SELECT `+memoryColumns+` FROM memories WHERE owner_id = ? AND deleted_at IS NULL
		 ORDER BY memory_at DESC, id`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Batch asset hydration: one query for every memory in the list.
	byID := make(map[string]*domain.Memory, len(out))
	marks := make([]string, 0, len(out))
	args := make([]any, 0, len(out))
	for _, m := range out {
		byID[m.ID] = m
		marks = append(marks, "?")
		args = append(args, m.ID)
	}
	assetRows, err := s.ro.QueryContext(ctx, `
		SELECT memory_id, asset_id FROM memory_assets
		WHERE memory_id IN (`+strings.Join(marks, ",")+`) ORDER BY memory_id, position, asset_id`, args...)
	if err != nil {
		return nil, err
	}
	for assetRows.Next() {
		var memoryID, assetID string
		if err := assetRows.Scan(&memoryID, &assetID); err != nil {
			assetRows.Close()
			return nil, err
		}
		if m, ok := byID[memoryID]; ok {
			m.AssetIDs = append(m.AssetIDs, assetID)
		}
	}
	err = assetRows.Err()
	assetRows.Close()
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ---- sync acks ----

type syncAckStore Store

func (s *syncAckStore) List(ctx context.Context, userID string) ([]domain.SyncAck, error) {
	rows, err := s.ro.QueryContext(ctx,
		`SELECT type, ack FROM sync_acks WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SyncAck
	for rows.Next() {
		var a domain.SyncAck
		if err := rows.Scan(&a.Type, &a.Ack); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *syncAckStore) Put(ctx context.Context, userID string, acks []domain.SyncAck) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, a := range acks {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO sync_acks (user_id, type, ack) VALUES (?, ?, ?)
			ON CONFLICT (user_id, type, ack) DO NOTHING`, userID, a.Type, a.Ack); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *syncAckStore) DeleteTypes(ctx context.Context, userID string, types []string) error {
	if len(types) == 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM sync_acks WHERE user_id = ?`, userID)
		return err
	}
	marks := strings.TrimSuffix(strings.Repeat("?, ", len(types)), ", ")
	args := make([]any, 0, len(types)+1)
	args = append(args, userID)
	for _, t := range types {
		args = append(args, t)
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sync_acks WHERE user_id = ? AND type IN (`+marks+`)`, args...)
	return err
}

// ---- system metadata ----

type metadataStore Store

func (s *metadataStore) Get(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.ro.QueryRowContext(ctx,
		`SELECT value FROM system_metadata WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (s *metadataStore) Set(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO system_metadata (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// SetIfAbsent claims a key atomically: INSERT ... DO NOTHING keeps the
// second caller from winning.
func (s *metadataStore) SetIfAbsent(ctx context.Context, key, value string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO system_metadata (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO NOTHING`, key, value)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, nil
	}
	return n > 0, nil
}
