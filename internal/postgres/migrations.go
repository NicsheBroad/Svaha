package postgres

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// applyMigrations применяет новые SQL-файлы по порядку и сохраняет их версии.
func applyMigrations(ctx context.Context, db *sql.DB) error {
	files, err := fs.Glob(migrationFiles, "migrations/*.sql")
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
	if _, err := tx.ExecContext(ctx, `
  CREATE TABLE IF NOT EXISTS schema_migrations (
      version text PRIMARY KEY
  )`); err != nil {
		return err
	}

	for _, file := range files {
		version := path.Base(file)
		var applied bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", version).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		migration, err := migrationFiles.ReadFile(file)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(migration)); err != nil {
			return fmt.Errorf("миграция %s: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			return err
		}
	}
	return tx.Commit()
}
