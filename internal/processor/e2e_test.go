package processor

// These tests exercise the real pipeline end to end: a real Postgres
// database, a real (configured) AI extraction provider, a real local
// embeddings endpoint, and a real/LocalStack SQS queue for notifications.
// They do NOT run as part of a normal `go test ./...` - set RUN_E2E_TESTS=1
// and point the usual DB_*/AWS_*/AI_* env vars (a real .env file, the same
// shape config.Load() already reads) at a database that has the FULL
// platform schema applied - not just this repo's two category-tree
// migrations under /migrations.
//
// Tables/functions this package's own queries assume exist, beyond what
// /migrations creates here (owned by another service, not this repo):
//   - base_categories(id, slug, is_active) with an active 'gig' row
//   - categories with canonical_name, canonical_slug, canonical_description,
//     hierarchy, embedding, generated_by, and is_selectable
//   - category_localizations(category_id, locale, name, slug, description,
//     generated_by), unique(category_id, locale)
//   - listings(id, status, base_category_id, description_clean, ...)
//   - listing_categories(listing_id, category_id, base_category_id), unique(listing_id, category_id)
//   - gig_details(gig_id unique, title, duration_type, pay_amount, pay_currency, expires_at)
//   - users(id, profession_request_id)
//   - user_categories(user_id unique, category_id)
//
// The seed helpers below intentionally only set the columns gig.go /
// profession.go themselves read or write. If your real tables have other
// NOT NULL columns, the INSERTs will fail with a plain Postgres error -
// extend seedListing/seedUser/seedBaseCategories rather than guess further.
//
// Run with: make test-e2e (see Makefile), or manually:
//   set -a; . ./.env; set +a; RUN_E2E_TESTS=1 go test ./internal/processor/... -run E2E -v

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
	"github.com/vsayfb/gig-platform-categorization-service/internal/extractor"
	"github.com/vsayfb/gig-platform-categorization-service/internal/notification"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/database"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/embeddings"
)

func skipUnlessE2E(t *testing.T) {
	t.Helper()
	if os.Getenv("RUN_E2E_TESTS") != "1" {
		t.Skip("set RUN_E2E_TESTS=1 (with a real .env pointed at a fully-migrated DB) to run this test; see e2e_test.go")
	}
}

type e2eEnv struct {
	cfg        *config.Config
	db         *pgxpool.Pool
	gig        *GigProcessor
	profession *ProfessionProcessor
}

// newE2EEnv wires the exact same dependency graph cmd/worker/app.go builds
// for the real binary, so these tests exercise the real thing rather than a
// parallel test-only construction of it.
func newE2EEnv(t *testing.T) *e2eEnv {
	t.Helper()
	skipUnlessE2E(t)

	ctx := context.Background()

	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DB.DSN())
	if err != nil {
		t.Fatalf("parse database url: %v", err)
	}
	poolCfg.MaxConns = 4

	db, err := database.NewPool(ctx, poolCfg)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	t.Cleanup(db.Close)

	embeddingClient := embeddings.NewLocalClient(cfg)

	var extractors []extractor.Extractor

	if cfg.AI.Gemini.APIKey != "" && cfg.AI.Gemini.Model != "" {
		extractors = append(extractors, extractor.NewGeminiExtractor(&cfg.AI.Gemini))
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
		if provider.cfg.APIKey != "" && provider.cfg.BaseURL != "" && provider.cfg.Model != "" {
			extractors = append(extractors, extractor.NewOpenAIExtractor(provider.cfg, provider.name))
		}
	}

	if len(extractors) == 0 {
		t.Fatalf("no AI extraction provider is configured in the environment")
	}

	fallback := extractor.NewFallbackExtractor(extractors...)

	publisher, err := notification.NewNotificationPublisher(ctx, cfg)
	if err != nil {
		t.Fatalf("create sqs publisher: %v", err)
	}

	repo := NewRepository(db)

	return &e2eEnv{
		cfg:        cfg,
		db:         db,
		gig:        NewGigProcessor(repo, embeddingClient, fallback, publisher, cfg),
		profession: NewProfessionProcessor(repo, embeddingClient, fallback, cfg),
	}
}

// seedBaseCategories makes sure the two base categories this service
// classifies against exist and are active. Safe to call repeatedly; assumes
// a unique constraint on base_categories.slug.
func (e *e2eEnv) seedBaseCategories(t *testing.T, ctx context.Context) {
	t.Helper()

	for _, slug := range []string{BaseCategoryGig} {
		_, err := e.db.Exec(ctx, `
			INSERT INTO base_categories (id, slug, is_active)
			VALUES ($1, $2, TRUE)
			ON CONFLICT (slug) DO UPDATE SET is_active = TRUE
		`, uuid.New(), slug)
		if err != nil {
			t.Fatalf("seed base category %q: %v", slug, err)
		}
	}
}

// seedListing inserts a minimal listings row in PROCESSING status, the
// precondition LockListing requires, and registers cleanup for every table
// gig.go's pipeline can write to for this listing.
func (e *e2eEnv) seedListing(t *testing.T, ctx context.Context, description string) uuid.UUID {
	t.Helper()

	id := uuid.New()

	_, err := e.db.Exec(ctx, `
		INSERT INTO listings (id, author_id, description, status)
		VALUES ($1, $2, $3, 'PROCESSING')
	`, id, uuid.New(), description)
	if err != nil {
		t.Fatalf("seed listing: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		for _, stmt := range []string{
			`DELETE FROM listing_categories WHERE listing_id = $1`,
			`DELETE FROM gig_details WHERE gig_id = $1`,
			`DELETE FROM sale_details WHERE listing_id = $1`,
			`DELETE FROM listings WHERE id = $1`,
		} {
			if _, err := e.db.Exec(cleanupCtx, stmt, id); err != nil {
				t.Logf("cleanup %q for listing %s: %v", stmt, id, err)
			}
		}
	})

	return id
}

// seedUser inserts a minimal users row with profession_request_id already
// pointed at requestID - matching the contract profession.go expects: the
// publisher writes this column before publishing the event, and the worker
// only proceeds if it still matches by the time it processes the message.
func (e *e2eEnv) seedUser(t *testing.T, ctx context.Context, requestID uuid.UUID) uuid.UUID {
	t.Helper()

	id := uuid.New()

	_, err := e.db.Exec(ctx, `
		INSERT INTO users (id, profession_request_id)
		VALUES ($1, $2)
	`, id, requestID)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		for _, stmt := range []string{
			`DELETE FROM user_categories WHERE user_id = $1`,
			`DELETE FROM users WHERE id = $1`,
		} {
			if _, err := e.db.Exec(cleanupCtx, stmt, id); err != nil {
				t.Logf("cleanup %q for user %s: %v", stmt, id, err)
			}
		}
	})

	return id
}
