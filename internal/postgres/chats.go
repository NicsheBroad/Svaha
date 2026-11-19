package postgres

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"
)

var ErrChatUnavailable = errors.New("чат доступен только при взаимном лайке")

type ChatPartner struct {
	ID           string
	FirstName    string
	Unread       int64
	LastActivity time.Time
}

type ChatMessage struct {
	ID         int64
	Own        bool
	Body       string
	SentAt     time.Time
	Edited     bool
	Attachment ChatAttachment
}

type Conversation struct {
	Partner        ChatPartner
	Messages       []ChatMessage
	BeforeID       int64
	LastIncomingID int64
}

const chatPageSize = 50

// ChatPartners возвращает список пользователей, с которыми есть взаимный лайк.
func (s *Store) ChatPartners(ctx context.Context, username string) ([]ChatPartner, error) {
	rows, err := s.db.QueryContext(ctx, `
  SELECT u.search_id::text, u.first_name,
         (SELECT count(*) FROM chat_messages AS incoming
          WHERE incoming.sender = mm.partner AND incoming.recipient = mm.viewer
            AND incoming.id > COALESCE(cr.last_read_id, 0)),
         COALESCE(latest.sent_at, GREATEST(own_vote.reviewed_at, their_vote.reviewed_at)) AS last_activity
  FROM mutual_matches AS mm
  JOIN users AS u ON u.username = mm.partner
  JOIN search_reactions AS own_vote ON own_vote.viewer = mm.viewer AND own_vote.candidate = mm.partner
  JOIN search_reactions AS their_vote ON their_vote.viewer = mm.partner AND their_vote.candidate = mm.viewer
  LEFT JOIN chat_reads AS cr ON cr.viewer = mm.viewer AND cr.partner = mm.partner
  LEFT JOIN LATERAL (
      SELECT sent_at FROM chat_messages
      WHERE (sender = mm.viewer AND recipient = mm.partner)
         OR (sender = mm.partner AND recipient = mm.viewer)
      ORDER BY id DESC LIMIT 1
  ) AS latest ON true
  WHERE mm.viewer = $1
  ORDER BY last_activity DESC, u.first_name, u.search_id`, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var partners []ChatPartner
	for rows.Next() {
		var partner ChatPartner
		if err := rows.Scan(&partner.ID, &partner.FirstName, &partner.Unread, &partner.LastActivity); err != nil {
			return nil, err
		}
		partners = append(partners, partner)
	}
	return partners, rows.Err()
}

// OpenConversation проверяет взаимный лайк и возвращает страницу сообщений.
func (s *Store) OpenConversation(ctx context.Context, username, partnerID string, beforeID int64) (Conversation, error) {
	rows, err := s.db.QueryContext(ctx, `
  SELECT u.search_id::text, u.first_name, m.id, m.sender = $1, m.body, m.sent_at, m.edited_at IS NOT NULL,
         COALESCE(m.attachment_name, ''), COALESCE(m.attachment_type, ''), COALESCE(m.is_voice, false)
  FROM mutual_matches AS mm
  JOIN users AS u ON u.username = mm.partner
  LEFT JOIN LATERAL (
      SELECT id, sender, body, sent_at, edited_at, attachment_name, attachment_type, is_voice
      FROM chat_messages
      WHERE ((sender = $1 AND recipient = u.username) OR (sender = u.username AND recipient = $1))
        AND ($3 = 0 OR id < $3)
      ORDER BY id DESC
      LIMIT $4
  ) AS m ON true
  WHERE mm.viewer = $1 AND u.search_id = $2
  ORDER BY m.id DESC`, username, partnerID, beforeID, chatPageSize+1)
	if err != nil {
		return Conversation{}, err
	}
	defer rows.Close()

	var chat Conversation
	for rows.Next() {
		var id sql.NullInt64
		var own sql.NullBool
		var body sql.NullString
		var sentAt sql.NullTime
		var message ChatMessage
		if err := rows.Scan(&chat.Partner.ID, &chat.Partner.FirstName, &id, &own, &body, &sentAt, &message.Edited,
			&message.Attachment.Name, &message.Attachment.MIME, &message.Attachment.Voice); err != nil {
			return Conversation{}, err
		}
		if id.Valid {
			message.ID, message.Own, message.Body, message.SentAt = id.Int64, own.Bool, body.String, sentAt.Time
			chat.Messages = append(chat.Messages, message)
		}
	}
	if err := rows.Err(); err != nil {
		return Conversation{}, err
	}
	if chat.Partner.ID == "" {
		return Conversation{}, ErrChatUnavailable
	}
	if len(chat.Messages) > chatPageSize {
		chat.Messages = chat.Messages[:chatPageSize]
		chat.BeforeID = chat.Messages[chatPageSize-1].ID
	}
	for _, message := range chat.Messages {
		if !message.Own {
			chat.LastIncomingID = message.ID
			break
		}
	}
	slices.Reverse(chat.Messages)
	return chat, nil
}

// MarkChatRead отмечает видимое входящее сообщение и более ранние сообщения прочитанными.
func (s *Store) MarkChatRead(ctx context.Context, username, partnerID string, messageID int64) error {
	tx, partner, err := s.beginProfilePair(ctx, username, partnerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
  INSERT INTO chat_reads (viewer, partner, last_read_id)
  SELECT mm.viewer, mm.partner, m.id FROM mutual_matches AS mm
  JOIN chat_messages AS m ON m.recipient = mm.viewer AND m.sender = mm.partner
  WHERE mm.viewer = $1 AND mm.partner = $2 AND m.id = $3
  ON CONFLICT (viewer, partner) DO UPDATE SET last_read_id = EXCLUDED.last_read_id
  WHERE chat_reads.last_read_id < EXCLUDED.last_read_id`, username, partner, messageID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteConversation удаляет историю чата и сбрасывает реакции обоих участников.
func (s *Store) DeleteConversation(ctx context.Context, username, partnerID string) error {
	tx, partner, err := s.beginProfilePair(ctx, username, partnerID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrChatUnavailable
	}
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
  DELETE FROM search_reactions AS r
  WHERE ((r.viewer = $1 AND r.candidate = $2) OR (r.viewer = $2 AND r.candidate = $1))
    AND EXISTS (SELECT 1 FROM mutual_matches WHERE viewer = $1 AND partner = $2)`, username, partner)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 2 {
		return ErrChatUnavailable
	}
	if _, err := tx.ExecContext(ctx, `
  DELETE FROM chat_messages
  WHERE (sender = $1 AND recipient = $2) OR (sender = $2 AND recipient = $1)`, username, partner); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
  DELETE FROM chat_reads
  WHERE (viewer = $1 AND partner = $2) OR (viewer = $2 AND partner = $1)`, username, partner); err != nil {
		return err
	}
	return tx.Commit()
}
