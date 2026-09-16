package app

import (
	"argus-collector/internal/config"
	"context"
	"fmt"
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
	cancel context.CancelFunc
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
	workerCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	err := a.initDeps(ctx)
	if err != nil {
		return err
	}

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

	proberSvc, err := a.di.Prober(ctx)
	if err != nil {
		return fmt.Errorf("init prober: %w", err)
	}

	a.wg.Go(func() {
		a.log.Info("Run: prober started")

		proberSvc.RunProber(workerCtx)
	})

	watcherSvc, err := a.di.Watcher(ctx)
	if err != nil {
		a.log.Error("watcher not started, Docker client unavailable", "error", err)
	} else {
		a.wg.Go(func() {
			a.log.Info("Run: watcher started")

			watcherSvc.RunWatcher(workerCtx)
		})
	}

	a.startAlerter(ctx, workerCtx)

	return nil
}

func (a *App) startAlerter(ctx, workerCtx context.Context) {
	if a.cfg.Telegram.BotToken == "" || a.cfg.Telegram.ChatID == 0 {
		a.log.Info("alerter not started, telegram is not configured")

		return
	}

	alerter, err := a.di.Alerter(ctx)
	if err != nil {
		a.log.Error("alerter not started", "error", err)

		return
	}

	a.wg.Go(func() {
		a.log.Info("Run: bot started")

		alerter.StartBot()
	})

	a.wg.Go(func() {
		a.log.Info("Run: alerter started")

		alerter.StartAlerter(workerCtx)
	})
}

func (a *App) initDeps(ctx context.Context) error {
	_, err := a.di.Handler(ctx)
	if err != nil {
		return fmt.Errorf("init dependencies: %w", err)
	}

	return nil
}

func (a *App) Stop(ctx context.Context) {
	a.log.Info("shutting down..")

	srvCtx, srvCancel := context.WithTimeout(ctx, time.Second*5)
	defer srvCancel()

	err := a.server.Shutdown(srvCtx)
	if err != nil {
		a.log.Error("cannot close server", "error", err)
	}

	a.cancel()

	if a.di.alerter != nil {
		a.di.alerter.StopBot()
	}

	a.wg.Wait()

	a.di.db.Close()

	a.log.Info("Shutdown completed gracefully")
}
