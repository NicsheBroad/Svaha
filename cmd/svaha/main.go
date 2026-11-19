package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"svaha/internal/postgres"
	"svaha/internal/web"
)

// main читает адрес прослушивания и запускает веб-сервер.
func main() {
	listenAddress := flag.String("addr", "127.0.0.1:8080", "адрес веб-сервера")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *listenAddress); err != nil {
		log.Print(err)
		stop()
		os.Exit(1)
	}
}

// run открывает базу, запускает сайт и освобождает ресурсы при завершении.
func run(ctx context.Context, listenAddress string) (runErr error) {
	dsn := os.Getenv("SVAHA_DATABASE_URL")
	if dsn == "" {
		dsn = localDatabaseDSN
	}
	if dsn == localDatabaseDSN {
		dataDir, err := startLocalDatabase()
		if err != nil {
			return fmt.Errorf("не удалось запустить локальный PostgreSQL: %w", err)
		}
		if dataDir != "" {
			// Останавливает PostgreSQL, запущенный этим экземпляром Svaha.
			defer func() {
				if err := stopLocalDatabase(dataDir); err != nil {
					runErr = errors.Join(runErr, fmt.Errorf("не удалось остановить PostgreSQL: %w", err))
				}
			}()
		}
	}
	connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	store, err := postgres.Open(connectCtx, dsn)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("не удалось открыть базу Svaha: %w", err)
	}
	defer store.Close()

	server := &http.Server{
		Addr:              listenAddress,
		Handler:           web.NewHandler(store),
		ReadHeaderTimeout: 5 * time.Second,
	}
	serverDone := make(chan error, 1)
	// Запускает HTTP-сервер и передаёт результат в основной поток.
	go func() {
		log.Printf("Страница входа доступна на http://%s", listenAddress)
		serverDone <- server.ListenAndServe()
	}()

	select {
	case err := <-serverDone:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return errors.Join(err, server.Close())
		}
		return nil
	}
}
