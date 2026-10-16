package retention

import (
	"argus-collector/internal/config"
	"context"
	"log/slog"
	"time"
)

type UseCase interface {
	DeleteProbesOlderThan(ctx context.Context, before time.Time) error
	DeleteEventsOlderThan(ctx context.Context, before time.Time) error
	DeleteLogMatchesOlderThan(ctx context.Context, before time.Time) error
}

type Cleaner struct {
	cfg *config.Config
	log *slog.Logger
	uc  UseCase
}

func NewCleaner(cfg *config.Config, log *slog.Logger, uc UseCase) *Cleaner {
	return &Cleaner{cfg: cfg, log: log, uc: uc}
}

func (c *Cleaner) Clean(ctx context.Context) {
	const op = "Cleaner.Clean"

	log := c.log.With("op", op)

	before := time.Now().Add(-c.cfg.Retention.Keep)

	tables := []struct {
		name   string
		delete func(ctx context.Context, before time.Time) error
	}{
		{"probes", c.uc.DeleteProbesOlderThan},
		{"events", c.uc.DeleteEventsOlderThan},
		{"log_matches", c.uc.DeleteLogMatchesOlderThan},
	}

	for _, t := range tables {
		err := t.delete(ctx, before)
		if err != nil {
			log.Error("failed to delete old records", "table", t.name, "error", err)
		}
	}
}

func (c *Cleaner) RunCleaner(ctx context.Context) {
	c.Clean(ctx)

	ticker := time.NewTicker(c.cfg.Retention.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.Clean(ctx)
		case <-ctx.Done():
			return
		}
	}
}
