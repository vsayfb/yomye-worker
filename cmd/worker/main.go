package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	w, err := worker.New(ctx, app.cfg, app)

	if err != nil {
		slog.Error("failed to initialize worker", "err", err)
		os.Exit(1)
	}

	healthMux := http.NewServeMux()

	healthMux.HandleFunc("/health", func(rw http.ResponseWriter, r *http.Request) {
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
		10*time.Second,
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
