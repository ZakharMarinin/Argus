// Package config читает YAML-конфиг Argus; переменные окружения
// перекрывают значения из файла.
//
// web больше не подключается к Postgres напрямую (ТЗ §8 в этой части устарел):
// дашборд - чистый HTTP-клиент argus-backend, поэтому конфиг разбирает только
// адрес бэкенда и настройки собственного сервера. Секции telegram, docker и
// probes сознательно игнорируются - дашборд про них ничего не знает.
package config

import (
	"fmt"
	"net/url"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

// Config - разобранный конфиг с точки зрения web.
type Config struct {
	Backend Backend `yaml:"backend"`
	Web     Web     `yaml:"web"`
}

// Backend - подключение к HTTP API argus-backend.
type Backend struct {
	// BaseURL - адрес бэкенда, например "http://localhost:8081".
	BaseURL string `env:"ARGUS_BACKEND_URL" yaml:"base_url"`
	// TimeoutSeconds - таймаут HTTP-запросов к бэкенду.
	TimeoutSeconds int `env-default:"5" yaml:"timeout_seconds"`
}

// Web - настройки HTTP-сервера дашборда.
type Web struct {
	ListenAddr string `env-default:":8080" yaml:"listen_addr"`
	// TemplateDir включает режим разработки: шаблоны и статика читаются
	// с диска вместо вшитых в бинарник. В проде остаётся пустым.
	TemplateDir string `yaml:"template_dir"`
	// RetentionDays - на какой срок бэкенд хранит историю (собственная
	// настройка ретеншна коллектора, продублированная здесь для отображения).
	// Показывается на странице алертов, чтобы пустой список читался как
	// «событий не было», а не «история потерялась».
	RetentionDays int `env-default:"14" yaml:"retention_days"`
}

func Load(path string) (Config, error) {
	var cfg Config

	err := cleanenv.ReadConfig(path, &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("конфиг %s: %w", path, err)
	}

	err = cfg.validate()
	if err != nil {
		return Config{}, fmt.Errorf("конфиг %s: %w", path, err)
	}

	return cfg, nil
}

func (c Config) validate() error {
	u, err := url.Parse(c.Backend.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("backend.base_url %q: нужен адрес вида http://host:port - задайте ARGUS_BACKEND_URL в окружении или .env",
			c.Backend.BaseURL)
	}

	if c.Web.RetentionDays < 1 {
		return fmt.Errorf("web.retention_days: %d - должно быть не меньше 1", c.Web.RetentionDays)
	}

	if c.Backend.TimeoutSeconds < 1 {
		return fmt.Errorf("backend.timeout_seconds: %d - должно быть не меньше 1", c.Backend.TimeoutSeconds)
	}

	return nil
}

// Retention возвращает срок хранения как длительность.
func (c Config) Retention() time.Duration {
	return time.Duration(c.Web.RetentionDays) * 24 * time.Hour
}

// Timeout возвращает таймаут HTTP-запросов к бэкенду как длительность.
func (c Config) Timeout() time.Duration {
	return time.Duration(c.Backend.TimeoutSeconds) * time.Second
}
