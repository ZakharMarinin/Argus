// Package app assembles and runs the dashboard process: dependencies, the HTTP
// server and the shutdown order.
package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"argus-frontend/internal/config"
)

// shutdownTimeout is how long we wait for in-flight requests to finish.
const shutdownTimeout = 5 * time.Second

// App is the dashboard process.
type App struct {
	cfg    *config.Config
	log    *slog.Logger
	di     *Container
	server *http.Server
	errc   chan error
}

// NewApp prepares the application.
func NewApp(cfg *config.Config, log *slog.Logger) *App {
	return &App{
		cfg:  cfg,
		log:  log,
		di:   NewContainer(cfg, log),
		errc: make(chan error, 1),
	}
}

// Run starts the HTTP server and returns without waiting for it to stop.
// An error means the process has nothing to do: broken templates or routes.
func (a *App) Run(ctx context.Context) error {
	handler, err := a.di.Router()
	if err != nil {
		return err
	}

	a.server = &http.Server{
		Addr:              a.cfg.Web.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		a.log.Info("dashboard started", "addr", a.cfg.Web.ListenAddr, "version", Version())

		err := a.server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.errc <- err
		}
	}()

	// The first connectivity check runs in the background and is only logged:
	// an unavailable backend at startup isn't a startup failure, the waiting
	// page already explains it.
	go a.checkBackend(ctx)

	return nil
}

// Failed returns the channel that receives a server error: the process should
// die rather than silently hang with no listener.
func (a *App) Failed() <-chan error { return a.errc }

// Stop stops the server, letting in-flight requests finish.
func (a *App) Stop(ctx context.Context) {
	a.log.Info("shutting down")

	srvCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()

	err := a.server.Shutdown(srvCtx)
	if err != nil {
		a.log.Error("server did not shut down cleanly", "err", err)
	}
}

func (a *App) checkBackend(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()

	err := a.di.UseCase().Ping(ctx)
	if err != nil {
		a.log.Warn("argus-backend is not available yet, showing the waiting screen", "err", err)

		return
	}

	a.log.Info("argus-backend is responding")
}

// Version takes the version from the build info: no separate flag is needed for it.
func Version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "argus-web"
	}

	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && len(setting.Value) >= 7 {
			return "argus-web " + setting.Value[:7]
		}
	}

	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return "argus-web " + info.Main.Version
	}

	return "argus-web dev"
}
