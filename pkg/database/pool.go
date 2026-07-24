package database

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
)

const (
	maxRetries = 30
	retryDelay = 2 * time.Second
)

func NewPool(ctx context.Context, cfg *pgxpool.Config) (*pgxpool.Pool, error) {
	cfg.AfterConnect = func(
		ctx context.Context,
		conn *pgx.Conn,
	) error {
		return pgxvec.RegisterTypes(ctx, conn)
	}

	var (
		db  *pgxpool.Pool
		err error
	)

	for attempt := 1; attempt <= maxRetries; attempt++ {
		db, err = pgxpool.NewWithConfig(ctx, cfg)

		if err == nil {
			err = db.Ping(ctx)
		}

		if err == nil {
			slog.Info("database initialized",
				"attempt", attempt,
			)

			return db, nil
		}

		if db != nil {
			db.Close()
			db = nil
		}

		slog.Warn("database initialization not ready, retrying",
			"attempt", attempt,
			"max_attempts", maxRetries,
			"err", err,
		)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retryDelay):
		}
	}

	return nil, err
}
