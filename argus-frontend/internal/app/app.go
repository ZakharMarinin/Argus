// Package app собирает и держит процесс дашборда: зависимости, HTTP-сервер
// и порядок остановки.
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

// shutdownTimeout - сколько ждём завершения текущих запросов.
const shutdownTimeout = 5 * time.Second

// App - процесс дашборда.
type App struct {
	cfg    *config.Config
	log    *slog.Logger
	di     *Container
	server *http.Server
	errc   chan error
}

// NewApp готовит приложение.
func NewApp(cfg *config.Config, log *slog.Logger) *App {
	return &App{
		cfg:  cfg,
		log:  log,
		di:   NewContainer(cfg, log),
		errc: make(chan error, 1),
	}
}

// Run поднимает HTTP-сервер и возвращается, не дожидаясь его остановки.
// Ошибка означает, что процессу нечего делать: кривые шаблоны или роуты.
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
		a.log.Info("дашборд запущен", "addr", a.cfg.Web.ListenAddr, "version", Version())

		err := a.server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.errc <- err
		}
	}()

	// Первую проверку связи делаем в фоне и только логируем: недоступный
	// бэкенд на старте - не ошибка запуска, страница ожидания уже это
	// объясняет.
	go a.checkBackend(ctx)

	return nil
}

// Failed отдаёт канал, в который падает ошибка сервера: процессу надо
// умереть, а не молча висеть без слушателя.
func (a *App) Failed() <-chan error { return a.errc }

// Stop останавливает сервер, давая текущим запросам дописаться.
func (a *App) Stop(ctx context.Context) {
	a.log.Info("останавливаюсь")

	srvCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()

	err := a.server.Shutdown(srvCtx)
	if err != nil {
		a.log.Error("сервер не остановился штатно", "err", err)
	}
}

func (a *App) checkBackend(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()

	err := a.di.UseCase().Ping(ctx)
	if err != nil {
		a.log.Warn("argus-backend пока недоступен, показываю экран ожидания", "err", err)

		return
	}

	a.log.Info("argus-backend отвечает")
}

// Version берёт версию из данных сборки: отдельного флага под неё не нужно.
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
