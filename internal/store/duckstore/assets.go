package duckstore

import (
	"context"
	"database/sql"
	"strings"

	"immich-go/internal/domain"
	"immich-go/internal/store"
)

// ---- assets ----

type assetStore Store

const assetColumns = `id, owner_id, type, original_path, thumbnail_path, preview_path,
	original_file_name, original_mime_type,
	file_created_at, file_modified_at, local_datetime, created_at, updated_at, deleted_at,
	is_favorite, duration, checksum, checksum_b64, width, height, visibility,
	library_id, live_photo_video_id, duplicate_id, thumbhash`

func scanAsset(scanner rowScanner) (*domain.Asset, error) {
	var a domain.Asset
	var deleted sql.NullTime
	var duration, width, height sql.NullInt64
	var libraryID, livePhotoID, duplicateID sql.NullString
	if err := scanner.Scan(&a.ID, &a.OwnerID, &a.Type, &a.OriginalPath, &a.ThumbnailPath,
		&a.PreviewPath, &a.OriginalFileName, &a.OriginalMimeType,
		&a.FileCreatedAt, &a.FileModifiedAt, &a.LocalDateTime, &a.CreatedAt, &a.UpdatedAt,
		&deleted, &a.IsFavorite, &duration, &a.Checksum, &a.ChecksumB64, &width, &height,
		&a.Visibility, &libraryID, &livePhotoID, &duplicateID, &a.Thumbhash); err != nil {
		return nil, err
	}
	if deleted.Valid {
		t := deleted.Time
		a.DeletedAt = &t
	}
	if duration.Valid {
		v := duration.Int64
		a.Duration = &v
	}
	if width.Valid {
		v := int(width.Int64)
		a.Width = &v
	}
	if height.Valid {
		v := int(height.Int64)
		a.Height = &v
	}
	if libraryID.Valid {
		v := libraryID.String
		a.LibraryID = &v
	}
	if livePhotoID.Valid {
		v := livePhotoID.String
		a.LivePhotoVideoID = &v
	}
	if duplicateID.Valid {
		v := duplicateID.String
		a.DuplicateID = &v
	}
	return &a, nil
}

func assetValues(a *domain.Asset) []any {
	return []any{
		a.ID, a.OwnerID, a.Type, a.OriginalPath, a.ThumbnailPath, a.PreviewPath,
		a.OriginalFileName, a.OriginalMimeType,
		a.FileCreatedAt, a.FileModifiedAt, a.LocalDateTime, a.CreatedAt, a.UpdatedAt,
		a.DeletedAt, a.IsFavorite, a.Duration, a.Checksum, a.ChecksumB64, a.Width, a.Height,
		a.Visibility, a.LibraryID, a.LivePhotoVideoID, a.DuplicateID, a.Thumbhash,
	}
}

func (s *assetStore) Create(ctx context.Context, a *domain.Asset) error {
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", 25), ", ")
	return (*Store)(s).tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO assets (`+assetColumns+`) VALUES (`+placeholders+`)`, assetValues(a)...); err != nil {
			if isUniqueViolation(err) {
				return store.ErrConflict
			}
			return err
		}
		return upsertExif(ctx, tx, a)
	})
}

func (s *assetStore) Update(ctx context.Context, a *domain.Asset) error {
	return (*Store)(s).tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE assets SET
				owner_id = ?, type = ?, original_path = ?, thumbnail_path = ?, preview_path = ?,
				original_file_name = ?, original_mime_type = ?,
				file_created_at = ?, file_modified_at = ?, local_datetime = ?,
				updated_at = ?, deleted_at = ?, is_favorite = ?, duration = ?,
				checksum = ?, checksum_b64 = ?, width = ?, height = ?, visibility = ?,
				library_id = ?, live_photo_video_id = ?, duplicate_id = ?, thumbhash = ?
			WHERE id = ?`,
			a.OwnerID, a.Type, a.OriginalPath, a.ThumbnailPath, a.PreviewPath,
			a.OriginalFileName, a.OriginalMimeType,
			a.FileCreatedAt, a.FileModifiedAt, a.LocalDateTime,
			a.UpdatedAt, a.DeletedAt, a.IsFavorite, a.Duration,
			a.Checksum, a.ChecksumB64, a.Width, a.Height, a.Visibility,
			a.LibraryID, a.LivePhotoVideoID, a.DuplicateID, a.Thumbhash, a.ID)
		if err != nil {
			return err
		}
		if err := rowsAffected(res, 1); err != nil {
			return err
		}
		return upsertExif(ctx, tx, a)
	})
}

// upsertExif writes the embedded EXIF record when present.
func upsertExif(ctx context.Context, tx *sql.Tx, a *domain.Asset) error {
	if a.Exif == nil {
		return nil
	}
	e := a.Exif
	_, err := tx.ExecContext(ctx, `
		INSERT INTO asset_exifs (asset_id, make, model, lens_model, file_size,
			exif_width, exif_height, date_time_original, latitude, longitude,
			city, state, country, description, rating, fps)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (asset_id) DO UPDATE SET
			make = excluded.make, model = excluded.model, lens_model = excluded.lens_model,
			file_size = excluded.file_size, exif_width = excluded.exif_width,
			exif_height = excluded.exif_height, date_time_original = excluded.date_time_original,
			latitude = excluded.latitude, longitude = excluded.longitude,
			city = excluded.city, state = excluded.state, country = excluded.country,
			description = excluded.description, rating = excluded.rating, fps = excluded.fps`,
		a.ID, e.Make, e.Model, e.LensModel, e.FileSize,
		e.ExifWidth, e.ExifHeight, e.DateTimeOriginal, e.Latitude, e.Longitude,
		e.City, e.State, e.Country, e.Description, e.Rating, e.FPS)
	return err
}

func (s *assetStore) Delete(ctx context.Context, id string) error {
	return (*Store)(s).tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM album_assets WHERE asset_id = ?`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM memory_assets WHERE asset_id = ?`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM asset_exifs WHERE asset_id = ?`, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM assets WHERE id = ?`, id)
		if err != nil {
			return err
		}
		return rowsAffected(res, 1)
	})
}

func (s *assetStore) loadExif(ctx context.Context, a *domain.Asset) {
	row := s.db.QueryRowContext(ctx, `
		SELECT make, model, lens_model, file_size, exif_width, exif_height,
			date_time_original, latitude, longitude, city, state, country, description, rating, fps
		FROM asset_exifs WHERE asset_id = ?`, a.ID)
	var e domain.AssetExif
	var fileSize, exifW, exifH, rating sql.NullInt64
	var dateTime sql.NullTime
	var lat, lon sql.NullFloat64
	var fps sql.NullFloat64
	err := row.Scan(&e.Make, &e.Model, &e.LensModel, &fileSize, &exifW, &exifH,
		&dateTime, &lat, &lon, &e.City, &e.State, &e.Country, &e.Description, &rating, &fps)
	if err != nil {
		return // no exif row
	}
	if fileSize.Valid {
		v := fileSize.Int64
		e.FileSize = v
	}
	if exifW.Valid {
		v := int(exifW.Int64)
		e.ExifWidth = &v
	}
	if exifH.Valid {
		v := int(exifH.Int64)
		e.ExifHeight = &v
	}
	if dateTime.Valid {
		t := dateTime.Time
		e.DateTimeOriginal = &t
	}
	if lat.Valid {
		v := lat.Float64
		e.Latitude = &v
	}
	if lon.Valid {
		v := lon.Float64
		e.Longitude = &v
	}
	if rating.Valid {
		v := int(rating.Int64)
		e.Rating = &v
	}
	if fps.Valid {
		v := fps.Float64
		e.FPS = &v
	}
	a.Exif = &e
}

func (s *assetStore) Get(ctx context.Context, id string) (*domain.Asset, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+assetColumns+` FROM assets WHERE id = ?`, id)
	a, err := scanAsset(row)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.loadExif(ctx, a)
	return a, nil
}

func (s *assetStore) GetByChecksum(ctx context.Context, ownerID string, checksum []byte) (*domain.Asset, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+assetColumns+` FROM assets
		WHERE owner_id = ? AND checksum = ? AND deleted_at IS NULL
		LIMIT 1`, ownerID, checksum)
	a, err := scanAsset(row)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.loadExif(ctx, a)
	return a, nil
}

func (s *assetStore) GetByChecksumAny(ctx context.Context, ownerID string, checksum []byte) (*domain.Asset, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+assetColumns+` FROM assets
		WHERE owner_id = ? AND checksum = ?
		LIMIT 1`, ownerID, checksum)
	a, err := scanAsset(row)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.loadExif(ctx, a)
	return a, nil
}

func (s *assetStore) listWhere(ctx context.Context, where string, args ...any) ([]*domain.Asset, error) {
	query := `SELECT ` + assetColumns + ` FROM assets` + where + ` ORDER BY created_at, id`
	rows, err := s.db.QueryContext(ctx, query, args...)
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
	for _, a := range out {
		s.loadExif(ctx, a)
	}
	return out, nil
}

func (s *assetStore) List(ctx context.Context) ([]*domain.Asset, error) {
	return s.listWhere(ctx, ``)
}

func (s *assetStore) ListForOwner(ctx context.Context, ownerID string) ([]*domain.Asset, error) {
	return s.listWhere(ctx, ` WHERE owner_id = ?`, ownerID)
}

// ---- albums ----

type albumStore Store

const albumColumns = `id, owner_id, album_name, description, album_thumbnail_asset_id,
	created_at, updated_at, deleted_at, is_activity_enabled, sort_order`

func scanAlbum(scanner rowScanner) (*domain.Album, error) {
	var al domain.Album
	var deleted sql.NullTime
	var thumb sql.NullString
	if err := scanner.Scan(&al.ID, &al.OwnerID, &al.AlbumName, &al.Description, &thumb,
		&al.CreatedAt, &al.UpdatedAt, &deleted, &al.IsActivityEnabled, &al.Order); err != nil {
		return nil, err
	}
	if deleted.Valid {
		t := deleted.Time
		al.DeletedAt = &t
	}
	if thumb.Valid {
		v := thumb.String
		al.AlbumThumbnailAssetID = &v
	}
	return &al, nil
}

func albumValues(al *domain.Album) []any {
	return []any{
		al.ID, al.OwnerID, al.AlbumName, al.Description, al.AlbumThumbnailAssetID,
		al.CreatedAt, al.UpdatedAt, al.DeletedAt, al.IsActivityEnabled, al.Order,
	}
}

func (s *albumStore) Create(ctx context.Context, al *domain.Album) error {
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", 10), ", ")
	return (*Store)(s).tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO albums (`+albumColumns+`) VALUES (`+placeholders+`)`, albumValues(al)...); err != nil {
			return err
		}
		return writeAlbumMembers(ctx, tx, al)
	})
}

// writeAlbumMembers upserts the ordered asset list and shared users,
// pruning entries that are no longer members. Upsert-instead-of-rewrite
// avoids DuckDB's restriction on deleting and re-inserting the same
// primary key within one transaction.
func writeAlbumMembers(ctx context.Context, tx *sql.Tx, al *domain.Album) error {
	for i, assetID := range al.AssetIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO album_assets (album_id, asset_id, position)
			VALUES (?, ?, ?)
			ON CONFLICT (album_id, asset_id) DO UPDATE SET position = excluded.position`,
			al.ID, assetID, i); err != nil {
			return err
		}
	}
	if len(al.AssetIDs) > 0 {
		marks := strings.TrimSuffix(strings.Repeat("?, ", len(al.AssetIDs)), ", ")
		args := append([]any{al.ID}, assetIDArgs(al.AssetIDs)...)
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM album_assets WHERE album_id = ? AND asset_id NOT IN (`+marks+`)`, args...); err != nil {
			return err
		}
	} else if _, err := tx.ExecContext(ctx,
		`DELETE FROM album_assets WHERE album_id = ?`, al.ID); err != nil {
		return err
	}

	for _, u := range al.Users {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO album_users (album_id, user_id, role)
			VALUES (?, ?, ?)
			ON CONFLICT (album_id, user_id) DO UPDATE SET role = excluded.role`,
			al.ID, u.UserID, u.Role); err != nil {
			return err
		}
	}
	if len(al.Users) > 0 {
		ids := make([]any, 0, len(al.Users))
		for _, u := range al.Users {
			ids = append(ids, u.UserID)
		}
		marks := strings.TrimSuffix(strings.Repeat("?, ", len(al.Users)), ", ")
		args := append([]any{al.ID}, ids...)
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM album_users WHERE album_id = ? AND user_id NOT IN (`+marks+`)`, args...); err != nil {
			return err
		}
	} else if _, err := tx.ExecContext(ctx,
		`DELETE FROM album_users WHERE album_id = ?`, al.ID); err != nil {
		return err
	}
	return nil
}

func assetIDArgs(ids []string) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

func (s *albumStore) Update(ctx context.Context, al *domain.Album) error {
	return (*Store)(s).tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE albums SET
				owner_id = ?, album_name = ?, description = ?, album_thumbnail_asset_id = ?,
				updated_at = ?, deleted_at = ?, is_activity_enabled = ?, sort_order = ?
			WHERE id = ?`,
			al.OwnerID, al.AlbumName, al.Description, al.AlbumThumbnailAssetID,
			al.UpdatedAt, al.DeletedAt, al.IsActivityEnabled, al.Order, al.ID)
		if err != nil {
			return err
		}
		if err := rowsAffected(res, 1); err != nil {
			return err
		}
		return writeAlbumMembers(ctx, tx, al)
	})
}

func (s *albumStore) Delete(ctx context.Context, id string) error {
	return (*Store)(s).tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM album_assets WHERE album_id = ?`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM album_users WHERE album_id = ?`, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM albums WHERE id = ?`, id)
		if err != nil {
			return err
		}
		return rowsAffected(res, 1)
	})
}

func (s *albumStore) Get(ctx context.Context, id string) (*domain.Album, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+albumColumns+` FROM albums WHERE id = ?`, id)
	al, err := scanAlbum(row)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.loadMembers(ctx, al); err != nil {
		return nil, err
	}
	return al, nil
}

func (s *albumStore) loadMembers(ctx context.Context, al *domain.Album) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT asset_id FROM album_assets WHERE album_id = ? ORDER BY position, asset_id`, al.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		al.AssetIDs = append(al.AssetIDs, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	userRows, err := s.db.QueryContext(ctx,
		`SELECT user_id, role FROM album_users WHERE album_id = ? ORDER BY user_id`, al.ID)
	if err != nil {
		return err
	}
	defer userRows.Close()
	for userRows.Next() {
		var u domain.AlbumUser
		if err := userRows.Scan(&u.UserID, &u.Role); err != nil {
			return err
		}
		al.Users = append(al.Users, u)
	}
	al.AssetIndex = make(map[string]bool, len(al.AssetIDs))
	for _, id := range al.AssetIDs {
		al.AssetIndex[id] = true
	}
	return userRows.Err()
}

func (s *albumStore) listWhere(ctx context.Context, where string, args ...any) ([]*domain.Album, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+albumColumns+` FROM albums`+where+` ORDER BY created_at, id`, args...)
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
	for _, al := range out {
		if err := s.loadMembers(ctx, al); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *albumStore) List(ctx context.Context) ([]*domain.Album, error) {
	return s.listWhere(ctx, ``)
}

func (s *albumStore) ListForOwner(ctx context.Context, ownerID string) ([]*domain.Album, error) {
	return s.listWhere(ctx, ` WHERE owner_id = ?`, ownerID)
}
