package duckstore

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"immich-go/internal/crypto"
	"immich-go/internal/domain"
	"immich-go/internal/store"
)

type tagStore Store

const tagColumns = `id, user_id, name, value, parent_id, color, created_at, updated_at, update_id`

func scanTag(sc rowScanner2) (*domain.Tag, error) {
	var t domain.Tag
	var parent, color sql.NullString
	if err := sc.Scan(&t.ID, &t.UserID, &t.Name, &t.Value, &parent, &color, &t.CreatedAt, &t.UpdatedAt, &t.UpdateID); err != nil {
		if err == sql.ErrNoRows {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	if parent.Valid {
		p := parent.String
		t.ParentID = &p
	}
	if color.Valid {
		c := color.String
		t.Color = &c
	}
	return &t, nil
}

func (s *tagStore) Create(ctx context.Context, t *domain.Tag) error {
	uid, err := (*Store)(s).nextUpdateID(ctx)
	if err != nil {
		return err
	}
	t.UpdateID = uid
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	t.UpdatedAt = t.CreatedAt
	_, err = (*Store)(s).exec(ctx, `
		INSERT INTO tags (id, user_id, name, value, parent_id, color, created_at, updated_at, update_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.UserID, t.Name, t.Value, nullStrPtr(t.ParentID), nullStrPtr(t.Color), t.CreatedAt, t.UpdatedAt, t.UpdateID)
	return err
}

func (s *tagStore) Update(ctx context.Context, t *domain.Tag) error {
	uid, err := (*Store)(s).nextUpdateID(ctx)
	if err != nil {
		return err
	}
	t.UpdateID = uid
	t.UpdatedAt = time.Now().UTC()
	res, err := (*Store)(s).exec(ctx, `
		UPDATE tags SET name = ?, value = ?, parent_id = ?, color = ?, updated_at = ?, update_id = ?
		WHERE id = ?`,
		t.Name, t.Value, nullStrPtr(t.ParentID), nullStrPtr(t.Color), t.UpdatedAt, t.UpdateID, t.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *tagStore) Delete(ctx context.Context, id string) error {
	return (*Store)(s).tx(ctx, func(tx *sql.Tx) error {
		return s.deleteTx(ctx, tx, id)
	})
}

func (s *tagStore) deleteTx(ctx context.Context, tx *sql.Tx, id string) error {
	var userID string
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM tags WHERE id = ?`, id).Scan(&userID); err != nil {
		if err == sql.ErrNoRows {
			return store.ErrNotFound
		}
		return err
	}
	// Remember which assets were linked so their sync watermark can be
	// bumped — the tag disappears from their next asset row redelivery.
	rows, err := tx.QueryContext(ctx, `
		SELECT ta.asset_id FROM tag_assets ta
		JOIN tags t ON t.id = ta.tag_id
		WHERE t.id = ? OR t.parent_id = ?`, id, id)
	if err != nil {
		return err
	}
	var linked []string
	for rows.Next() {
		var assetID string
		if err := rows.Scan(&assetID); err != nil {
			rows.Close()
			return err
		}
		linked = append(linked, assetID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM tag_assets WHERE tag_id IN (SELECT id FROM tags WHERE id = ? OR parent_id = ?)`, id, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tags WHERE id = ? OR parent_id = ?`, id, id); err != nil {
		return err
	}
	if err := s.bumpAssets(ctx, tx, linked); err != nil {
		return err
	}
	// Upstream does not sync tag deletions (no SyncEntityType for tags);
	// asset rows carry their tags, and the delete bumps above already
	// cover attached assets, so no tombstone is recorded here.
	return nil
}

func (s *tagStore) Get(ctx context.Context, id string) (*domain.Tag, error) {
	row := s.ro.QueryRowContext(ctx, `SELECT `+tagColumns+` FROM tags WHERE id = ?`, id)
	return scanTag(row)
}

func (s *tagStore) GetByValue(ctx context.Context, userID, value string) (*domain.Tag, error) {
	row := s.ro.QueryRowContext(ctx, `SELECT `+tagColumns+` FROM tags WHERE user_id = ? AND value = ?`, userID, value)
	return scanTag(row)
}

func (s *tagStore) ListForUser(ctx context.Context, userID string) ([]*domain.Tag, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT `+tagColumns+` FROM tags WHERE user_id = ? ORDER BY value ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Tag
	for rows.Next() {
		t, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// UpsertValue creates the tag (and every missing path segment above it) or
// returns the existing one — the same walk as upstream's upsertTags.
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
			ParentID:  idPtr(parent),
			CreatedAt: time.Now().UTC(),
		}
		if err := s.Create(ctx, tag); err != nil {
			return nil, err
		}
		parent = tag
	}
	return parent, nil
}

func idPtr(t *domain.Tag) *string {
	if t == nil {
		return nil
	}
	return &t.ID
}

func nullStrPtr(p *string) any {
	if p == nil || *p == "" {
		return nil
	}
	return *p
}

func (s *tagStore) ListForAsset(ctx context.Context, assetID string) ([]*domain.Tag, error) {
	rows, err := s.ro.QueryContext(ctx, `
		SELECT `+tagColumns+` FROM tags t
		JOIN tag_assets ta ON ta.tag_id = t.id
		WHERE ta.asset_id = ?
		ORDER BY t.value ASC`, assetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Tag
	for rows.Next() {
		t, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *tagStore) ListForAssets(ctx context.Context, assetIDs []string) (map[string][]*domain.Tag, error) {
	out := map[string][]*domain.Tag{}
	if len(assetIDs) == 0 {
		return out, nil
	}
	// Chunk to keep the IN list a sane size; asset listings pass hundreds.
	const chunk = 200
	for start := 0; start < len(assetIDs); start += chunk {
		end := start + chunk
		if end > len(assetIDs) {
			end = len(assetIDs)
		}
		ids := assetIDs[start:end]
		placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
		args := make([]any, len(ids))
		for i, id := range ids {
			args[i] = id
		}
		rows, err := s.ro.QueryContext(ctx, `
			SELECT ta.asset_id, `+tagColumns+` FROM tags t
			JOIN tag_assets ta ON ta.tag_id = t.id
			WHERE ta.asset_id IN (`+placeholders+`)
			ORDER BY t.value ASC`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var assetID string
			var t domain.Tag
			var parent, color sql.NullString
			if err := rows.Scan(&assetID, &t.ID, &t.UserID, &t.Name, &t.Value, &parent, &color, &t.CreatedAt, &t.UpdatedAt, &t.UpdateID); err != nil {
				rows.Close()
				return nil, err
			}
			if parent.Valid {
				p := parent.String
				t.ParentID = &p
			}
			if color.Valid {
				c := color.String
				t.Color = &c
			}
			out[assetID] = append(out[assetID], &t)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// bumpAssets stamps a fresh update_id on the given assets so incremental
// sync redelivers them (their tag links changed).
func (s *tagStore) bumpAssets(ctx context.Context, tx *sql.Tx, assetIDs []string) error {
	for _, id := range assetIDs {
		if _, err := tx.ExecContext(ctx, `UPDATE assets SET update_id = nextval('update_id_seq') WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *tagStore) Attach(ctx context.Context, tagID string, assetIDs []string) (int, error) {
	added := 0
	err := (*Store)(s).tx(ctx, func(tx *sql.Tx) error {
		for _, assetID := range assetIDs {
			res, err := tx.ExecContext(ctx, `
				INSERT INTO tag_assets (tag_id, asset_id, attached_at) VALUES (?, ?, ?)
				ON CONFLICT (tag_id, asset_id) DO NOTHING`, tagID, assetID, time.Now().UTC())
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				added++
			}
		}
		return s.bumpAssets(ctx, tx, assetIDs)
	})
	if err != nil {
		return 0, err
	}
	return added, nil
}

func (s *tagStore) Detach(ctx context.Context, tagID string, assetIDs []string) (int, error) {
	removed := 0
	err := (*Store)(s).tx(ctx, func(tx *sql.Tx) error {
		for _, assetID := range assetIDs {
			res, err := tx.ExecContext(ctx, `DELETE FROM tag_assets WHERE tag_id = ? AND asset_id = ?`, tagID, assetID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				removed++
			}
		}
		return s.bumpAssets(ctx, tx, assetIDs)
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}
