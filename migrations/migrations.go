package migrations

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const categoryDomainVersion = 1

//go:embed 000001_category_domain.up.sql
var categoryDomainUp string

// Up applies every pending worker-owned schema migration atomically.
func Up(ctx context.Context, db interface {
	Begin(context.Context) (pgx.Tx, error)
}) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	// Serialize worker deployments sharing the same database. The transaction
	// scoped lock is released automatically on commit or rollback.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('yevmiye-category-worker-migrations'))`); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS categorization_schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}

	var applied bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM categorization_schema_migrations
			WHERE version = $1
		)
	`, categoryDomainVersion).Scan(&applied); err != nil {
		return fmt.Errorf("check migration version %d: %w", categoryDomainVersion, err)
	}

	if applied {
		return tx.Commit(ctx)
	}

	if _, err := tx.Exec(ctx, categoryDomainUp); err != nil {
		return fmt.Errorf("apply migration version %d: %w", categoryDomainVersion, err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO categorization_schema_migrations (version)
		VALUES ($1)
	`, categoryDomainVersion); err != nil {
		return fmt.Errorf("record migration version %d: %w", categoryDomainVersion, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration version %d: %w", categoryDomainVersion, err)
	}

	return nil
}
