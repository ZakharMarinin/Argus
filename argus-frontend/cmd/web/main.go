// Команда web поднимает дашборд Argus.
//
// Процесс ходит только в HTTP API argus-backend и не касается Postgres и
// миграций. Его можно останавливать, пересобирать и перезапускать
// независимо от collector'а.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"argus-frontend/internal/app"
	"argus-frontend/internal/config"

	"github.com/joho/godotenv"
)

func main() {
	configPath := flag.String("config", "config.yaml", "путь к YAML-конфигу")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	err := run(*configPath, log)
	if err != nil {
		log.Error("не удалось запуститься", "err", err)
		os.Exit(1)
	}
}

func run(configPath string, log *slog.Logger) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := godotenv.Load()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf(".env: %w", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	a := app.NewApp(&cfg, log)

	err = a.Run(ctx)
	if err != nil {
		return err
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-a.Failed():
		return err
	case <-stop:
	}

	a.Stop(ctx)

	return nil
}
