package postgres_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
	"svaha/internal/postgres"
)

const aliceID = "59704cf5-979e-4c2b-bca4-7db32ac5a168"
const bobID = "b025b063-ff17-4442-b93c-559538babcd3"
const caraID = "c5058150-f08b-49db-9f71-385f20d96146"

// TestPostgresWorkflows проверяет SQL-сценарии поиска, чатов и защиты пользователей.
func TestPostgresWorkflows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	store, db := openTestStore(t, ctx)

	_, err := db.ExecContext(ctx, `
  INSERT INTO users (username,email,first_name,phone,birth_date,city,gender,looking_for,password_salt,password_hash,search_id)
  VALUES ('alice','a@example.test','Алиса','','1990-01-01','Тестград','female','','','',$1),
         ('bob','b@example.test','Борис','','1990-01-01','Тестград','male','','','',$2),
         ('cara','c@example.test','Клара','','1990-01-01','Тестград','female','','','',$3)`, aliceID, bobID, caraID)
	requireOK(t, err)

	candidate, err := store.NextSearchProfile(ctx, "alice", "male", "тестград")
	requireOK(t, err)
	if candidate.ID != bobID {
		t.Fatal("Борис отсутствует в поиске")
	}
	requireOK(t, store.RecordSearchReaction(ctx, "bob", aliceID, true))
	likes, err := store.IncomingLikes(ctx, "alice")
	requireOK(t, err)
	if len(likes) != 1 || likes[0].ID != bobID {
		t.Fatal("входящий лайк Бориса отсутствует")
	}
	matched, err := store.ReplyToLike(ctx, "alice", bobID)
	requireOK(t, err)
	if !matched {
		t.Fatal("ответный лайк не создал чат")
	}
	if _, err := store.NextSearchProfile(ctx, "alice", "male", ""); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("совпавшая анкета осталась в поиске: %v", err)
	}
	likes, err = store.IncomingLikes(ctx, "alice")
	requireOK(t, err)
	if len(likes) != 0 {
		t.Fatal("совпавшая анкета осталась во входящих")
	}

	// Отправляет один и тот же запрос параллельно от двух вкладок.
	media := bytes.Repeat([]byte("media"), 130000)
	attachment := postgres.ChatAttachment{Name: "demo.webm", MIME: "audio/webm", Data: media, Voice: true}
	nonce := rand.Text()
	result := make(chan error, 2)
	for i := 0; i < cap(result); i++ {
		// Проверяет повторную отправку на уровне реального PostgreSQL.
		go func() {
			result <- store.SendChatMessage(ctx, "alice", bobID, nonce, "", attachment)
		}()
	}
	for i := 0; i < cap(result); i++ {
		requireOK(t, <-result)
	}
	chat, err := store.OpenConversation(ctx, "bob", aliceID, 0)
	requireOK(t, err)
	if len(chat.Messages) != 1 || chat.LastIncomingID != chat.Messages[0].ID {
		t.Fatal("повторная отправка создала дубликат или потеряла входящее сообщение")
	}
	messageID := chat.LastIncomingID

	partners, err := store.ChatPartners(ctx, "bob")
	requireOK(t, err)
	if len(partners) != 1 || partners[0].Unread != 1 {
		t.Fatal("для Бориса ожидается одно непрочитанное")
	}
	requireOK(t, store.MarkChatRead(ctx, "bob", aliceID, messageID))
	partners, err = store.ChatPartners(ctx, "bob")
	requireOK(t, err)
	if partners[0].Unread != 0 {
		t.Fatal("прочтение не сбросило счётчик")
	}

	if _, _, err := store.OpenMessageAttachment(ctx, "cara", aliceID, messageID); !errors.Is(err, postgres.ErrMessageUnavailable) {
		t.Fatalf("постороннему доступно вложение: %v", err)
	}
	metadata, mediaReader, err := store.OpenMessageAttachment(ctx, "bob", aliceID, messageID)
	requireOK(t, err)
	if metadata.Size != int64(len(media)) {
		t.Fatal("размер вложения неверен")
	}
	_, err = mediaReader.Seek(300001, io.SeekStart)
	requireOK(t, err)
	chunk := make([]byte, 777)
	_, err = io.ReadFull(mediaReader, chunk)
	requireOK(t, err)
	if !bytes.Equal(chunk, media[300001:300778]) {
		t.Fatal("перемотка вернула неверные байты")
	}

	requireOK(t, store.RecordSearchReaction(ctx, "cara", aliceID, true))
	requireOK(t, store.SetProfileBlock(ctx, "alice", caraID, true))
	likes, err = store.IncomingLikes(ctx, "alice")
	requireOK(t, err)
	if len(likes) != 0 {
		t.Fatal("лайк от заблокированной анкеты виден")
	}
	if matched, err := store.ReplyToLike(ctx, "alice", caraID); err != nil || matched {
		t.Fatalf("ответный лайк обошёл блокировку: %v", err)
	}
	if _, err := store.NextSearchProfile(ctx, "cara", "female", ""); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("блокировка не скрыла анкету от второй стороны: %v", err)
	}
	requireOK(t, store.SetProfileBlock(ctx, "alice", caraID, false))
	matched, err = store.ReplyToLike(ctx, "alice", caraID)
	requireOK(t, err)
	if !matched {
		t.Fatal("разблокированный входящий лайк не восстановился")
	}
	requireOK(t, store.SendChatMessage(ctx, "cara", aliceID, rand.Text(), "Новая активность", postgres.ChatAttachment{}))
	partners, err = store.ChatPartners(ctx, "alice")
	requireOK(t, err)
	if len(partners) != 2 || partners[0].ID != caraID || partners[0].Unread != 1 {
		t.Fatal("чаты не отсортированы по активности")
	}
	if _, _, err := store.OpenMessageAttachment(ctx, "alice", caraID, messageID); !errors.Is(err, postgres.ErrMessageUnavailable) {
		t.Fatalf("чужое вложение доступно через другой чат: %v", err)
	}

	requireOK(t, store.ReportProfile(ctx, "alice", bobID, "spam", "Повторяющаяся реклама"))
	var reportCount int
	requireOK(t, db.QueryRowContext(ctx, "SELECT count(*) FROM profile_reports WHERE reporter = 'alice' AND subject = 'bob' AND reason = 'spam'").Scan(&reportCount))
	if reportCount != 1 {
		t.Fatal("жалоба не сохранена")
	}
	var photoID int64
	requireOK(t, db.QueryRowContext(ctx, "INSERT INTO profile_photos (username, content) VALUES ('bob', 'test') RETURNING id").Scan(&photoID))
	_, err = store.PublicProfilePhoto(ctx, "alice", bobID, photoID)
	requireOK(t, err)
	requireOK(t, store.SetProfileBlock(ctx, "bob", aliceID, true))
	if _, err := store.OpenConversation(ctx, "alice", bobID, 0); !errors.Is(err, postgres.ErrChatUnavailable) {
		t.Fatalf("после блокировки чат доступен: %v", err)
	}
	if _, err := store.PublicProfile(ctx, "alice", bobID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("после блокировки анкета доступна: %v", err)
	}
	if _, err := store.PublicProfilePhoto(ctx, "alice", bobID, photoID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("после блокировки фото профиля доступно: %v", err)
	}
	if err := store.SendChatMessage(ctx, "alice", bobID, rand.Text(), "Проверка", postgres.ChatAttachment{}); !errors.Is(err, postgres.ErrChatUnavailable) {
		t.Fatalf("после блокировки отправка доступна: %v", err)
	}
	_, err = mediaReader.Seek(0, io.SeekStart)
	requireOK(t, err)
	if _, err := mediaReader.Read(chunk); !errors.Is(err, postgres.ErrMessageUnavailable) {
		t.Fatalf("новый фрагмент доступен после блокировки: %v", err)
	}
	requireOK(t, store.SetProfileBlock(ctx, "bob", aliceID, false))
	requireOK(t, store.DeleteConversation(ctx, "alice", bobID))
	likes, err = store.IncomingLikes(ctx, "alice")
	requireOK(t, err)
	if len(likes) != 0 {
		t.Fatal("удаление чата не сбросило входящий лайк")
	}
	candidate, err = store.NextSearchProfile(ctx, "alice", "male", "")
	requireOK(t, err)
	if candidate.ID != bobID {
		t.Fatal("удаление чата не вернуло анкету в пул")
	}
	requireOK(t, store.RecordSearchReaction(ctx, "bob", aliceID, true))
	matched, err = store.ReplyToLike(ctx, "alice", bobID)
	requireOK(t, err)
	if !matched {
		t.Fatal("повторный матч недоступен")
	}
	requireOK(t, store.SendChatMessage(ctx, "alice", bobID, nonce, "", attachment))
	chat, err = store.OpenConversation(ctx, "alice", bobID, 0)
	requireOK(t, err)
	if len(chat.Messages) != 0 {
		t.Fatal("старый запрос восстановил сообщение в новом чате")
	}
}

// openTestStore создаёт отдельную схему, запускает миграции и возвращает подключение для фикстур.
func openTestStore(t *testing.T, ctx context.Context) (*postgres.Store, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("SVAHA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SVAHA_TEST_DATABASE_URL не задана")
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := pq.ParseURL(dsn)
		requireOK(t, err)
		dsn = parsed
	}
	admin, err := sql.Open("postgres", dsn)
	requireOK(t, err)
	// Закрывает служебное подключение после удаления тестовой схемы.
	t.Cleanup(func() { admin.Close() })
	schema := "svaha_test_" + strings.ToLower(rand.Text())
	_, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema))
	requireOK(t, err)
	// Удаляет только временную схему, созданную этим тестом.
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(cleanup, "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE"); err != nil {
			t.Errorf("очистка тестовой схемы: %v", err)
		}
	})
	scopedDSN := dsn + " search_path=" + schema
	store, err := postgres.Open(ctx, scopedDSN)
	requireOK(t, err)
	// Закрывает пул приложения перед очисткой тестовой схемы.
	t.Cleanup(func() { store.Close() })
	db, err := sql.Open("postgres", scopedDSN)
	requireOK(t, err)
	// Закрывает подключение для фикстур перед очисткой.
	t.Cleanup(func() { db.Close() })
	// Воспроизводит прежний журнал миграций и проверяет его обновление при запуске.
	_, err = db.ExecContext(ctx, "ALTER TABLE schema_migrations DROP COLUMN checksum")
	requireOK(t, err)
	second, err := postgres.Open(ctx, scopedDSN)
	requireOK(t, err)
	requireOK(t, second.Close())

	var total, checksums int
	requireOK(t, db.QueryRowContext(ctx, "SELECT count(*), count(checksum) FROM schema_migrations").Scan(&total, &checksums))
	if total == 0 || total != checksums {
		t.Fatal("контрольные суммы старых миграций не восстановились")
	}

	// Подменяет контрольную сумму только в тестовой схеме и проверяет отказ запуска.
	var original string
	requireOK(t, db.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE version = '001_accounts.sql'").Scan(&original))
	_, err = db.ExecContext(ctx, "UPDATE schema_migrations SET checksum = 'changed' WHERE version = '001_accounts.sql'")
	requireOK(t, err)
	rejected, err := postgres.Open(ctx, scopedDSN)
	if err == nil {
		rejected.Close()
		t.Fatal("приложение приняло изменённую миграцию")
	}
	if !strings.Contains(err.Error(), "001_accounts.sql") {
		t.Fatalf("ожидалась ошибка контрольной суммы, получено: %v", err)
	}
	_, err = db.ExecContext(ctx, "UPDATE schema_migrations SET checksum = $1 WHERE version = '001_accounts.sql'", original)
	requireOK(t, err)
	return store, db
}

// requireOK останавливает интеграционный тест при ошибке SQL или ввода-вывода.
func requireOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
