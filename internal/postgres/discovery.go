package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lib/pq"
)

type SearchProfile struct {
	ID        string
	FirstName string
	City      string
	Hobbies   string
}

type PublicProfile struct {
	ID        string
	FirstName string
	City      string
	Bio       string
	Hobbies   string
	PhotoIDs  []int64
}

// NextSearchProfile показывает анкеты без чата, начиная с новых и давно просмотренных.
func (s *Store) NextSearchProfile(ctx context.Context, username, gender, city string) (SearchProfile, error) {
	var profile SearchProfile
	err := s.db.QueryRowContext(ctx, `
  SELECT u.search_id::text, u.first_name, u.city, u.hobbies
  FROM users AS u
  LEFT JOIN search_reactions AS r ON r.viewer = $1 AND r.candidate = u.username
  WHERE u.username <> $1
    AND ($2 = '' OR u.gender = $2)
    AND ($3 = '' OR strpos(lower(u.city COLLATE pg_catalog.pg_unicode_fast), lower($3 COLLATE pg_catalog.pg_unicode_fast)) > 0)
    AND NOT EXISTS (
        SELECT 1 FROM conversation_pairs AS c
        WHERE c.viewer = $1 AND c.partner = u.username
    )
    AND NOT EXISTS (SELECT 1 FROM blocked_pairs AS b WHERE b.viewer = $1 AND b.partner = u.username)
  ORDER BY r.reviewed_at ASC NULLS FIRST, u.first_name, u.username
  LIMIT 1`, username, gender, city).Scan(&profile.ID, &profile.FirstName, &profile.City, &profile.Hobbies)
	return profile, err
}

// RecordSearchReaction обновляет реакцию на анкету, с которой ещё нет чата.
func (s *Store) RecordSearchReaction(ctx context.Context, username, candidateID string, liked bool) error {
	tx, candidate, err := s.beginProfilePair(ctx, username, candidateID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
  INSERT INTO search_reactions (viewer, candidate, liked)
  SELECT $1, $2, $3
  WHERE NOT EXISTS (
      SELECT 1 FROM conversation_pairs WHERE viewer = $1 AND partner = $2
  )
    AND NOT EXISTS (SELECT 1 FROM blocked_pairs WHERE viewer = $1 AND partner = $2)
  ON CONFLICT (viewer, candidate) DO UPDATE
  SET liked = EXCLUDED.liked, reviewed_at = clock_timestamp()`, username, candidate, liked)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// IncomingLikes возвращает входящие лайки от людей, с которыми ещё нет чата.
func (s *Store) IncomingLikes(ctx context.Context, username string) ([]SearchProfile, error) {
	rows, err := s.db.QueryContext(ctx, `
  SELECT u.search_id::text, u.first_name, u.city
  FROM search_reactions AS r
  JOIN users AS u ON u.username = r.viewer
  WHERE r.candidate = $1 AND r.liked
    AND NOT EXISTS (
        SELECT 1 FROM conversation_pairs AS c
        WHERE c.viewer = $1 AND c.partner = u.username
    )
    AND NOT EXISTS (SELECT 1 FROM blocked_pairs AS b WHERE b.viewer = $1 AND b.partner = u.username)
  ORDER BY r.reviewed_at DESC, u.search_id`, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanProfileSummaries(rows)
}

// scanProfileSummaries считывает краткие сведения об анкетах из результатов запроса.
func scanProfileSummaries(rows *sql.Rows) ([]SearchProfile, error) {
	var profiles []SearchProfile
	for rows.Next() {
		var profile SearchProfile
		if err := rows.Scan(&profile.ID, &profile.FirstName, &profile.City); err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

// PublicProfile загружает поля анкеты, которые видны другим вошедшим пользователям.
func (s *Store) PublicProfile(ctx context.Context, viewer, profileID string) (PublicProfile, error) {
	var profile PublicProfile
	err := s.db.QueryRowContext(ctx, `
  SELECT u.search_id::text, u.first_name, u.city, u.bio, u.hobbies,
         ARRAY(SELECT p.id FROM profile_photos AS p WHERE p.username = u.username ORDER BY p.id)
  FROM users AS u
  WHERE u.search_id = $2 AND u.username <> $1
    AND NOT EXISTS (SELECT 1 FROM blocked_pairs AS b WHERE b.viewer = $1 AND b.partner = u.username)`, viewer, profileID).
		Scan(&profile.ID, &profile.FirstName, &profile.City, &profile.Bio, &profile.Hobbies, pq.Array(&profile.PhotoIDs))
	return profile, err
}

// PublicProfilePhoto возвращает фотографию только из галереи указанной анкеты.
func (s *Store) PublicProfilePhoto(ctx context.Context, viewer, profileID string, photoID int64) ([]byte, error) {
	var photo []byte
	err := s.db.QueryRowContext(ctx, `
  SELECT p.content
  FROM profile_photos AS p
  JOIN users AS u ON u.username = p.username
  WHERE u.search_id = $2 AND u.username <> $1
    AND NOT EXISTS (SELECT 1 FROM blocked_pairs AS b WHERE b.viewer = $1 AND b.partner = u.username)
    AND p.id = $3`, viewer, profileID, photoID).Scan(&photo)
	return photo, err
}

// ReplyToLike отвечает на действующий входящий лайк, если между пользователями нет блокировки.
func (s *Store) ReplyToLike(ctx context.Context, username, profileID string) (bool, error) {
	tx, partner, err := s.beginProfilePair(ctx, username, profileID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var available bool
	err = tx.QueryRowContext(ctx, `
  SELECT EXISTS (SELECT 1 FROM search_reactions WHERE viewer = $2 AND candidate = $1 AND liked)
     AND NOT EXISTS (SELECT 1 FROM blocked_pairs WHERE viewer = $1 AND partner = $2)`, username, partner).Scan(&available)
	if err != nil || !available {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
  INSERT INTO search_reactions (viewer, candidate, liked) VALUES ($1, $2, true)
  ON CONFLICT (viewer, candidate) DO UPDATE SET liked = true, reviewed_at = clock_timestamp()
  WHERE NOT search_reactions.liked`, username, partner); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// HasMatch проверяет доступ к чату по публичному ID собеседника.
func (s *Store) HasMatch(ctx context.Context, username, profileID string) (bool, error) {
	var matched bool
	err := s.db.QueryRowContext(ctx, `
  SELECT EXISTS (
      SELECT 1 FROM mutual_matches AS mm JOIN users AS u ON u.username = mm.partner
      WHERE mm.viewer = $1 AND u.search_id = $2
  )`, username, profileID).Scan(&matched)
	return matched, err
}
