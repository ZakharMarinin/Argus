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
	Env      string   `yaml:"env"`
	Server   Server   `yaml:"http_server"`
	Postgres string   `yaml:"postgres"`
	Prober   Prober   `yaml:"prober"`
	Telegram Telegram `yaml:"telegram"`
	Matcher  Matcher  `yaml:"matcher"`
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
	ProbeInterval time.Duration `yaml:"probe_interval"`
	ProbeTimeout  time.Duration `yaml:"probe_timeout"`
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

	return nil
}
