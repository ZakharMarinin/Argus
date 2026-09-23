package config

import (
	"errors"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
	"github.com/joho/godotenv"
)

type Config struct {
	Env       string    `yaml:"env"`
	Server    Server    `yaml:"http_server"`
	Postgres  string    `yaml:"postgres"`
	Prober    Prober    `yaml:"prober"`
	Telegram  Telegram  `yaml:"telegram"`
	Matcher   Matcher   `yaml:"matcher"`
	Retention Retention `yaml:"retention"`
	Watcher   Watcher   `yaml:"watcher"`
	Host      Host      `yaml:"host"`
}

type Host struct {
	Interval    time.Duration `yaml:"interval"    env-default:"15s"`
	DiskPath    string        `yaml:"disk_path"   env-default:"/"   env:"HOST_DISK_PATH"`
	Warning     float64       `yaml:"warning"     env-default:"90"`
	Critical    float64       `yaml:"critical"    env-default:"95"`
	Consecutive int           `yaml:"consecutive" env-default:"3"`
}

type Watcher struct {
	CrashLoopRestarts int           `yaml:"crash_loop_restarts" env-default:"5"`
	CrashLoopWindow   time.Duration `yaml:"crash_loop_window"   env-default:"5m"`
}

type Retention struct {
	Keep     time.Duration `yaml:"keep"     env-default:"336h"`
	Interval time.Duration `yaml:"interval" env-default:"24h"`
}

type Matcher struct {
	Rules []Rule `yaml:"rules"`
}

type Rule struct {
	Pattern   string `yaml:"pattern"`
	Severity  string `yaml:"severity"`
	Container string `yaml:"container"`
}

type Server struct {
	Addr         string        `yaml:"addr"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	IdleTimeout  time.Duration `yaml:"idle_timeout"`
	Retry        int           `yaml:"retry"`
}

type Prober struct {
	ProbeInterval    time.Duration `yaml:"probe_interval"`
	ProbeTimeout     time.Duration `yaml:"probe_timeout"`
	FailureThreshold int           `yaml:"failure_threshold" env-default:"2"`
}

type Telegram struct {
	NotifyInterval time.Duration `yaml:"notify_interval"`
	Cooldown       time.Duration `yaml:"cooldown" env-default:"5m"`
	BotToken       string        `yaml:"-"`
	ChatID         int64         `yaml:"-"`
}

func MustParseConfig() *Config {
	err := godotenv.Load()
	if err != nil {
		log.Printf("no .env file found, relying on environment variables: %v", err)
	}

	cfgPath := os.Getenv("CONFIG_PATH")

	cfg := Config{}

	err = cleanenv.ReadConfig(cfgPath, &cfg)
	if err != nil {
		log.Fatalf("cannot read config: %v", err)
	}

	cfg.Postgres = os.Getenv("POSTGRES_URL")
	cfg.Telegram.BotToken = os.Getenv("BOT_TOKEN")

	if chatID := os.Getenv("CHAT_ID"); chatID != "" {
		cfg.Telegram.ChatID, err = strconv.ParseInt(chatID, 10, 64)
		if err != nil {
			log.Fatalf("cannot parse CHAT_ID: %s", err)
		}
	}

	err = cfg.ValidateConfig()
	if err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	return &cfg
}

func (c *Config) ValidateConfig() error {
	if c.Postgres == "" {
		return errors.New("no postgress addr")
	}

	if c.Server.Addr == "" {
		return errors.New("no http addr")
	}

	if c.Prober.FailureThreshold < 1 {
		return errors.New("prober.failure_threshold must be >= 1")
	}

	if c.Retention.Keep <= 0 || c.Retention.Interval <= 0 {
		return errors.New("retention.keep and retention.interval must be positive")
	}

	if c.Watcher.CrashLoopRestarts < 2 || c.Watcher.CrashLoopWindow <= 0 {
		return errors.New("watcher.crash_loop_restarts must be >= 2 and watcher.crash_loop_window positive")
	}

	h := c.Host
	if h.Interval <= 0 || h.Consecutive < 1 || h.Warning <= 0 || h.Warning >= h.Critical || h.Critical > 100 {
		return errors.New("host: interval must be positive, consecutive >= 1, and 0 < warning < critical <= 100")
	}

	return nil
}
