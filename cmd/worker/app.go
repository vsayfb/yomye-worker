package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
	"github.com/vsayfb/gig-platform-categorization-service/internal/extractor"
	"github.com/vsayfb/gig-platform-categorization-service/internal/notification"
	"github.com/vsayfb/gig-platform-categorization-service/internal/processor"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/database"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/embeddings"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/metrics"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/telemetry"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/tracing"

	lg "github.com/vsayfb/gig-platform-categorization-service/pkg/logger"
)

const (
	MessageTypeListingCreated      = "ListingCreated"
	MessageTypeGigPublished        = "GigPublished"
	MessageTypeProfessionPublished = "ProfessionPublished"
)

type App struct {
	cfg *config.Config

	handlers              map[string]processor.Processor
	notificationPublisher *notification.SQSPublisher

	db *pgxpool.Pool

	mu                sync.RWMutex
	telemetryShutdown func(context.Context) error
	cancelTelemetry   context.CancelFunc
}

var (
	app     *App
	once    sync.Once
	initErr error
)

func initTelemetryWithRetry(
	ctx context.Context,
	cfg *config.Config,
	logHandler slog.Handler,
	app *App,
) {
	const (
		retryInterval = 30 * time.Second
		retryTimeout  = 5 * time.Minute
	)

	deadline := time.Now().Add(retryTimeout)

	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()

	for {
		shutdownTelemetry, err := telemetry.Init(
			ctx,
			cfg.App.ServiceName,
			cfg.App.Env,
			cfg.Server.OTelCollectorAddr,
		)

		if err == nil {
			select {
			case <-ctx.Done():
				shutdownTelemetry(ctx)
				return
			default:
			}

			app.mu.Lock()
			app.telemetryShutdown = shutdownTelemetry
			app.mu.Unlock()

			slog.SetDefault(
				slog.New(tracing.NewOTelHandler(logHandler)),
			)

			slog.Info("telemetry initialized")
			return
		}

		if time.Now().After(deadline) {
			slog.Error(
				"failed to initialize telemetry after retries",
				"error", err,
			)
			return
		}

		slog.Warn(
			"telemetry unavailable, retrying",
			"retry_in", retryInterval,
			"error", err,
		)

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

func getApp(ctx context.Context) (*App, error) {
	once.Do(func() {
		cfg, err := config.Load(ctx)

		if err != nil {
			initErr = fmt.Errorf("load config: %w", err)
			return
		}

		logHandler := lg.Init(cfg.App.Env)

		if err := metrics.Register(); err != nil {
			initErr = fmt.Errorf("register metrics: %w", err)
			return
		}

		poolCfg, err := pgxpool.ParseConfig(cfg.DB.DSN())

		if err != nil {
			initErr = fmt.Errorf("parse database url: %w", err)
			return
		}

		// Each in-flight message can hold a connection (queries plus the
		// RunInTx transaction), so the pool needs headroom for the worker's
		// configured concurrency, or concurrent processing just serializes
		// on pool acquisition instead of actually running in parallel.
		poolCfg.MaxConns = int32(cfg.Server.WorkerConcurrency) + 2

		if poolCfg.MaxConns < 2 {
			poolCfg.MaxConns = 2
		}

		db, err := database.NewPool(ctx, poolCfg)

		if err != nil {
			initErr = fmt.Errorf("initialize database: %w", err)
			return
		}

		embeddingClient := embeddings.NewLocalClient(cfg)

		var extractors []extractor.Extractor

		if cfg.AI.Gemini.APIKey != "" &&
			cfg.AI.Gemini.Model != "" {
			extractors = append(
				extractors,
				extractor.NewGeminiExtractor(&cfg.AI.Gemini),
			)
		}

		for _, provider := range []struct {
			name string
			cfg  *config.AIConfig
		}{
			{name: "groq", cfg: &cfg.AI.Groq},
			{name: "open-router", cfg: &cfg.AI.OpenRouter},
			{name: "mistral", cfg: &cfg.AI.Mistral},
			{name: "nvidia", cfg: &cfg.AI.Nvidia},
		} {
			if provider.cfg.APIKey != "" &&
				provider.cfg.BaseURL != "" &&
				provider.cfg.Model != "" {
				extractors = append(
					extractors,
					extractor.NewOpenAIExtractor(provider.cfg, provider.name),
				)
			}
		}

		if len(extractors) == 0 {
			initErr = fmt.Errorf("no AI extraction provider is configured")
			db.Close()
			return
		}

		fallback := extractor.NewFallbackExtractor(extractors...)

		publisher, err := notification.NewNotificationPublisher(
			ctx,
			cfg,
		)

		if err != nil {
			initErr = fmt.Errorf("create sqs publisher: %w", err)
			return
		}

		telemetryCtx, telemetryCancel := context.WithCancel(ctx)

		processorRepo := processor.NewRepository(db)

		gigProcessor := processor.NewGigProcessor(processorRepo, embeddingClient, fallback, publisher, cfg)

		professionProcessor := processor.NewProfessionProcessor(processorRepo, embeddingClient, fallback, cfg)

		app = &App{
			cfg: cfg,
			handlers: map[string]processor.Processor{
				MessageTypeListingCreated:      gigProcessor,
				MessageTypeGigPublished:        gigProcessor,
				MessageTypeProfessionPublished: professionProcessor,
			},
			notificationPublisher: publisher,
			db:                    db,
			telemetryShutdown: func(context.Context) error {
				return nil
			},
			cancelTelemetry: telemetryCancel,
		}

		go initTelemetryWithRetry(
			telemetryCtx,
			cfg,
			logHandler,
			app,
		)
	})

	return app, initErr
}

// Ready reports whether the app's dependencies are usable. It's meant to
// back a readiness probe so an orchestrator stops routing traffic to (or
// restarts) an instance whose database connection has gone bad, rather than
// relying solely on a liveness check that only proves the process is alive.
func (a *App) Ready(ctx context.Context) error {
	if a.db == nil {
		return fmt.Errorf("database pool not initialized")
	}

	if err := a.db.Ping(ctx); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}

	return nil
}

func (a *App) Close(ctx context.Context) error {
	slog.Info("closing application")

	if a.cancelTelemetry != nil {
		a.cancelTelemetry()
	}

	a.mu.RLock()
	shutdownTelemetry := a.telemetryShutdown
	a.mu.RUnlock()

	if shutdownTelemetry != nil {
		if err := shutdownTelemetry(ctx); err != nil {
			slog.Error(
				"shutdown telemetry failed",
				"err",
				err,
			)
		}
	}

	if a.notificationPublisher != nil {
		if err := a.notificationPublisher.Close(); err != nil {
			slog.Error(
				"close sqs publisher failed",
				"err",
				err,
			)
		}
	}

	if a.db != nil {
		a.db.Close()
	}

	slog.Info("application closed")

	return nil
}
