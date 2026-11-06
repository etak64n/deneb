package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// User is an administrator of the dashboard.
type User struct {
	ID           string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

// APIKey is a key of the API. The secret is known only when the key is created.
type APIKey struct {
	ID         string
	Name       string
	Prefix     string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// CountUsers returns the number of administrators.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

// CreateFirstUser creates the first administrator, and fails once one exists, so
// that two browsers that set up deneb at the same time cannot both succeed.
func (s *Store) CreateFirstUser(ctx context.Context, id, email, passwordHash string, now time.Time) (User, error) {
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, created_at) SELECT ?, ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM users)`,
		id, email, passwordHash, millis(now))
	if err != nil {
		return User{}, err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return User{}, ErrExists
	}
	return User{ID: id, Email: email, PasswordHash: passwordHash, CreatedAt: now}, nil
}

// UserByEmail returns the administrator with the email.
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, `SELECT id, email, password_hash, created_at FROM users WHERE email = ?`, email))
}

func (s *Store) scanUser(row *sql.Row) (User, error) {
	var user User
	var created int64
	err := row.Scan(&user.ID, &user.Email, &user.PasswordHash, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	user.CreatedAt = fromMillis(created)
	return user, err
}

// CreateSession records a signed-in browser by the hash of its token.
func (s *Store) CreateSession(ctx context.Context, tokenHash, userID string, expires time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`, tokenHash, userID, millis(expires))
	return err
}

// SessionUser returns the administrator of a session that has not expired.
func (s *Store) SessionUser(ctx context.Context, tokenHash string, now time.Time) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT users.id, users.email, users.password_hash, users.created_at FROM sessions JOIN users ON users.id = sessions.user_id
		 WHERE sessions.token_hash = ? AND sessions.expires_at > ?`, tokenHash, millis(now)))
}

// DeleteSession signs a browser out.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// DeleteExpiredSessions removes the sessions that can no longer sign in.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, millis(now))
	return err
}

// CreateAPIKey records a key of the API by the hash of its secret.
func (s *Store) CreateAPIKey(ctx context.Context, id, name, prefix, secretHash string, now time.Time) (APIKey, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO api_keys (id, name, prefix, secret_hash, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, name, prefix, secretHash, millis(now))
	return APIKey{ID: id, Name: name, Prefix: prefix, CreatedAt: now}, err
}

// ListAPIKeys returns every key of the API, the newest first.
func (s *Store) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, prefix, created_at, last_used_at FROM api_keys ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []APIKey{}
	for rows.Next() {
		var key APIKey
		var created int64
		var used sql.NullInt64
		if err := rows.Scan(&key.ID, &key.Name, &key.Prefix, &created, &used); err != nil {
			return nil, err
		}
		key.CreatedAt = fromMillis(created)
		key.LastUsedAt = nullableTime(used)
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// UseAPIKey finds the key with the hash of a secret, and records that it was used.
// It writes the time of use at most once a minute, so that busy clients do not
// turn every request into a write.
func (s *Store) UseAPIKey(ctx context.Context, secretHash string, now time.Time) (APIKey, error) {
	var key APIKey
	var created int64
	var used sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id, name, prefix, created_at, last_used_at FROM api_keys WHERE secret_hash = ?`, secretHash).
		Scan(&key.ID, &key.Name, &key.Prefix, &created, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return APIKey{}, ErrNotFound
	}
	if err != nil {
		return APIKey{}, err
	}
	key.CreatedAt = fromMillis(created)
	key.LastUsedAt = nullableTime(used)
	if !used.Valid || now.Sub(fromMillis(used.Int64)) >= time.Minute {
		if _, err := s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`, millis(now), key.ID); err != nil {
			return APIKey{}, err
		}
		key.LastUsedAt = &now
	}
	return key, nil
}

// DeleteAPIKey revokes a key of the API.
func (s *Store) DeleteAPIKey(ctx context.Context, id string) error {
	return expectRow(s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ?`, id))
}

// expectRow turns a change that touched no row into ErrNotFound.
func expectRow(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return ErrNotFound
	}
	return nil
}
