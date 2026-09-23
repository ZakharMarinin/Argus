package app

import (
	"argus-collector/internal/alerter"
	"argus-collector/internal/config"
	"argus-collector/internal/host"
	"argus-collector/internal/matcher"
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
	"github.com/moby/moby/client"
)

type Container struct {
	cfg          *config.Config
	log          *slog.Logger
	router       *chi.Mux
	prober       *prober.Prober
	watcher      *watcher.Watcher
	alerter      *alerter.Alerter
	matcher      *matcher.Matcher
	dockerClient *client.Client
	handlers     *handlers.Handler
	host         *host.Sampler
	hostName     string
	uc           *usecase.UseCase
	db           *storage.Storage
	starts       chan string
}

func NewContainer(cfg *config.Config, log *slog.Logger) *Container {
	return &Container{
		cfg:    cfg,
		log:    log,
		starts: make(chan string, 16),
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

func (c *Container) Matcher(ctx context.Context) (*matcher.Matcher, error) {
	if c.matcher == nil {
		uc, err := c.UseCase(ctx)
		if err != nil {
			return nil, err
		}

		client, err := c.DockerClient()
		if err != nil {
			return nil, err
		}

		m, err := matcher.NewMatcher(c.cfg, c.log, uc, client, c.starts)
		if err != nil {
			return nil, fmt.Errorf("init matcher: %w", err)
		}

		c.matcher = m
	}

	return c.matcher, nil
}

func (c *Container) Watcher(ctx context.Context) (*watcher.Watcher, error) {
	if c.watcher == nil {
		uc, err := c.UseCase(ctx)
		if err != nil {
			return nil, err
		}

		client, err := c.DockerClient()
		if err != nil {
			return nil, err
		}

		c.watcher = watcher.NewWatcher(c.cfg, c.log, uc, client, c.starts)
	}

	return c.watcher, nil
}

func (c *Container) Alerter(ctx context.Context) (*alerter.Alerter, error) {
	if c.alerter == nil {
		uc, err := c.UseCase(ctx)
		if err != nil {
			return nil, err
		}

		a, err := alerter.NewAlerter(c.cfg, c.log, uc, c.HostName(ctx))
		if err != nil {
			return nil, err
		}

		c.alerter = a
	}

	return c.alerter, nil
}

func (c *Container) DockerClient() (*client.Client, error) {
	if c.dockerClient == nil {
		client, err := client.New(client.FromEnv)
		if err != nil {
			return nil, err
		}

		c.dockerClient = client
	}

	return c.dockerClient, nil
}

func (c *Container) Handler(ctx context.Context) (*handlers.Handler, error) {
	if c.handlers == nil {
		uc, err := c.UseCase(ctx)
		if err != nil {
			return nil, err
		}

		sampler, err := c.Host(ctx)
		if err != nil {
			return nil, err
		}

		c.handlers = handlers.NewHandler(c.log, uc, sampler)
		router.Router(c.log, c.Router(), c.handlers)
	}

	return c.handlers, nil
}

func (c *Container) Host(ctx context.Context) (*host.Sampler, error) {
	if c.host == nil {
		uc, err := c.UseCase(ctx)
		if err != nil {
			return nil, err
		}

		c.host = host.NewSampler(c.cfg, c.log, uc, c.HostName(ctx))
	}

	return c.host, nil
}

func (c *Container) HostName(ctx context.Context) string {
	if c.hostName != "" {
		return c.hostName
	}

	var fromDocker string

	cli, err := c.DockerClient()
	if err == nil {
		info, err := cli.Info(ctx, client.InfoOptions{})
		if err == nil {
			fromDocker = info.Info.Name
		}
	}

	c.hostName = host.Hostname(c.cfg.Host.DiskPath, fromDocker)

	return c.hostName
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
