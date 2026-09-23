package app

import (
	"fmt"
	"log/slog"
	"net/http"

	"argus-frontend/internal/config"
	"argus-frontend/internal/gateway/backend"
	"argus-frontend/internal/transport/http/handlers"
	"argus-frontend/internal/transport/http/router"
	"argus-frontend/internal/transport/http/view"
	"argus-frontend/internal/usecase"
)

// Container - ленивая сборка зависимостей. Каждый узел создаётся один раз
// и только когда его попросили.
type Container struct {
	cfg *config.Config
	log *slog.Logger

	client   *backend.Client
	uc       *usecase.UseCase
	tpl      *view.Templates
	handlers *handlers.Handler
	router   http.Handler
}

// NewContainer готовит контейнер, ничего пока не создавая.
func NewContainer(cfg *config.Config, log *slog.Logger) *Container {
	return &Container{cfg: cfg, log: log}
}

// Router собирает всё дерево зависимостей и отдаёт обработчик.
func (c *Container) Router() (http.Handler, error) {
	if c.router == nil {
		h, err := c.Handler()
		if err != nil {
			return nil, err
		}

		r, err := router.Router(h)
		if err != nil {
			return nil, fmt.Errorf("сборка роутов: %w", err)
		}

		c.router = r
	}

	return c.router, nil
}

func (c *Container) Handler() (*handlers.Handler, error) {
	if c.handlers == nil {
		tpl, err := c.Templates()
		if err != nil {
			return nil, err
		}

		c.handlers = handlers.NewHandler(c.log, c.UseCase(), tpl, view.Presenter{
			Build:     Version(),
			Retention: c.cfg.Retention(),
		})
	}

	return c.handlers, nil
}

// Templates выбирает источник шаблонов: вшитый или дисковый.
func (c *Container) Templates() (*view.Templates, error) {
	if c.tpl == nil {
		var (
			tpl *view.Templates
			err error
		)

		if c.cfg.Web.TemplateDir == "" {
			tpl, err = view.ParseTemplates()
		} else {
			c.log.Info("режим разработки: шаблоны и статика читаются с диска",
				"dir", c.cfg.Web.TemplateDir)

			tpl, err = view.Live(c.cfg.Web.TemplateDir)
		}

		if err != nil {
			return nil, fmt.Errorf("шаблоны: %w", err)
		}

		c.tpl = tpl
	}

	return c.tpl, nil
}

func (c *Container) UseCase() *usecase.UseCase {
	if c.uc == nil {
		c.uc = usecase.NewUseCase(c.Client())
	}

	return c.uc
}

// Client создаёт клиент бэкенда. Связь при создании не проверяется: если
// бэкенд сейчас недоступен, дашборд всё равно поднимется и покажет экран
// ожидания вместо отказа старта.
func (c *Container) Client() *backend.Client {
	if c.client == nil {
		c.client = backend.NewClient(c.cfg.Backend.BaseURL, c.cfg.Timeout())
	}

	return c.client
}
