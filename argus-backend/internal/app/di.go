package app

import (
	"argus-collector/internal/config"
	"argus-collector/internal/storage"
	"argus-collector/internal/transport/http/handlers"
	"argus-collector/internal/transport/http/router"
	"argus-collector/internal/usecase"
	"context"
	"log/slog"

	"github.com/go-chi/chi/v5"
)

type Container struct {
	cfg      *config.Config
	log      *slog.Logger
	router   *chi.Mux
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

func (c *Container) Handler(ctx context.Context) *handlers.Handler {
	if c.handlers == nil {
		c.handlers = handlers.NewHandler(c.log, c.UseCase(ctx))
		router.Router(c.log, c.Router(), c.handlers)
	}

	return c.handlers
}

func (c *Container) Router() *chi.Mux {
	if c.router == nil {
		c.router = chi.NewRouter()
	}

	return c.router
}

func (c *Container) UseCase(ctx context.Context) *usecase.UseCase {
	if c.uc == nil {
		c.uc = usecase.NewUseCase(c.DB(ctx))
	}

	return c.uc
}

func (c *Container) DB(ctx context.Context) *storage.Storage {
	if c.db == nil {
		db, err := storage.NewStorage(ctx, c.cfg)
		if err != nil {
			return nil
		}

		c.db = db
	}

	return c.db
}
