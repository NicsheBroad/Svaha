package postgres

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"

	"github.com/lib/pq"
)

var ErrDuplicate = errors.New("логин или email уже занят")

type User struct {
	Username, Email, FirstName, Phone, BirthDate, City, Gender, LookingFor string
	Bio, Hobbies                                                           string
	PhotoIDs                                                               []int64
}

type Store struct {
	db *sql.DB
}

// Open подключается к PostgreSQL и подготавливает схему приложения.
func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	if err := applyMigrations(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close закрывает пул подключений к PostgreSQL.
func (s *Store) Close() error {
	return s.db.Close()
}

// Register создаёт аккаунт и сохраняет хеш пароля с индивидуальной солью.
func (s *Store) Register(ctx context.Context, user User, password string) error {
	salt := make([]byte, 16)
	rand.Read(salt)
	hash, err := passwordHash(password, salt)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
  INSERT INTO users (username, email, first_name, phone, birth_date, city,
                     gender, looking_for, password_salt, password_hash)
  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		user.Username, user.Email, user.FirstName, user.Phone, user.BirthDate,
		user.City, user.Gender, user.LookingFor, salt, hash)
	return accountWriteError(err)
}

// accountWriteError преобразует нарушение уникальности в ErrDuplicate.
func accountWriteError(err error) error {
	var dbError *pq.Error
	if errors.As(err, &dbError) && dbError.Code == "23505" {
		return ErrDuplicate
	}
	return err
}

// Authenticate проверяет, соответствует ли пароль указанному логину.
func (s *Store) Authenticate(ctx context.Context, username, password string) (bool, error) {
	var salt, expected []byte
	err := s.db.QueryRowContext(ctx,
		"SELECT password_salt, password_hash FROM users WHERE username = $1", username).
		Scan(&salt, &expected)
	if errors.Is(err, sql.ErrNoRows) {
		salt, expected = make([]byte, 16), make([]byte, 32)
	} else if err != nil {
		return false, err
	}
	actual, hashErr := passwordHash(password, salt)
	if hashErr != nil {
		return false, hashErr
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1 && err == nil, nil
}

// passwordHash вычисляет хеш пароля и соли с помощью PBKDF2.
func passwordHash(password string, salt []byte) ([]byte, error) {
	return pbkdf2.Key(sha256.New, password, salt, 600000, 32)
}
