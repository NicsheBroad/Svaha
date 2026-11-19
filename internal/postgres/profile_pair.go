package postgres

import (
	"context"
	"database/sql"
)

// beginProfilePair блокирует обоих участников в одинаковом порядке перед изменением реакций или чата.
func (s *Store) beginProfilePair(ctx context.Context, username, partnerID string) (*sql.Tx, string, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, "", err
	}
	var partner string
	err = tx.QueryRowContext(ctx, `
  WITH locked AS MATERIALIZED (
      SELECT username, search_id
      FROM users
      WHERE username = $1 OR search_id = $2
      ORDER BY username
      FOR NO KEY UPDATE
  )
  SELECT partner.username
  FROM locked AS viewer
  CROSS JOIN locked AS partner
  WHERE viewer.username = $1 AND partner.search_id = $2 AND partner.username <> $1`,
		username, partnerID).Scan(&partner)
	if err != nil {
		tx.Rollback()
		return nil, "", err
	}
	return tx, partner, nil
}
