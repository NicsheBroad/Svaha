package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	version  string
	sql      string
	checksum string
}

// applyMigrations проверяет историю и применяет новые миграции в одной транзакции.
func applyMigrations(ctx context.Context, db *sql.DB) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Блокировка Svaha действует до конца транзакции и защищает от параллельных запусков.
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(1398161736, 1)"); err != nil {
		return err
	}
	applied, err := readMigrationHistory(ctx, tx)
	if err != nil {
		return err
	}
	if err := validateMigrationHistory(migrations, applied); err != nil {
		return err
	}

	for _, entry := range migrations {
		previous, exists := applied[entry.version]
		switch {
		case !exists:
			if _, err := tx.ExecContext(ctx, entry.sql); err != nil {
				return fmt.Errorf("миграция %s: %w", entry.version, err)
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)", entry.version, entry.checksum); err != nil {
				return err
			}
		case previous == "":
			// У старых записей суммы нет. Запоминаем содержимое из текущей сборки.
			if _, err := tx.ExecContext(ctx, "UPDATE schema_migrations SET checksum = $2 WHERE version = $1", entry.version, entry.checksum); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// loadMigrations читает встроенные SQL-файлы по порядку имён и вычисляет их SHA-256.
func loadMigrations() ([]migration, error) {
	files, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	migrations := make([]migration, 0, len(files))
	for _, file := range files {
		sql, err := migrationFiles.ReadFile(file)
		if err != nil {
			return nil, err
		}
		hash := sha256.Sum256(sql)
		migrations = append(migrations, migration{version: path.Base(file), sql: string(sql), checksum: hex.EncodeToString(hash[:])})
	}
	return migrations, nil
}

// readMigrationHistory создаёт журнал или добавляет контрольную сумму к старому формату.
func readMigrationHistory(ctx context.Context, tx *sql.Tx) (map[string]string, error) {
	if _, err := tx.ExecContext(ctx, `
  CREATE TABLE IF NOT EXISTS schema_migrations (
      version text PRIMARY KEY,
      checksum text
  )`); err != nil {
		return nil, err
	}

	var hasChecksum bool
	if err := tx.QueryRowContext(ctx, `
  SELECT EXISTS (
      SELECT 1 FROM pg_attribute
      WHERE attrelid = 'schema_migrations'::regclass AND attname = 'checksum' AND NOT attisdropped
  )`).Scan(&hasChecksum); err != nil {
		return nil, err
	}
	if !hasChecksum {
		if _, err := tx.ExecContext(ctx, "ALTER TABLE schema_migrations ADD COLUMN checksum text"); err != nil {
			return nil, err
		}
	}

	rows, err := tx.QueryContext(ctx, "SELECT version, COALESCE(checksum, '') FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	applied := make(map[string]string)
	for rows.Next() {
		var version, checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			return nil, err
		}
		applied[version] = checksum
	}
	return applied, rows.Err()
}

// validateMigrationHistory отклоняет изменённые и отсутствующие в сборке применённые миграции.
func validateMigrationHistory(migrations []migration, applied map[string]string) error {
	found := 0
	for _, entry := range migrations {
		checksum, exists := applied[entry.version]
		if !exists {
			continue
		}
		found++
		if checksum != "" && checksum != entry.checksum {
			return fmt.Errorf("применённая миграция %s была изменена", entry.version)
		}
	}
	if found != len(applied) {
		return fmt.Errorf("в базе есть применённые миграции, отсутствующие в этой сборке")
	}
	return nil
}
