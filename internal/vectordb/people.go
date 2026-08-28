package vectordb

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrPersonNotFound = errors.New("person not found")

// GetPerson loads one person (any owner; authorization is the caller's job).
func (s *Store) GetPerson(ctx context.Context, id string) (*Person, error) {
	row := s.ro.QueryRowContext(ctx, `
		SELECT id, owner_id, name, is_hidden, is_favorite, face_count,
			COALESCE(thumbnail_asset_id, ''), created_at, updated_at,
			COALESCE(birth_date, ''), COALESCE(color, '')
		FROM person WHERE id = ?`, id)
	var p Person
	err := row.Scan(&p.ID, &p.OwnerID, &p.Name, &p.IsHidden, &p.IsFavorite,
		&p.FaceCount, &p.ThumbnailAssetID, &p.CreatedAt, &p.UpdatedAt,
		&p.BirthDate, &p.Color)
	if err == sql.ErrNoRows {
		return nil, ErrPersonNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdatePerson writes user-editable fields (name, visibility, favorite,
// birth date, color, thumbnail choice); face_count stays computed.
func (s *Store) UpdatePerson(ctx context.Context, p *Person) error {
	p.UpdatedAt = time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		UPDATE person SET
			name = ?, is_hidden = ?, is_favorite = ?, birth_date = ?, color = ?,
			thumbnail_asset_id = ?, updated_at = ?
		WHERE id = ?`,
		p.Name, p.IsHidden, p.IsFavorite, p.BirthDate, p.Color,
		nullableString(p.ThumbnailAssetID), p.UpdatedAt, p.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrPersonNotFound
	}
	return nil
}

// CreatePerson inserts a manually created person (no faces yet).
func (s *Store) CreatePerson(ctx context.Context, p *Person) error {
	now := time.Now().UTC()
	p.CreatedAt, p.UpdatedAt = now, now
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO person (id, owner_id, name, is_hidden, is_favorite,
			face_count, thumbnail_asset_id, created_at, updated_at, birth_date, color)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?)`,
		p.ID, p.OwnerID, p.Name, p.IsHidden, p.IsFavorite,
		nullableString(p.ThumbnailAssetID), now, now, p.BirthDate, p.Color)
	return err
}

// DeletePersons removes people and unassigns their faces.
func (s *Store) DeletePersons(ctx context.Context, ids ...string) error {
	for _, id := range ids {
		if err := s.deletePerson(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) deletePerson(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE face_search SET person_id = NULL WHERE person_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM person WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrPersonNotFound
	}
	return tx.Commit()
}

// MergePersons folds the given people into target: their faces are
// reassigned and the source rows deleted. Returns an error naming the
// first missing source.
func (s *Store) MergePersons(ctx context.Context, targetID string, sourceIDs []string) error {
	for _, src := range sourceIDs {
		if src == targetID {
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE face_search SET person_id = ? WHERE person_id = ?`, targetID, src); err != nil {
			tx.Rollback()
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM person WHERE id = ?`, src)
		if err != nil {
			tx.Rollback()
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			tx.Rollback()
			return ErrPersonNotFound
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE person SET face_count = (SELECT COUNT(*) FROM face_search f WHERE f.person_id = person.id),
			updated_at = now() WHERE id = ?`, targetID)
	return err
}

// ReassignEntry moves every face of an asset currently assigned to
// fromPerson to toPerson ("" unassigns).
type ReassignEntry struct {
	AssetID  string
	PersonID string // destination, "" = unassign
}

// ReassignFaces applies per-asset face moves; only faces belonging to
// fromPerson are touched, mirroring the upstream semantics of moving a
// specific person's face in a photo.
func (s *Store) ReassignFaces(ctx context.Context, fromPerson string, entries []ReassignEntry) error {
	for _, e := range entries {
		var res sql.Result
		var err error
		if e.PersonID == "" {
			res, err = s.db.ExecContext(ctx, `
				UPDATE face_search SET person_id = NULL
				WHERE asset_id = ? AND person_id = ?`, e.AssetID, fromPerson)
		} else {
			res, err = s.db.ExecContext(ctx, `
				UPDATE face_search SET person_id = ?
				WHERE asset_id = ? AND person_id = ?`, e.PersonID, e.AssetID, fromPerson)
		}
		if err != nil {
			return err
		}
		_ = res
	}
	// Refresh affected counts.
	_, err := s.db.ExecContext(ctx, `
		UPDATE person SET face_count =
			(SELECT COUNT(*) FROM face_search f WHERE f.person_id = person.id)`)
	return err
}

// PersonStats reports how many distinct assets a person appears in.
func (s *Store) PersonStats(ctx context.Context, personID string) (assets int, err error) {
	err = s.ro.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT asset_id) FROM face_search WHERE person_id = ?`,
		personID).Scan(&assets)
	return
}

// PersonFace returns a face of the person suitable for the thumbnail:
// prefer the configured thumbnail asset, else the first assigned face.
type FaceRef struct {
	AssetID string
	FaceIdx int
	Box     [4]int
}

func (s *Store) PersonFace(ctx context.Context, personID string) (*FaceRef, error) {
	p, err := s.GetPerson(ctx, personID)
	if err != nil {
		return nil, err
	}
	row := s.ro.QueryRowContext(ctx, `
		SELECT asset_id, face_idx, x1, y1, x2, y2 FROM face_search
		WHERE person_id = ?
		ORDER BY CASE WHEN asset_id = ? THEN 0 ELSE 1 END, asset_id, face_idx
		LIMIT 1`, personID, p.ThumbnailAssetID)
	var f FaceRef
	err = row.Scan(&f.AssetID, &f.FaceIdx, &f.Box[0], &f.Box[1], &f.Box[2], &f.Box[3])
	if err == sql.ErrNoRows {
		return nil, ErrPersonNotFound
	}
	return &f, err
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
