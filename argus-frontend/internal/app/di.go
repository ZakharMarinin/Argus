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

// Container is lazy dependency wiring. Each node is created once, and only
// when it is asked for.
type Container struct {
	cfg *config.Config
	log *slog.Logger

	client   *backend.Client
	uc       *usecase.UseCase
	tpl      *view.Templates
	handlers *handlers.Handler
	router   http.Handler
}

// NewContainer prepares the container without creating anything yet.
func NewContainer(cfg *config.Config, log *slog.Logger) *Container {
	return &Container{cfg: cfg, log: log}
}

// Router builds the whole dependency tree and returns the handler.
func (c *Container) Router() (http.Handler, error) {
	if c.router == nil {
		h, err := c.Handler()
		if err != nil {
			return nil, err
		}

		r, err := router.Router(h)
		if err != nil {
			return nil, fmt.Errorf("building routes: %w", err)
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

// Templates picks the template source: embedded or on disk.
func (c *Container) Templates() (*view.Templates, error) {
	if c.tpl == nil {
		var (
			tpl *view.Templates
			err error
		)

		if c.cfg.Web.TemplateDir == "" {
			tpl, err = view.ParseTemplates()
		} else {
			c.log.Info("development mode: templates and static files are read from disk",
				"dir", c.cfg.Web.TemplateDir)

			tpl, err = view.Live(c.cfg.Web.TemplateDir)
		}

		if err != nil {
			return nil, fmt.Errorf("templates: %w", err)
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

// Client creates the backend client. Connectivity isn't checked on creation:
// if the backend is unavailable right now, the dashboard still starts and
// shows a waiting screen instead of failing to start.
func (c *Container) Client() *backend.Client {
	if c.client == nil {
		c.client = backend.NewClient(c.cfg.Backend.BaseURL, c.cfg.Timeout())
	}

	return c.client
}
