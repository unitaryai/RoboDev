// Package main is the entrypoint for the RoboDev controller binary.
package main

import (
	"flag"
	"log/slog"
	"os"

	"github.com/robodev-inc/robodev/internal/config"

	// Register metrics with the default Prometheus registry.
	_ "github.com/robodev-inc/robodev/internal/metrics"
)

func main() {
	var (
		configPath  = flag.String("config", "/etc/robodev/config.yaml", "path to the RoboDev configuration file")
		metricsAddr = flag.String("metrics-addr", ":8080", "address for the Prometheus metrics endpoint")
	)
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	logger.Info("starting robodev controller",
		"config", *configPath,
		"metrics_addr", *metricsAddr,
	)

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	logger.Info("configuration loaded",
		"ticketing_backend", cfg.Ticketing.Backend,
		"default_engine", cfg.Engines.Default,
	)

	// TODO: initialise controller-runtime manager, register reconciler,
	// start metrics server, and begin reconciliation loop.
	logger.Info("controller initialisation complete — reconciliation loop not yet implemented")
}
