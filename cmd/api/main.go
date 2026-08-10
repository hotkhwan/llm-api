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
	authadapter "github.com/hotkhwan/llm-api/internal/auth"
	"github.com/hotkhwan/llm-api/internal/buildinfo"
	"github.com/hotkhwan/llm-api/internal/config"
	"github.com/hotkhwan/llm-api/internal/lifecycle"
	"github.com/hotkhwan/llm-api/internal/mission"
	"github.com/hotkhwan/llm-api/internal/server"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
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
	var identity mission.IdentityVerifier
	if cfg.OIDCIssuer != "" {
		identityCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		identity, err = authadapter.NewOIDCVerifier(identityCtx, cfg.OIDCIssuer, cfg.OIDCAudience)
		if err != nil {
			return fmt.Errorf("configure authentication: %w", err)
		}
	}
	id := func() string { return uuid.NewString() }
	var repo mission.Repository = mission.NewMemoryRepository()
	var objects mission.ObjectStore = mission.MemoryObjectStore{}
	var ledger mission.AuditSink = &mission.MemoryLedger{}
	var exportQueue mission.ExportQueue = mission.NewMemoryJobQueue(time.Now, id)
	var exportWorker *mission.FFmpegExportQueue
	var visualQueue mission.VisualQCQueue
	var visualWorker *mission.ShotVLQueue
	var cinematicAnalyzer mission.VisualQCAnalyzer
	var durableStore *mission.MongoStore
	var durableObjects *mission.SeaweedFSStore
	var videoWorker *mission.CloudVideoPipeline
	var mongoClient *mongo.Client
	if cfg.MongoURI != "" {
		mongoClient, err = mongo.Connect(context.Background(), options.Client().ApplyURI(cfg.MongoURI))
		if err != nil {
			return fmt.Errorf("connect MongoDB: %w", err)
		}
		defer func() { _ = mongoClient.Disconnect(context.Background()) }()
		probeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mongoClient.Ping(probeCtx, nil); err != nil {
			return fmt.Errorf("ping MongoDB: %w", err)
		}
		store := mission.NewMongoStore(mongoClient.Database(cfg.MongoDatabase), time.Now, id)
		durableStore = store
		if err := store.EnsureIndexes(probeCtx); err != nil {
			return fmt.Errorf("prepare MongoDB: %w", err)
		}
		seaweed, createErr := mission.NewSeaweedFSStore(probeCtx, cfg.S3Endpoint, cfg.S3PublicEndpoint, cfg.S3Region, cfg.S3Bucket, cfg.S3Prefix, cfg.S3AccessKey, cfg.S3SecretKey)
		if createErr != nil {
			return fmt.Errorf("configure SeaweedFS: %w", createErr)
		}
		repo, ledger, objects = store, store, seaweed
		durableObjects = seaweed
		exportWorker = &mission.FFmpegExportQueue{Jobs: store, Objects: seaweed, Reader: seaweed, Runner: mission.ExecCommandRunner{}, Now: time.Now, Timeout: 5 * time.Minute}
		exportQueue = exportWorker
		if cfg.ShotVLURL != "" {
			analyzer := mission.OpenAICompatibleVisualQC{Endpoint: cfg.ShotVLURL, Model: cfg.ShotVLModel, ModelRevision: cfg.ShotVLModelRevision, APIKey: cfg.ShotVLAPIKey, Threshold: cfg.ShotVLThreshold, Client: &http.Client{Timeout: 3 * time.Minute}}
			cinematicAnalyzer = analyzer
			visualWorker = &mission.ShotVLQueue{Jobs: store, Objects: seaweed, Reader: seaweed, Runner: mission.ExecCommandRunner{}, Analyzer: analyzer, Now: time.Now, WorkerID: "visual-" + id(), Timeout: 5 * time.Minute}
			visualQueue = visualWorker
		}
	}
	var localPlanner mission.ProductionPlanner
	if cfg.LocalLLMURL != "" {
		client := &http.Client{Timeout: cfg.LocalLLMTimeout}
		localPlanner = mission.OpenAICompatiblePlanner{Endpoint: cfg.LocalLLMURL, Model: cfg.LocalLLMModel, APIKey: cfg.LocalLLMAPIKey, Client: client}
	}
	planner := mission.FallbackPlanner{Primary: localPlanner, Fallback: mission.CaptionBackedPlanner{Captions: mission.FallbackCaptioner{}}}
	missionService := mission.NewServiceWithOptions(repo, objects, planner, exportQueue, visualQueue, ledger, time.Now, id, int64(cfg.BodyLimit))
	providers := map[string]mission.VideoProvider{}
	providerClient := &http.Client{Timeout: 45 * time.Minute}
	if cfg.VeoAPIKey != "" {
		providers["veo"] = mission.GeminiVeoProvider{BaseURL: cfg.VeoBaseURL, Model: cfg.VeoModel, APIKey: cfg.VeoAPIKey, Client: providerClient}
	}
	if cfg.SeedanceAPIKey != "" {
		providers["seedance"] = mission.ArkSeedanceProvider{BaseURL: cfg.SeedanceBaseURL, Model: cfg.SeedanceModel, APIKey: cfg.SeedanceAPIKey, Client: providerClient}
	}
	if cfg.WanURL != "" && cfg.WanAPIKey != "" {
		providers["wan"] = mission.WanLocalProvider{Endpoint: cfg.WanURL, APIKey: cfg.WanAPIKey, Client: &http.Client{Timeout: cfg.WanTimeout}}
	}
	if cfg.HunyuanURL != "" && cfg.HunyuanAPIKey != "" {
		providers["hunyuan"] = mission.LocalProductPreviewProvider{Provider: "hunyuan", Endpoint: cfg.HunyuanURL, APIKey: cfg.HunyuanAPIKey, Client: &http.Client{Timeout: cfg.HunyuanTimeout}}
	}
	if cfg.LTXURL != "" && cfg.LTXAPIKey != "" {
		providers["ltx"] = mission.LocalProductPreviewProvider{Provider: "ltx", Endpoint: cfg.LTXURL, APIKey: cfg.LTXAPIKey, Client: &http.Client{Timeout: cfg.LTXTimeout}}
	}
	if len(providers) > 0 && durableStore != nil && durableObjects != nil {
		fidelity := mission.OpenAIProductFidelity{Endpoint: cfg.FidelityVLMURL, Model: cfg.FidelityVLMModel, ModelRevision: cfg.FidelityVLMModelRevision, APIKey: cfg.FidelityVLMAPIKey, Threshold: cfg.FidelityVLMThreshold, Client: &http.Client{Timeout: 5 * time.Minute}}
		reviser := mission.OpenAIShotReviser{Endpoint: cfg.LocalLLMURL, Model: cfg.LocalLLMModel, APIKey: cfg.LocalLLMAPIKey, Client: &http.Client{Timeout: cfg.LocalLLMTimeout}}
		videoWorker = &mission.CloudVideoPipeline{Jobs: durableStore, Objects: durableObjects, Reader: durableObjects, Providers: providers, Fidelity: fidelity, Cinematic: cinematicAnalyzer, Reviser: reviser, Runner: mission.ExecCommandRunner{}, Now: time.Now, WorkerID: "video-" + id(), Timeout: 45 * time.Minute}
		missionService.SetVideoGenerationQueue(videoWorker)
	}
	app := server.New(logger, buildinfo.Current(), readiness, server.Options{
		ReadTimeout:                cfg.ReadTimeout,
		WriteTimeout:               cfg.WriteTimeout,
		IdleTimeout:                cfg.IdleTimeout,
		BodyLimit:                  cfg.BodyLimit,
		Concurrency:                cfg.Concurrency,
		BasePath:                   cfg.BasePath,
		MissionIdentity:            identity,
		AllowTrustedIdentityHeader: cfg.AllowTrustedIdentityHeader,
		Mission:                    missionService,
	})
	ctx, stop := serviceContext()
	defer stop()
	if exportWorker != nil {
		go exportWorker.Run(ctx)
	}
	if visualWorker != nil {
		go visualWorker.Run(ctx)
	}
	if videoWorker != nil {
		go videoWorker.Run(ctx)
	}
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
