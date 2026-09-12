package config

import (
	"log"
	"os"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
	"github.com/joho/godotenv"
)

type Config struct {
	Env    string `yaml:"env"`
	Server Server `yaml:"server"`
	Postgres string `yaml:"postgres"`
}

type Server struct {
	Addr         string        `yaml:"addr"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	IdleTimeout  time.Duration `yaml:"idle_timeout"`
	Retry        int           `yaml:"retry"`
}

func MustParseConfig() *Config {
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("cannot load .env file: %v", err)
	}

	cfgPath := os.Getenv("CONFIG_PATH")

	cfg := Config{}

	err = cleanenv.ReadConfig(cfgPath, cfg)
	if err != nil {
		log.Fatalf("cannot read config: %v", err)
	}

	cfg.Postgres = os.Getenv("POSTGRES_ADDR")

	return &cfg
}
