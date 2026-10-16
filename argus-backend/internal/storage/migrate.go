package storage

import (
	"argus-collector/internal/config"
	"argus-collector/migrations"
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/stdlib"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

func Migrate(ctx context.Context, cfg *config.Postgres, log *slog.Logger) error {
	const op = "storage.Migrate"

	poolConfig, err := getConfig(cfg)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	
	db := stdlib.OpenDB(*poolConfig.ConnConfig)
	defer func() { _ = db.Close() }()

	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	current, err := provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	latest := latestVersion(provider.ListSources())

	err = checkDowngrade(current, latest)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	results, err := provider.Up(ctx)
	for _, r := range results {
		log.Info("migration applied", "version", r.Source.Version, "duration", r.Duration.String())
	}

	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	log.Info("database schema is up to date", "version", latest)

	return nil
}

func checkDowngrade(current, latest int64) error {
	if current > latest {
		return fmt.Errorf("database schema version %d is newer than this build (%d): upgrade the image instead of downgrading", current, latest)
	}

	return nil
}

func latestVersion(sources []*goose.Source) int64 {
	var latest int64
	for _, s := range sources {
		latest = max(latest, s.Version)
	}

	return latest
}
