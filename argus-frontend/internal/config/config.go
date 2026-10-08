// Package config reads the Argus YAML config; environment variables
// override values from the file.
//
// web no longer connects to Postgres directly (spec §8 is outdated on this
// point): the dashboard is a pure HTTP client of argus-backend, so the config
// parses only the backend address and the settings of its own server. The
// telegram, docker and probes sections are ignored on purpose: the dashboard
// knows nothing about them.
package config

import (
	"fmt"
	"net/url"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

// Config is the parsed config from web's point of view.
type Config struct {
	Backend Backend `yaml:"backend"`
	Web     Web     `yaml:"web"`
}

// Backend is the connection to the argus-backend HTTP API.
type Backend struct {
	// BaseURL is the backend address, for example "http://localhost:8081".
	BaseURL string `env:"ARGUS_BACKEND_URL" yaml:"base_url"`
	// TimeoutSeconds is the timeout for HTTP requests to the backend.
	TimeoutSeconds int `env-default:"5" yaml:"timeout_seconds"`
}

// Web holds the settings of the dashboard's HTTP server.
type Web struct {
	ListenAddr string `env-default:":8080" yaml:"listen_addr"`
	// TemplateDir enables development mode: templates and static files are read
	// from disk instead of being embedded in the binary. Empty in production.
	TemplateDir string `yaml:"template_dir"`
	// RetentionDays is how long the backend keeps history (the collector's own
	// retention setting, duplicated here for display). It is shown on the
	// alerts page so that an empty list reads as "nothing happened", not as
	// "the history got lost".
	RetentionDays int `env-default:"14" yaml:"retention_days"`
}

func Load(path string) (Config, error) {
	var cfg Config

	err := cleanenv.ReadConfig(path, &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}

	err = cfg.validate()
	if err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}

	return cfg, nil
}

func (c Config) validate() error {
	u, err := url.Parse(c.Backend.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("backend.base_url %q: need an address like http://host:port - set ARGUS_BACKEND_URL in the environment or .env",
			c.Backend.BaseURL)
	}

	if c.Web.RetentionDays < 1 {
		return fmt.Errorf("web.retention_days: %d - must be at least 1", c.Web.RetentionDays)
	}

	if c.Backend.TimeoutSeconds < 1 {
		return fmt.Errorf("backend.timeout_seconds: %d - must be at least 1", c.Backend.TimeoutSeconds)
	}

	return nil
}

// Retention returns the retention period as a duration.
func (c Config) Retention() time.Duration {
	return time.Duration(c.Web.RetentionDays) * 24 * time.Hour
}

// Timeout returns the timeout for HTTP requests to the backend as a duration.
func (c Config) Timeout() time.Duration {
	return time.Duration(c.Backend.TimeoutSeconds) * time.Second
}
