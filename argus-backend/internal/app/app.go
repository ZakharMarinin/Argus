package app

import (
	"argus-collector/internal/config"
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

type App struct {
	cfg    *config.Config
	log    *slog.Logger
	server *http.Server
	di     *Container
	wg     *sync.WaitGroup
}

func NewApp(cfg *config.Config, log *slog.Logger) *App {
	return &App{
		cfg: cfg,
		log: log,
		di:  NewContainer(cfg, log),
		wg:  &sync.WaitGroup{},
	}
}

func (a *App) MustRun(ctx context.Context) {
	err := a.run(ctx)
	if err != nil {
		panic(err)
	}
}

func (a *App) run(ctx context.Context) error {
	a.initDeps(ctx)

	a.server = &http.Server{
		Addr:         a.cfg.Server.Addr,
		Handler:      a.di.Router(),
		ReadTimeout:  a.cfg.Server.ReadTimeout,
		WriteTimeout: a.cfg.Server.WriteTimeout,
		IdleTimeout:  a.cfg.Server.IdleTimeout,
	}

	a.wg.Go(func() {
		a.log.Info("Run: server started")

		err := a.server.ListenAndServe()
		if err != nil {
			a.log.Error("ListenAndServer failed", "error", err)
		}
	})

	return nil
}

func (a *App) initDeps(ctx context.Context) {
	a.di.Handler(ctx)
}

func (a *App) Stop(ctx context.Context) {
	a.log.Info("shutting down..")

	srvCtx, srvCancel := context.WithTimeout(ctx, time.Second*5)
	defer srvCancel()

	err := a.server.Shutdown(srvCtx)
	if err != nil {
		a.log.Error("cannot close server", "error", err)
	}

	a.di.db.Close()

	a.wg.Wait()

	a.log.Info("Shutdown completed gracefully")
}
