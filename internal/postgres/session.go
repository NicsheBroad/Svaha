package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"time"
)

type Viewer struct {
	Username   string
	LookingFor string
}

var ErrSessionMissing = errors.New("сессия не найдена")

// CreateSession удаляет просроченные сессии и создаёт токен для входа.
func (s *Store) CreateSession(ctx context.Context, username string, expiresAt time.Time) (string, error) {
	token := rand.Text()
	hash := sha256.Sum256([]byte(token))
	_, err := s.db.ExecContext(ctx, `
  WITH expired AS (DELETE FROM sessions WHERE expires_at <= now())
  INSERT INTO sessions (token_hash, username, expires_at) VALUES ($1, $2, $3)`,
		hash[:], username, expiresAt)
	if err != nil {
		return "", err
	}
	return token, nil
}

// SessionViewer загружает логин и поисковое предпочтение по действующему токену.
func (s *Store) SessionViewer(ctx context.Context, token string) (Viewer, error) {
	hash := sha256.Sum256([]byte(token))
	var viewer Viewer
	err := s.db.QueryRowContext(ctx, `
  SELECT u.username, u.looking_for
  FROM sessions AS s
  JOIN users AS u ON u.username = s.username
  WHERE s.token_hash = $1 AND s.expires_at > now()`, hash[:]).
		Scan(&viewer.Username, &viewer.LookingFor)
	if errors.Is(err, sql.ErrNoRows) {
		return Viewer{}, ErrSessionMissing
	}
	return viewer, err
}

// DeleteSession завершает сессию с указанным токеном.
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	hash := sha256.Sum256([]byte(token))
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = $1", hash[:])
	return err
}
