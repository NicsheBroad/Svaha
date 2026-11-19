package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lib/pq"
)

const MaxProfilePhotos = 10

var ErrPhotoLimit = errors.New("в анкете можно хранить максимум 10 фото")

// OwnProfile загружает личную анкету и галерею для просмотра или редактирования.
func (s *Store) OwnProfile(ctx context.Context, username string) (User, error) {
	var user User
	err := s.db.QueryRowContext(ctx, `
  SELECT u.username, u.first_name, u.email, u.phone, to_char(u.birth_date, 'YYYY-MM-DD'),
         u.city, u.gender, u.looking_for, u.bio, u.hobbies,
         ARRAY(SELECT p.id FROM profile_photos AS p WHERE p.username = u.username ORDER BY p.id)
  FROM users AS u
  WHERE u.username = $1`, username).
		Scan(&user.Username, &user.FirstName, &user.Email, &user.Phone, &user.BirthDate,
			&user.City, &user.Gender, &user.LookingFor, &user.Bio, &user.Hobbies, pq.Array(&user.PhotoIDs))
	return user, err
}

// UpdateProfile сохраняет анкету и изменения галереи в одной транзакции.
func (s *Store) UpdateProfile(ctx context.Context, user User, photos [][]byte, removeIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Обновление блокирует строку пользователя до конца транзакции, чтобы
	// одновременные сохранения одной анкеты не превысили лимит.
	_, err = tx.ExecContext(ctx, `
  UPDATE users SET first_name = $2, email = $3, phone = $4, birth_date = $5,
                   city = $6, gender = $7, looking_for = $8, bio = $9, hobbies = $10
  WHERE username = $1`,
		user.Username, user.FirstName, user.Email, user.Phone, user.BirthDate,
		user.City, user.Gender, user.LookingFor, user.Bio, user.Hobbies)
	if err != nil {
		return accountWriteError(err)
	}

	if len(removeIDs) > 0 {
		if _, err := tx.ExecContext(ctx, "DELETE FROM profile_photos WHERE username = $1 AND id = ANY($2)", user.Username, pq.Array(removeIDs)); err != nil {
			return err
		}
	}
	if len(photos) > 0 {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM profile_photos WHERE username = $1", user.Username).Scan(&count); err != nil {
			return err
		}
		if count+len(photos) > MaxProfilePhotos {
			return ErrPhotoLimit
		}
		for _, photo := range photos {
			if _, err := tx.ExecContext(ctx, "INSERT INTO profile_photos (username, content) VALUES ($1, $2)", user.Username, photo); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// ProfilePhoto загружает фото с указанным ID из галереи пользователя.
func (s *Store) ProfilePhoto(ctx context.Context, username string, id int64) ([]byte, error) {
	var photo []byte
	err := s.db.QueryRowContext(ctx, "SELECT content FROM profile_photos WHERE username = $1 AND id = $2", username, id).Scan(&photo)
	return photo, err
}
