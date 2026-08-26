package duckstore

import (
	"context"
	"database/sql"
	"strings"

	"immich-go/internal/domain"
	"immich-go/internal/store"
)

// ---- users ----

type userStore Store

const userColumns = `id, email, password, name, is_admin, should_change_password,
	avatar_color, profile_image_path, storage_label, is_onboarded, preferences,
	created_at, updated_at, deleted_at`

func scanUser(scanner interface{ Scan(...any) error }) (*domain.User, error) {
	var u domain.User
	var deleted sql.NullTime
	var prefs sql.NullString
	if err := scanner.Scan(&u.ID, &u.Email, &u.Password, &u.Name, &u.IsAdmin,
		&u.ShouldChangePassword, &u.AvatarColor, &u.ProfileImagePath, &u.StorageLabel,
		&u.IsOnboarded, &prefs, &u.CreatedAt, &u.UpdatedAt, &deleted); err != nil {
		return nil, err
	}
	if prefs.Valid {
		u.Preferences = prefs.String
	}
	if deleted.Valid {
		t := deleted.Time
		u.DeletedAt = &t
	}
	return &u, nil
}

func (s *userStore) Create(ctx context.Context, u *domain.User) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (`+userColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Email, u.Password, u.Name, u.IsAdmin, u.ShouldChangePassword,
		u.AvatarColor, u.ProfileImagePath, u.StorageLabel, u.IsOnboarded, u.Preferences,
		u.CreatedAt, u.UpdatedAt, u.DeletedAt)
	if isUniqueViolation(err) {
		return store.ErrConflict
	}
	return err
}

func (s *userStore) Update(ctx context.Context, u *domain.User) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE users SET
			email = ?, password = ?, name = ?, is_admin = ?, should_change_password = ?,
			avatar_color = ?, profile_image_path = ?, storage_label = ?, is_onboarded = ?,
			preferences = ?, updated_at = ?, deleted_at = ?
		WHERE id = ?`,
		u.Email, u.Password, u.Name, u.IsAdmin, u.ShouldChangePassword,
		u.AvatarColor, u.ProfileImagePath, u.StorageLabel, u.IsOnboarded,
		u.Preferences, u.UpdatedAt, u.DeletedAt, u.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrConflict
		}
		return err
	}
	return rowsAffected(res, 1)
}

func (s *userStore) Delete(ctx context.Context, id string) error {
	// Users are referenced by sessions/api-keys/assets; remove dependents
	// first so re-registration by email stays clean.
	return (*Store)(s).tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM api_keys WHERE user_id = ?`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM sync_acks WHERE user_id = ?`, id); err != nil {
			return err
		}
		// Owned albums take their join rows with them.
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM album_assets WHERE album_id IN (SELECT id FROM albums WHERE owner_id = ?)`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM album_users WHERE album_id IN (SELECT id FROM albums WHERE owner_id = ?)`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM albums WHERE owner_id = ?`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM album_users WHERE user_id = ?`, id); err != nil {
			return err
		}
		// Memories and their assets.
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM memory_assets WHERE memory_id IN (SELECT id FROM memories WHERE owner_id = ?)`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM memories WHERE owner_id = ?`, id); err != nil {
			return err
		}
		// Assets: dependent rows first, then the rows themselves.
		if _, err := tx.ExecContext(ctx, `DELETE FROM album_assets WHERE asset_id IN (SELECT id FROM assets WHERE owner_id = ?)`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM memory_assets WHERE asset_id IN (SELECT id FROM assets WHERE owner_id = ?)`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM asset_exifs WHERE asset_id IN (SELECT id FROM assets WHERE owner_id = ?)`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM assets WHERE owner_id = ?`, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
		if err != nil {
			return err
		}
		return rowsAffected(res, 1)
	})
}

func (s *userStore) Get(ctx context.Context, id string) (*domain.User, error) {
	row := s.ro.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return u, err
}

func (s *userStore) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	row := s.ro.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE email = ?`, email)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return u, err
}

func (s *userStore) List(ctx context.Context) ([]*domain.User, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY created_at, id`)
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

func (s *userStore) Count(ctx context.Context) (int, error) {
	var n int
	err := s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// ---- sessions ----

type sessionStore Store

const sessionColumns = `id, token_hash, user_id, device_os, device_type, app_version,
	created_at, updated_at, expires_at`

type rowScanner interface{ Scan(...any) error }

func scanSession(scanner rowScanner) (*domain.Session, error) {
	var sess domain.Session
	var expires sql.NullTime
	if err := scanner.Scan(&sess.ID, &sess.TokenHash, &sess.UserID, &sess.DeviceOS,
		&sess.DeviceType, &sess.AppVersion, &sess.CreatedAt, &sess.UpdatedAt, &expires); err != nil {
		return nil, err
	}
	if expires.Valid {
		t := expires.Time
		sess.ExpiresAt = &t
	}
	return &sess, nil
}

func (s *sessionStore) Create(ctx context.Context, sess *domain.Session) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (`+sessionColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.TokenHash, sess.UserID, sess.DeviceOS, sess.DeviceType,
		sess.AppVersion, sess.CreatedAt, sess.UpdatedAt, sess.ExpiresAt)
	return err
}

func (s *sessionStore) Update(ctx context.Context, sess *domain.Session) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET
			token_hash = ?, user_id = ?, device_os = ?, device_type = ?, app_version = ?,
			updated_at = ?, expires_at = ?
		WHERE id = ?`,
		sess.TokenHash, sess.UserID, sess.DeviceOS, sess.DeviceType, sess.AppVersion,
		sess.UpdatedAt, sess.ExpiresAt, sess.ID)
	if err != nil {
		return err
	}
	return rowsAffected(res, 1)
}

func (s *sessionStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return rowsAffected(res, 1)
}

func (s *sessionStore) DeleteAllForUser(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

func (s *sessionStore) Get(ctx context.Context, id string) (*domain.Session, error) {
	row := s.ro.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE id = ?`, id)
	sess, err := scanSession(row)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return sess, err
}

func (s *sessionStore) GetByTokenHash(ctx context.Context, hash []byte) (*domain.Session, error) {
	row := s.ro.QueryRowContext(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE token_hash = ? LIMIT 1`, hash)
	sess, err := scanSession(row)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return sess, err
}

func (s *sessionStore) ListForUser(ctx context.Context, userID string) ([]*domain.Session, error) {
	rows, err := s.ro.QueryContext(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE user_id = ? ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// ---- api keys ----

type apiKeyStore Store

const apiKeyColumns = `id, name, key_hash, user_id, permissions, created_at, updated_at`

func scanAPIKey(scanner rowScanner) (*domain.APIKey, error) {
	var k domain.APIKey
	var perms string
	if err := scanner.Scan(&k.ID, &k.Name, &k.KeyHash, &k.UserID, &perms,
		&k.CreatedAt, &k.UpdatedAt); err != nil {
		return nil, err
	}
	k.Permissions = splitPermissions(perms)
	return &k, nil
}

func (s *apiKeyStore) Create(ctx context.Context, k *domain.APIKey) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api_keys (`+apiKeyColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		k.ID, k.Name, k.KeyHash, k.UserID, joinPermissions(k.Permissions),
		k.CreatedAt, k.UpdatedAt)
	return err
}

func (s *apiKeyStore) Update(ctx context.Context, k *domain.APIKey) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE api_keys SET
			name = ?, key_hash = ?, user_id = ?, permissions = ?, updated_at = ?
		WHERE id = ?`,
		k.Name, k.KeyHash, k.UserID, joinPermissions(k.Permissions), k.UpdatedAt, k.ID)
	if err != nil {
		return err
	}
	return rowsAffected(res, 1)
}

func (s *apiKeyStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return rowsAffected(res, 1)
}

func (s *apiKeyStore) Get(ctx context.Context, id string) (*domain.APIKey, error) {
	row := s.ro.QueryRowContext(ctx, `SELECT `+apiKeyColumns+` FROM api_keys WHERE id = ?`, id)
	k, err := scanAPIKey(row)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return k, err
}

func (s *apiKeyStore) GetByKeyHash(ctx context.Context, hash []byte) (*domain.APIKey, error) {
	row := s.ro.QueryRowContext(ctx,
		`SELECT `+apiKeyColumns+` FROM api_keys WHERE key_hash = ? LIMIT 1`, hash)
	k, err := scanAPIKey(row)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return k, err
}

func (s *apiKeyStore) ListForUser(ctx context.Context, userID string) ([]*domain.APIKey, error) {
	rows, err := s.ro.QueryContext(ctx,
		`SELECT `+apiKeyColumns+` FROM api_keys WHERE user_id = ? ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.APIKey
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// ---- shared helpers ----

func rowsAffected(res sql.Result, want int64) error {
	n, err := res.RowsAffected()
	if err != nil {
		return nil // driver may not report; assume success
	}
	if n != want {
		return store.ErrNotFound
	}
	return nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// DuckDB raises 'Constraint Error: Duplicate key "..." violates
	// unique constraint.' — matching case-insensitively on the stable
	// phrases keeps this driver-agnostic.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "duplicate key")
}
