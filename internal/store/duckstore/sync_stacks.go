package duckstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"immich-go/internal/crypto"
	"immich-go/internal/domain"
	"immich-go/internal/store"
)

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nextUpdateID draws from the global change sequence (writer conn).
func (s *Store) nextUpdateID(ctx context.Context) (int64, error) {
	var id int64
	if err := s.db.QueryRowContext(ctx, `SELECT nextval('update_id_seq')`).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) recordDelete(ctx context.Context, tx *sql.Tx, entityType, entityID string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO sync_deletes (entity_type, entity_id, update_id)
		VALUES (?, ?, nextval('update_id_seq'))`, entityType, entityID)
	return err
}

// ---- stacks ----

type stackStore Store

const stackColumns = `id, owner_id, primary_asset_id, created_at, updated_at`

func scanStack(scanner rowScanner2) (*domain.Stack, error) {
	var st domain.Stack
	var primary sql.NullString
	if err := scanner.Scan(&st.ID, &st.OwnerID, &primary, &st.CreatedAt, &st.UpdatedAt); err != nil {
		return nil, err
	}
	if primary.Valid {
		st.PrimaryAssetID = primary.String
	}
	return &st, nil
}

func (s *stackStore) loadAssets(ctx context.Context, st *domain.Stack) error {
	rows, err := s.ro.QueryContext(ctx, `
		SELECT asset_id FROM stack_assets WHERE stack_id = ? ORDER BY position, asset_id`, st.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		st.AssetIDs = append(st.AssetIDs, id)
	}
	return rows.Err()
}

func writeStackAssets(ctx context.Context, tx *sql.Tx, st *domain.Stack) error {
	for i, id := range st.AssetIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO stack_assets (stack_id, asset_id, position) VALUES (?, ?, ?)
			ON CONFLICT (stack_id, asset_id) DO UPDATE SET position = excluded.position`,
			st.ID, id, i); err != nil {
			return err
		}
	}
	if len(st.AssetIDs) == 0 {
		_, err := tx.ExecContext(ctx, `DELETE FROM stack_assets WHERE stack_id = ?`, st.ID)
		return err
	}
	marks := strings.TrimSuffix(strings.Repeat("?, ", len(st.AssetIDs)), ", ")
	args := []any{st.ID}
	for _, id := range st.AssetIDs {
		args = append(args, id)
	}
	_, err := tx.ExecContext(ctx,
		`DELETE FROM stack_assets WHERE stack_id = ? AND asset_id NOT IN (`+marks+`)`, args...)
	return err
}

func (s *stackStore) Create(ctx context.Context, st *domain.Stack) error {
	return (*Store)(s).tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO stacks (`+stackColumns+`) VALUES (?, ?, ?, ?, ?)`,
			st.ID, st.OwnerID, nullableString(st.PrimaryAssetID), st.CreatedAt, st.UpdatedAt); err != nil {
			return err
		}
		return writeStackAssets(ctx, tx, st)
	})
}

func (s *stackStore) Update(ctx context.Context, st *domain.Stack) error {
	return (*Store)(s).tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE stacks SET primary_asset_id = ?, updated_at = ? WHERE id = ?`,
			nullableString(st.PrimaryAssetID), st.UpdatedAt, st.ID)
		if err != nil {
			return err
		}
		if err := rowsAffected(res, 1); err != nil {
			return err
		}
		return writeStackAssets(ctx, tx, st)
	})
}

func (s *stackStore) Delete(ctx context.Context, id string) error {
	return (*Store)(s).tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM stack_assets WHERE stack_id = ?`, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM stacks WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if err := rowsAffected(res, 1); err != nil {
			return store.ErrNotFound
		}
		return (*Store)(s).recordDelete(ctx, tx, "StackDeleteV1", id)
	})
}

func (s *stackStore) Get(ctx context.Context, id string) (*domain.Stack, error) {
	row := s.ro.QueryRowContext(ctx, `SELECT `+stackColumns+` FROM stacks WHERE id = ?`, id)
	st, err := scanStack(row)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.loadAssets(ctx, st); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *stackStore) ListForOwner(ctx context.Context, ownerID string) ([]*domain.Stack, error) {
	rows, err := s.ro.QueryContext(ctx,
		`SELECT `+stackColumns+` FROM stacks WHERE owner_id = ? ORDER BY created_at, id`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Stack
	for rows.Next() {
		st, err := scanStack(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	byID := make(map[string]*domain.Stack, len(out))
	marks := make([]string, 0, len(out))
	args := make([]any, 0, len(out))
	for _, st := range out {
		byID[st.ID] = st
		marks = append(marks, "?")
		args = append(args, st.ID)
	}
	if len(args) == 0 {
		return out, nil
	}
	assetRows, err := s.ro.QueryContext(ctx, `
		SELECT stack_id, asset_id FROM stack_assets
		WHERE stack_id IN (`+strings.Join(marks, ",")+`) ORDER BY stack_id, position, asset_id`, args...)
	if err != nil {
		return nil, err
	}
	for assetRows.Next() {
		var stackID, assetID string
		if err := assetRows.Scan(&stackID, &assetID); err != nil {
			assetRows.Close()
			return nil, err
		}
		if st, ok := byID[stackID]; ok {
			st.AssetIDs = append(st.AssetIDs, assetID)
		}
	}
	err = assetRows.Err()
	assetRows.Close()
	return out, err
}

// ---- partners ----

type partnerStore Store

const partnerColumns = `id, owner_id, user_id, in_timeline, created_at, updated_at`

func scanPartner(scanner rowScanner2) (*domain.Partner, error) {
	var p domain.Partner
	if err := scanner.Scan(&p.ID, &p.OwnerID, &p.UserID, &p.InTimeline, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *partnerStore) Create(ctx context.Context, p *domain.Partner) error {
	now := time.Now().UTC()
	p.ID = crypto.NewUUID()
	p.CreatedAt, p.UpdatedAt = now, now
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO partners (`+partnerColumns+`) VALUES (?, ?, ?, ?, ?, ?)`,
		p.ID, p.OwnerID, p.UserID, p.InTimeline, p.CreatedAt, p.UpdatedAt)
	return err
}

func (s *partnerStore) Update(ctx context.Context, p *domain.Partner) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE partners SET in_timeline = ?, updated_at = ? WHERE id = ?`,
		p.InTimeline, p.UpdatedAt, p.ID)
	if err != nil {
		return err
	}
	return rowsAffected(res, 1)
}

func (s *partnerStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM partners WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return rowsAffected(res, 1)
}

func (s *partnerStore) Get(ctx context.Context, id string) (*domain.Partner, error) {
	row := s.ro.QueryRowContext(ctx, `SELECT `+partnerColumns+` FROM partners WHERE id = ?`, id)
	p, err := scanPartner(row)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return p, err
}

func (s *partnerStore) listWhere(ctx context.Context, col, userID string) ([]*domain.Partner, error) {
	rows, err := s.ro.QueryContext(ctx,
		`SELECT `+partnerColumns+` FROM partners WHERE `+col+` = ? ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Partner
	for rows.Next() {
		p, err := scanPartner(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *partnerStore) ListSharedBy(ctx context.Context, userID string) ([]*domain.Partner, error) {
	return s.listWhere(ctx, "owner_id", userID)
}

func (s *partnerStore) ListSharedWith(ctx context.Context, userID string) ([]*domain.Partner, error) {
	return s.listWhere(ctx, "user_id", userID)
}

// ---- incremental sync ----

type syncStore Store

func (s *syncStore) AssetsSince(ctx context.Context, ownerID string, since int64, limit int) ([]*domain.Asset, error) {
	rows, err := s.ro.QueryContext(ctx, `
		SELECT `+assetColumns+` FROM assets
		WHERE owner_id = ? AND update_id > ? AND deleted_at IS NULL
		ORDER BY update_id LIMIT ?`, ownerID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Asset
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := (*assetStore)(s).loadExifs(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *syncStore) AlbumsSince(ctx context.Context, ownerID string, since int64, limit int) ([]*domain.Album, error) {
	rows, err := s.ro.QueryContext(ctx, `
		SELECT `+albumColumns+` FROM albums
		WHERE owner_id = ? AND update_id > ?
		ORDER BY update_id LIMIT ?`, ownerID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Album
	for rows.Next() {
		al, err := scanAlbum(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, al)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := (*albumStore)(s).loadMembersBatch(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *syncStore) UsersSince(ctx context.Context, since int64, limit int) ([]*domain.User, error) {
	rows, err := s.ro.QueryContext(ctx, `
		SELECT `+userColumns+` FROM users WHERE update_id > ?
		ORDER BY update_id LIMIT ?`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *syncStore) DeletesSince(ctx context.Context, types []string, since int64, limit int) ([]domain.SyncDelete, error) {
	if len(types) == 0 {
		return nil, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?, ", len(types)), ", ")
	args := make([]any, 0, len(types)+1)
	args = append(args, since)
	for _, t := range types {
		args = append(args, t)
	}
	rows, err := s.ro.QueryContext(ctx, `
		SELECT entity_type, entity_id, update_id FROM sync_deletes
		WHERE update_id > ? AND entity_type IN (`+marks+`)
		ORDER BY update_id LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SyncDelete
	for rows.Next() {
		var d domain.SyncDelete
		if err := rows.Scan(&d.Type, &d.EntityID, &d.UpdateID); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *syncStore) LatestUpdateID(ctx context.Context) (int64, error) {
	var id sql.NullInt64
	err := s.ro.QueryRowContext(ctx, `
		SELECT GREATEST(
			(SELECT COALESCE(MAX(update_id), 0) FROM assets),
			(SELECT COALESCE(MAX(update_id), 0) FROM albums),
			(SELECT COALESCE(MAX(update_id), 0) FROM users),
			(SELECT COALESCE(MAX(update_id), 0) FROM sync_deletes))`).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("latest update id: %w", err)
	}
	return id.Int64, nil
}
