package heartbeat

import (
	"argus-collector/internal/config"
	"context"
	"log/slog"
	"net/http"
	"time"
)

const beatTimeout = 10 * time.Second

type Pinger interface {
	Ping(ctx context.Context) error
}

type Heartbeat struct {
	cfg    config.Heartbeat
	log    *slog.Logger
	db     Pinger
	client *http.Client
}

func NewHeartbeat(cfg config.Heartbeat, log *slog.Logger, db Pinger) *Heartbeat {
	return &Heartbeat{
		cfg:    cfg,
		log:    log,
		db:     db,
		client: &http.Client{Timeout: beatTimeout},
	}
}

func (h *Heartbeat) Beat(ctx context.Context) {
	const op = "Heartbeat.Beat"

	log := h.log.With("op", op)

	ctx, cancel := context.WithTimeout(ctx, beatTimeout)
	defer cancel()

	err := h.db.Ping(ctx)
	if err != nil {
		log.Error("database unreachable, heartbeat skipped", "error", err)

		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.cfg.URL, nil)
	if err != nil {
		log.Error("invalid heartbeat url", "error", err)

		return
	}

	resp, err := h.client.Do(req)
	if err != nil {
		log.Error("heartbeat failed", "error", err)

		return
	}

	_ = resp.Body.Close()

	if resp.StatusCode >= http.StatusMultipleChoices {
		log.Error("heartbeat rejected", "status", resp.StatusCode)
	}
}

func (h *Heartbeat) RunHeartbeat(ctx context.Context) {
	h.Beat(ctx)

	ticker := time.NewTicker(h.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			h.Beat(ctx)
		case <-ctx.Done():
			return
		}
	}
}
