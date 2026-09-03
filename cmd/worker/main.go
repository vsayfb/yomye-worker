package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
	"github.com/vsayfb/gig-platform-categorization-service/internal/runtimepolicy"
	"github.com/vsayfb/gig-platform-categorization-service/internal/worker"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	defer stop()

	app, err := getApp(ctx)

	if err != nil {
		slog.Error("failed to initialize app", "err", err)
		os.Exit(1)
	}

	type runtimeWorker interface {
		Run(context.Context) error
	}
	var w runtimeWorker
	var closeWorker func() error
	if app.cfg.App.Env == config.EnvironmentProduction {
		pubSubWorker, createErr := worker.NewPubSub(ctx, app.cfg.GCP.ProjectID, app.cfg.GCP.CategorizationSubscription, app.handlers, app.cfg.Server.WorkerConcurrency)
		err = createErr
		if createErr == nil {
			w = pubSubWorker
			closeWorker = pubSubWorker.Close
		}
	} else {
		w, err = worker.New(ctx, app.cfg, app.cfg.AWS.CategorizationEventsQueue, app.handlers)
		closeWorker = func() error { return nil }
	}

	if err != nil {
		slog.Error("failed to initialize worker", "err", err)
		os.Exit(1)
	}
	defer func() {
		if closeWorker != nil {
			_ = closeWorker()
		}
	}()

	healthMux := http.NewServeMux()

	// Liveness: the process is up and able to serve HTTP. Keep this cheap and
	// dependency-free so monitoring or a service supervisor can distinguish a
	// live process from dependency degradation.
	healthMux.HandleFunc("/health", func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	// Readiness: dependencies (currently the database) are actually usable.
	// This is intentionally separate from liveness so dependency degradation
	// can be observed without implying that the process itself is dead.
	healthMux.HandleFunc("/ready", func(rw http.ResponseWriter, r *http.Request) {
		readyCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		if err := app.Ready(readyCtx); err != nil {
			slog.Warn("readiness check failed", "err", err)
			rw.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		rw.WriteHeader(http.StatusOK)
	})

	healthSrv := &http.Server{
		Addr:    ":8082",
		Handler: healthMux,
	}

	go func() {
		slog.Info("health check server listening", "addr", healthSrv.Addr)

		if err := healthSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("health check server failed", "err", err)
		}
	}()

	slog.Info("dependencies injected", "info", "worker will be running")

	if err := w.Run(ctx); err != nil && err != context.Canceled {
		slog.Error("worker stopped", "err", err)
		os.Exit(1)
	}

	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		runtimepolicy.ApplicationShutdownTimeout,
	)

	defer cancel()

	if err := healthSrv.Shutdown(shutdownCtx); err != nil {
		slog.Error("health check server shutdown failed", "err", err)
	}

	if err := app.Close(shutdownCtx); err != nil {
		slog.Error("application shutdown failed", "err", err)
	}

	slog.Info("worker shut down gracefully")
}
