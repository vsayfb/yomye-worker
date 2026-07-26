package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

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

	telemetryShutdown func(context.Context) error
}

var (
	app     *App
	once    sync.Once
	initErr error
)

func getApp(ctx context.Context) (*App, error) {
	once.Do(func() {

		cfg, err := config.Load(ctx)

		if err != nil {
			initErr = fmt.Errorf("load config: %w", err)
			return
		}

		logHandler := lg.Init(cfg.App.Env)

		shutdownTelemetry, err := telemetry.Init(ctx, cfg.App.ServiceName, cfg.Server.OTelCollectorAddr)

		if err != nil {
			initErr = fmt.Errorf("initialize telemetry: %w", err)
			return
		}

		if err := metrics.Register(); err != nil {
			initErr = fmt.Errorf("register metrics: %w", err)
			return
		}

		slog.SetDefault(slog.New(tracing.NewOTelHandler(logHandler)))

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
			if provider.APIKey != "" && provider.BaseURL != "" && provider.Model != "" {
				extractors = append(extractors, extractor.NewOpenAIExtractor(provider))
			}
		}
		if cfg.AI.Gemini.APIKey != "" && cfg.AI.Gemini.Model != "" {
			extractors = append(extractors, extractor.NewGeminiExtractor(&cfg.AI.Gemini))
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

		app = &App{
			cfg:                   cfg,
			workerService:         worker.NewService(worker.NewRepository(db), embeddingClient, fallback, cfg),
			notificationPublisher: publisher,
			db:                    db,
			telemetryShutdown:     shutdownTelemetry,
		}
	})

	return app, initErr
}

func (a *App) Close(ctx context.Context) error {

	slog.Info("closing application")

	if a.telemetryShutdown != nil {
		if err := a.telemetryShutdown(ctx); err != nil {
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
