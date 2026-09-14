package app

import (
	"argus-collector/internal/config"
	"argus-collector/internal/prober"
	"argus-collector/internal/storage"
	"argus-collector/internal/transport/http/handlers"
	"argus-collector/internal/transport/http/router"
	"argus-collector/internal/usecase"
	"argus-collector/internal/watcher"
	"context"
	"fmt"
	"log/slog"

	"github.com/go-chi/chi/v5"
)

type Container struct {
	cfg      *config.Config
	log      *slog.Logger
	router   *chi.Mux
	prober   *prober.Prober
	watcher  *watcher.Watcher
	handlers *handlers.Handler
	uc       *usecase.UseCase
	db       *storage.Storage
}

func NewContainer(cfg *config.Config, log *slog.Logger) *Container {
	return &Container{
		cfg: cfg,
		log: log,
	}
}

func (c *Container) Prober(ctx context.Context) (*prober.Prober, error) {
	if c.prober == nil {
		uc, err := c.UseCase(ctx)
		if err != nil {
			return nil, err
		}

		c.prober = prober.NewProber(c.cfg, c.log, uc)
	}

	return c.prober, nil
}

func (c *Container) Watcher(ctx context.Context) (*watcher.Watcher, error) {
	if c.watcher == nil {
		uc, err := c.UseCase(ctx)
		if err != nil {
			return nil, err
		}

		w, err := watcher.NewWatcher(c.cfg, c.log, uc)
		if err != nil {
			return nil, fmt.Errorf("init watcher: %w", err)
		}

		c.watcher = w
	}

	return c.watcher, nil
}

func (c *Container) Handler(ctx context.Context) (*handlers.Handler, error) {
	if c.handlers == nil {
		uc, err := c.UseCase(ctx)
		if err != nil {
			return nil, err
		}

		c.handlers = handlers.NewHandler(c.log, uc)
		router.Router(c.log, c.Router(), c.handlers)
	}

	return c.handlers, nil
}

func (c *Container) Router() *chi.Mux {
	if c.router == nil {
		c.router = chi.NewRouter()
	}

	return c.router
}

func (c *Container) UseCase(ctx context.Context) (*usecase.UseCase, error) {
	if c.uc == nil {
		db, err := c.DB(ctx)
		if err != nil {
			return nil, err
		}

		c.uc = usecase.NewUseCase(db)
	}

	return c.uc, nil
}

func (c *Container) DB(ctx context.Context) (*storage.Storage, error) {
	if c.db == nil {
		db, err := storage.NewStorage(ctx, c.cfg)
		if err != nil {
			return nil, fmt.Errorf("init storage: %w", err)
		}

		c.db = db
	}

	return c.db, nil
}
