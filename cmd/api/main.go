package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/hotkhwan/llm-api/internal/buildinfo"
	"github.com/hotkhwan/llm-api/internal/config"
	"github.com/hotkhwan/llm-api/internal/lifecycle"
	"github.com/hotkhwan/llm-api/internal/mission"
	"github.com/hotkhwan/llm-api/internal/server"
)

var notifyContext = signal.NotifyContext

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

	readiness := server.NewReadiness()
	var localCaptioner mission.CaptionGenerator
	if cfg.LocalLLMURL != "" {
		localCaptioner = mission.OpenAICompatibleCaptioner{Endpoint: cfg.LocalLLMURL, Model: cfg.LocalLLMModel, Client: &http.Client{Timeout: 12 * time.Second}}
	}
	missionService := mission.NewService(
		mission.NewMemoryRepository(),
		mission.MemoryObjectStore{},
		mission.FallbackCaptioner{Primary: localCaptioner},
		&mission.MemoryLedger{},
		time.Now,
		func() string { return uuid.NewString() },
	)
	app := server.New(logger, buildinfo.Current(), readiness, server.Options{
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
		BodyLimit:    cfg.BodyLimit,
		Concurrency:  cfg.Concurrency,
		Mission:      missionService,
	})
	ctx, stop := serviceContext()
	defer stop()
	logger.Info("service starting", "address", cfg.HTTPAddr, "environment", cfg.Environment)
	if err := lifecycle.Run(ctx, lifecycle.Config{
		Address:         cfg.HTTPAddr,
		ShutdownTimeout: cfg.ShutdownTimeout,
	}, lifecycle.Dependencies{
		Listen:    net.Listen,
		Service:   app,
		Readiness: readiness,
	}); err != nil {
		return err
	}
	logger.Info("service stopped cleanly")
	return nil
}

func serviceContext() (context.Context, context.CancelFunc) {
	return notifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}
