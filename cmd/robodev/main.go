// Package main is the entrypoint for the RoboDev controller binary.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/robodev-inc/robodev/internal/config"
	"github.com/robodev-inc/robodev/internal/controller"

	// Register metrics with the default Prometheus registry.
	_ "github.com/robodev-inc/robodev/internal/metrics"
)

func main() {
	var (
		configPath   = flag.String("config", "/etc/robodev/config.yaml", "path to the RoboDev configuration file")
		metricsAddr  = flag.String("metrics-addr", ":8080", "address for the Prometheus metrics and health endpoints")
		pollInterval = flag.Duration("poll-interval", 30*time.Second, "interval between ticketing backend polls")
		namespace    = flag.String("namespace", "robodev", "kubernetes namespace for job creation")
	)
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	logger.Info("starting robodev controller",
		"config", *configPath,
		"metrics_addr", *metricsAddr,
		"poll_interval", *pollInterval,
		"namespace", *namespace,
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

	// Readiness flag — set to true once the controller is fully initialised.
	var ready atomic.Bool

	// Set up HTTP server for metrics and health probes.
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if ready.Load() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("not ready"))
		}
	})

	srv := &http.Server{
		Addr:              *metricsAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Start the HTTP server in a goroutine.
	go func() {
		logger.Info("starting metrics and health server", "addr", *metricsAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("metrics server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Create the reconciler.
	reconciler := controller.NewReconciler(cfg, logger,
		controller.WithNamespace(*namespace),
	)

	// Mark as ready.
	ready.Store(true)
	logger.Info("controller initialised and ready")

	// Set up signal handling for graceful shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		logger.Info("received signal, shutting down", "signal", sig)
		cancel()
	}()

	// Run the reconciliation loop.
	if err := reconciler.Run(ctx, *pollInterval); err != nil && err != context.Canceled {
		logger.Error("reconciler exited with error", "error", err)
		os.Exit(1)
	}

	// Gracefully shut down the HTTP server.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http server shutdown error", "error", err)
	}

	logger.Info("robodev controller stopped")
}
