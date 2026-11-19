package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const localDatabaseDSN = "host=/tmp port=55432 dbname=svaha sslmode=disable"
const pgCtl = "/usr/lib/postgresql/18/bin/pg_ctl"

// startLocalDatabase запускает PostgreSQL при отсутствии сокета и возвращает путь к запущенной базе.
func startLocalDatabase() (string, error) {
	const socket = "/tmp/.s.PGSQL.55432"
	if _, err := os.Stat(socket); !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(home, ".local", "share", "svaha")
	dataDir := filepath.Join(root, "postgres")
	if _, err := os.Stat(filepath.Join(dataDir, "PG_VERSION")); err != nil {
		return "", fmt.Errorf("не удалось найти подготовленную базу в %s: %w", dataDir, err)
	}

	logPath := filepath.Join(root, "server.log")
	command := exec.Command(pgCtl, "-D", dataDir, "-l", logPath, "-w", "-t", "15", "start")
	if output, err := command.CombinedOutput(); err != nil {
		if _, socketErr := os.Stat(socket); socketErr == nil {
			// Другой экземпляр Svaha успел запустить PostgreSQL.
			return "", nil
		}
		return "", fmt.Errorf("%w (%s), журнал PostgreSQL: %s", err, strings.TrimSpace(string(output)), logPath)
	}
	return dataDir, nil
}

// stopLocalDatabase останавливает экземпляр PostgreSQL с указанным каталогом данных.
func stopLocalDatabase(dataDir string) error {
	command := exec.Command(pgCtl, "-D", dataDir, "-w", "-t", "15", "-m", "fast", "stop")
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("%w (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}
