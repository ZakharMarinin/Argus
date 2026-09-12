package main

import (
	"argus-collector/internal/app"
	"argus-collector/internal/config"
	"argus-collector/internal/pkg/logger"
	"context"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.MustParseConfig()
	log := logger.SetupLogger(cfg.Env)

	app := app.NewApp(cfg, log)
	app.MustRun(ctx)

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	<-shutdown

	app.Stop(ctx)
}
