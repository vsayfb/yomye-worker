package logger

import (
	"log/slog"
	"os"

	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
)

func Init(env string) slog.Handler {
	var (
		opts    slog.HandlerOptions
		handler slog.Handler
	)

	switch env {
	case config.EnvironmentProduction:
		opts.Level = slog.LevelInfo
		handler = slog.NewJSONHandler(os.Stdout, &opts)
	default:
		opts.Level = slog.LevelDebug
		handler = slog.NewTextHandler(os.Stdout, &opts)
	}

	slog.SetDefault(slog.New(handler))

	return handler
}
