package postgres

import (
	"context"
	"database/sql"
	"errors"
)

var ErrProfileUnavailable = errors.New("профиль недоступен")

// SetProfileBlock скрывает участников друг от друга или отменяет блокировку.
func (s *Store) SetProfileBlock(ctx context.Context, username, profileID string, blocked bool) error {
	tx, partner, err := s.beginProfilePair(ctx, username, profileID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrProfileUnavailable
	}
	if err != nil {
		return err
	}
	defer tx.Rollback()

	statement := "DELETE FROM profile_blocks WHERE blocker = $1 AND blocked = $2"
	if blocked {
		statement = "INSERT INTO profile_blocks (blocker, blocked) VALUES ($1, $2) ON CONFLICT DO NOTHING"
	}
	if _, err := tx.ExecContext(ctx, statement, username, partner); err != nil {
		return err
	}
	return tx.Commit()
}

// BlockedProfiles возвращает анкеты, заблокированные самим пользователем.
func (s *Store) BlockedProfiles(ctx context.Context, username string) ([]SearchProfile, error) {
	rows, err := s.db.QueryContext(ctx, `
  SELECT u.search_id::text, u.first_name, u.city FROM profile_blocks AS b
  JOIN users AS u ON u.username = b.blocked WHERE b.blocker = $1
  ORDER BY u.first_name, u.search_id`, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProfileSummaries(rows)
}

// ReportProfile записывает причину жалобы и обновляет предыдущую жалобу этой пары.
func (s *Store) ReportProfile(ctx context.Context, username, profileID, reason, details string) error {
	result, err := s.db.ExecContext(ctx, `
  INSERT INTO profile_reports (reporter, subject, reason, details)
  SELECT $1, username, $3, $4 FROM users WHERE search_id = $2 AND username <> $1
  ON CONFLICT (reporter, subject) DO UPDATE
  SET reason = EXCLUDED.reason, details = EXCLUDED.details, updated_at = now()`, username, profileID, reason, details)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return ErrProfileUnavailable
	}
	return err
}
