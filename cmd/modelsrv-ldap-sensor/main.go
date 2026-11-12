package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"

	"emeland.io/modelsrv-ldap-sensor/internal/config"
	"emeland.io/modelsrv-ldap-sensor/internal/ldapclient"
	"emeland.io/modelsrv-ldap-sensor/internal/scan"
	"emeland.io/modelsrv-ldap-sensor/internal/sensor"
)

func main() {
	var (
		configPath   = flag.String("config", envOrDefault("SENSOR_CONFIG", "config/sensor.yaml"), "Path to YAML config")
		listenAddr   = flag.String("listen", envOrDefault("SENSOR_LISTEN_ADDR", "localhost:24300"), "HTTP listen address for this sensor's modelsrv API")
		pollInterval = flag.Duration("poll-interval", envDurationOrDefault("SENSOR_POLL_INTERVAL", 0), "Scan interval (overrides config when > 0)")
	)
	flag.Parse()

	log := zap.Must(zap.NewDevelopmentConfig().Build(zap.AddStacktrace(zap.ErrorLevel)))
	slog := log.Sugar()
	defer func() { _ = log.Sync() }()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Fatalw("failed to load config", "path", *configPath, "error", err)
	}
	if *pollInterval > 0 {
		cfg.PollInterval = *pollInterval
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, cfg, *listenAddr, slog); err != nil {
		slog.Fatalw("sensor exited with error", "error", err)
	}
}

func run(ctx context.Context, cfg config.Config, listenAddr string, log *zap.SugaredLogger) error {
	srv, err := sensor.New(listenAddr, cfg.Subscribers, log)
	if err != nil {
		return fmt.Errorf("start sensor: %w", err)
	}
	defer func() { _ = srv.Close() }()

	client, err := ldapclient.New(cfg.LDAP)
	if err != nil {
		return fmt.Errorf("build ldap client: %w", err)
	}
	scanner := scan.New(cfg, client, srv, log)

	log.Infow("ldap sensor running",
		"listen", listenAddr,
		"ldap", cfg.LDAP.URL,
		"pollInterval", cfg.PollInterval.String(),
		"subscribers", len(cfg.Subscribers),
	)

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	// Scan once at startup, then on every tick. A failed scan is logged and
	// retried on the next tick.
	for {
		if err := scanner.ScanOnce(ctx); err != nil {
			log.Errorw("scan failed", "error", err)
		}
		select {
		case <-ctx.Done():
			log.Infow("shutting down")
			return nil
		case <-ticker.C:
		}
	}
}

func envOrDefault(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return fallback
}

func envDurationOrDefault(key string, fallback time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil {
			return d
		}
	}
	return fallback
}
