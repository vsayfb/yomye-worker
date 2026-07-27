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
	"github.com/vsayfb/gig-platform-categorization-service/internal/worker"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/database"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/embeddings"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/metrics"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/telemetry"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/tracing"

	lg "github.com/vsayfb/gig-platform-categorization-service/pkg/logger"
)

type App struct {
	cfg *config.Config

	workerService         *worker.Service
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

		db, err := database.NewPool(ctx, poolCfg)

		if err != nil {
			initErr = fmt.Errorf("initialize database: %w", err)
			return
		}

		embeddingClient := embeddings.NewLocalClient(cfg)

		var extractors []extractor.Extractor

		for _, provider := range []*config.AIConfig{
			&cfg.AI.Nvidia,
			&cfg.AI.Qwen,
			&cfg.AI.Groq,
		} {
			if provider.APIKey != "" &&
				provider.BaseURL != "" &&
				provider.Model != "" {
				extractors = append(
					extractors,
					extractor.NewOpenAIExtractor(provider),
				)
			}
		}

		if cfg.AI.Gemini.APIKey != "" &&
			cfg.AI.Gemini.Model != "" {
			extractors = append(
				extractors,
				extractor.NewGeminiExtractor(&cfg.AI.Gemini),
			)
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

		app = &App{
			cfg:                   cfg,
			workerService:         worker.NewService(worker.NewRepository(db), embeddingClient, fallback, cfg),
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
