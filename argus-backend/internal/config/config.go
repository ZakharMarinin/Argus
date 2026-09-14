package config

import (
	"log"
	"os"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
	"github.com/joho/godotenv"
)

type Config struct {
	Env           string        `yaml:"env"`
	Server        Server        `yaml:"http_server"`
	Postgres      string        `yaml:"postgres"`
	Prober Prober `yaml:"prober"`
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

	return &cfg
}
