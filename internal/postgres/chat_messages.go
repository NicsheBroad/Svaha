package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

var ErrMessageUnavailable = errors.New("сообщение недоступно")

type ChatAttachment struct {
	Name  string
	MIME  string
	Data  []byte
	Voice bool
}

// Kind возвращает тип вложения для выбора элемента просмотра в чате.
func (a ChatAttachment) Kind() string {
	kind, _, _ := strings.Cut(a.MIME, "/")
	return kind
}

// SendChatMessage сохраняет текст и вложение в чате с взаимным лайком.
func (s *Store) SendChatMessage(ctx context.Context, username, partnerID, nonce, body string, attachment ChatAttachment) error {
	tx, partner, err := s.beginProfilePair(ctx, username, partnerID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrChatUnavailable
	}
	if err != nil {
		return err
	}
	defer tx.Rollback()

	reservation, err := tx.ExecContext(ctx, `
  INSERT INTO chat_send_requests (sender, nonce, recipient)
  SELECT viewer, $3, partner FROM mutual_matches
  WHERE viewer = $1 AND partner = $2
  ON CONFLICT (sender, nonce) DO NOTHING`, username, partner, nonce)
	if err != nil {
		return err
	}
	reserved, err := reservation.RowsAffected()
	if err != nil {
		return err
	}
	if reserved == 0 {
		var alreadySent bool
		err := tx.QueryRowContext(ctx, `
   SELECT EXISTS (SELECT 1 FROM chat_send_requests WHERE sender = $1 AND nonce = $2 AND recipient = $3)`,
			username, nonce, partner).Scan(&alreadySent)
		if err != nil {
			return err
		}
		if alreadySent {
			return nil
		}
		return ErrChatUnavailable
	}

	result, err := tx.ExecContext(ctx, `
  INSERT INTO chat_messages (sender, recipient, body, attachment_name, attachment_type, attachment_data, is_voice)
  SELECT viewer, partner, $3, $4, $5, NULLIF($6::bytea, ''::bytea), $7 FROM mutual_matches
  WHERE viewer = $1 AND partner = $2`, username, partner, body, attachment.Name, attachment.MIME, attachment.Data, attachment.Voice)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrChatUnavailable
	}
	return tx.Commit()
}

// EditableChatMessage загружает текст и метаданные собственного сообщения.
func (s *Store) EditableChatMessage(ctx context.Context, username, partnerID string, id int64) (ChatMessage, error) {
	var message ChatMessage
	err := s.db.QueryRowContext(ctx, `
  SELECT m.id, m.body, m.attachment_name, m.attachment_type, m.is_voice
  FROM chat_messages AS m
  JOIN mutual_matches AS mm ON mm.viewer = m.sender AND mm.partner = m.recipient
  JOIN users AS u ON u.username = mm.partner
  WHERE m.sender = $1 AND u.search_id = $2 AND m.id = $3`, username, partnerID, id).
		Scan(&message.ID, &message.Body, &message.Attachment.Name, &message.Attachment.MIME, &message.Attachment.Voice)
	if errors.Is(err, sql.ErrNoRows) {
		return ChatMessage{}, ErrMessageUnavailable
	}
	return message, err
}

// EditChatMessage обновляет текст собственного сообщения, сохраняя вложение.
func (s *Store) EditChatMessage(ctx context.Context, username, partnerID string, id int64, body string) error {
	return s.mutateOwnChatMessage(ctx, username, partnerID, id, `
  UPDATE chat_messages SET body = $4,
      edited_at = CASE WHEN body IS DISTINCT FROM $4 THEN now() ELSE edited_at END
  WHERE sender = $1 AND recipient = $2 AND id = $3
    AND ($4 <> '' OR attachment_data IS NOT NULL)
    AND EXISTS (SELECT 1 FROM mutual_matches WHERE viewer = $1 AND partner = $2)`, body)
}

// DeleteChatMessage удаляет собственное сообщение вместе с вложением.
func (s *Store) DeleteChatMessage(ctx context.Context, username, partnerID string, id int64) error {
	return s.mutateOwnChatMessage(ctx, username, partnerID, id, `
  DELETE FROM chat_messages WHERE sender = $1 AND recipient = $2 AND id = $3
    AND EXISTS (SELECT 1 FROM mutual_matches WHERE viewer = $1 AND partner = $2)`)
}

// mutateOwnChatMessage выполняет изменение сообщения под блокировкой участников.
func (s *Store) mutateOwnChatMessage(ctx context.Context, username, partnerID string, id int64, statement string, values ...any) error {
	tx, partner, err := s.beginProfilePair(ctx, username, partnerID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrMessageUnavailable
	}
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, statement, append([]any{username, partner, id}, values...)...)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrMessageUnavailable
	}
	return tx.Commit()
}
