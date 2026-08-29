// immich-go is a Go implementation of the Immich server API, compatible
// with the official immich-machine-learning service.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"immich-go/internal/api"
	"immich-go/internal/app"
	"immich-go/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg := config.Load()

	// Entities persist to DuckDB — the single supported store.
	a, err := app.New(cfg, nil, logger)
	if err != nil {
		logger.Error("failed to initialize", "err", err)
		os.Exit(1)
	}
	defer a.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	a.Jobs.Start(ctx)

	server := &http.Server{
		Addr:              joinHostPort(cfg.Host, cfg.Port),
		Handler:           api.New(a).Router(),
		ReadHeaderTimeout: 30 * time.Second,
	}

	go func() {
		logger.Info("immich-go listening", "addr", server.Addr, "media", cfg.MediaLocation)
		logger.Info("machine learning", "enabled", cfg.MachineLearning.Enabled, "urls", cfg.MachineLearning.URLs)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}

func joinHostPort(host string, port int) string {
	if host == "" {
		host = "0.0.0.0"
	}
	return host + ":" + itoa(port)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
