package main

import (
	"argus-collector/internal/app"
	"argus-collector/internal/config"
	"argus-collector/internal/pkg/logger"
	"argus-collector/internal/version"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	_ "argus-collector/docs"
)

//go:generate swag init -g cmd/argus/main.go -d ../../ -o ../../docs

// @title           Argus API
// @version         1.0
// @BasePath        /v1/api
// @description     API for monitoring containers and HTTP targets.
func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("argus-collector " + version.Version)

		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.MustParseConfig()
	log := logger.SetupLogger(cfg.Env)
	log.Info("argus-collector starting", "version", version.Version)

	app := app.NewApp(cfg, log)
	app.MustRun(ctx)

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	<-shutdown

	app.Stop(ctx)
}