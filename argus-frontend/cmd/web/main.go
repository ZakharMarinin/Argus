// Command web starts the Argus dashboard.
//
// The process talks only to the argus-backend HTTP API and doesn't touch
// Postgres or migrations. It can be stopped, rebuilt and restarted
// independently of the collector.
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
	configPath := flag.String("config", "config.yaml", "path to the YAML config")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(app.Version())

		return
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	err := run(*configPath, log)
	if err != nil {
		log.Error("failed to start", "err", err)
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
