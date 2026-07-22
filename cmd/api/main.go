package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/hotkhwan/affiliate-api/internal/buildinfo"
	"github.com/hotkhwan/affiliate-api/internal/config"
	"github.com/hotkhwan/affiliate-api/internal/server"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("service stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	readiness := server.NewReadiness()
	app := server.New(logger, buildinfo.Current(), readiness)
	errCh := make(chan error, 1)
	readiness.Set(true)

	go func() {
		errCh <- app.Listener(listener)
	}()

	logger.Info("service started", "address", listener.Addr().String(), "environment", cfg.Environment)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		if err == nil {
			return nil
		}
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
		readiness.Set(false)
		logger.Info("shutdown requested")
		if err := app.ShutdownWithTimeout(cfg.ShutdownTimeout); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, net.ErrClosed) {
			return fmt.Errorf("serve after shutdown: %w", err)
		}
		logger.Info("service stopped cleanly")
		return nil
	}
}
