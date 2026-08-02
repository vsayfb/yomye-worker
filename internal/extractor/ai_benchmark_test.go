package extractor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
)

const minimumBenchmarkExampleCount = 271

type benchmarkAttempt struct {
	generator        string
	baseCategorySlug string
	extraction       *ExtractedListing
	err              error
	elapsed          time.Duration
}

func TestAIProviderFallbackBenchmark(t *testing.T) {
	if os.Getenv("RUN_AI_BENCHMARK") != "1" {
		t.Skip("set RUN_AI_BENCHMARK=1; use `make benchmark-ai` to run in Docker")
	}

	examples := readBenchmarkExamples(t, "example.txt")
	if len(examples) < minimumBenchmarkExampleCount {
		t.Fatalf(
			"loaded %d examples, want at least %d",
			len(examples),
			minimumBenchmarkExampleCount,
		)
	}

	providers := configuredBenchmarkProviders()
	if len(providers) == 0 {
		t.Fatal("no AI providers configured")
	}

	ctx := context.Background()
	db := openBenchmarkDatabase(t, ctx)
	defer db.Close()
	createBenchmarkTables(t, ctx, db)

	runID := uuid.New()
	startedAt := time.Now().UTC()
	if _, err := db.Exec(ctx, `
		INSERT INTO category_ai_benchmark_runs (
			id, example_count, provider_count, started_at
		)
		VALUES ($1, $2, $3, $4)
	`, runID, len(examples), len(providers), startedAt); err != nil {
		t.Fatalf("create benchmark run: %v", err)
	}

	t.Logf(
		"benchmark run_id=%s examples=%d providers=%d attempts=%d",
		runID,
		len(examples),
		len(providers),
		len(examples)*len(providers),
	)

	catalog := loadCategoryCatalog(t, ctx, db, "gig")

	successCount := 0
	fallbackSuccessCount := 0
	for exampleIndex, description := range examples {
		attempts := make([]benchmarkAttempt, 0, len(providers))
		winner := -1

		for providerIndex, provider := range providers {
			attempt := runBenchmarkAttempt(
				ctx,
				provider,
				description,
				catalog,
			)
			attempts = append(attempts, attempt)
			if attempt.err == nil {
				successCount++
				if winner == -1 {
					winner = providerIndex
					fallbackSuccessCount++
				}
			}
		}

		for providerIndex, attempt := range attempts {
			persistBenchmarkAttempt(
				t,
				ctx,
				db,
				runID,
				exampleIndex+1,
				description,
				providerIndex == winner,
				attempt,
			)
		}

		if (exampleIndex+1)%10 == 0 || exampleIndex+1 == len(examples) {
			t.Logf(
				"benchmark run_id=%s progress=%d/%d fallback_successes=%d",
				runID,
				exampleIndex+1,
				len(examples),
				fallbackSuccessCount,
			)
		}
	}

	finishedAt := time.Now().UTC()
	if _, err := db.Exec(ctx, `
		UPDATE category_ai_benchmark_runs
		SET finished_at = $2,
		    successful_attempts = $3,
		    fallback_successes = $4
		WHERE id = $1
	`, runID, finishedAt, successCount, fallbackSuccessCount); err != nil {
		t.Fatalf("finish benchmark run: %v", err)
	}

	t.Logf(
		"benchmark complete run_id=%s elapsed=%s successful_attempts=%d/%d fallback_successes=%d/%d",
		runID,
		finishedAt.Sub(startedAt).Round(time.Second),
		successCount,
		len(examples)*len(providers),
		fallbackSuccessCount,
		len(examples),
	)
}

func configuredBenchmarkProviders() []Extractor {
	var providers []Extractor

	gemini := config.AIConfig{
		APIKey: os.Getenv(config.EnvGeminiAPIKey),
		Model:  os.Getenv(config.EnvGeminiModel),
	}
	if gemini.APIKey != "" && gemini.Model != "" {
		providers = append(providers, NewGeminiExtractor(&gemini))
	}

	openAICompatible := []struct {
		name    string
		apiKey  string
		baseURL string
		model   string
	}{
		{
			name:    "groq",
			apiKey:  os.Getenv(config.EnvGroqAPIKey),
			baseURL: os.Getenv(config.EnvGroqBaseURL),
			model:   os.Getenv(config.EnvGroqModel),
		},
		{
			name:    "qwen",
			apiKey:  os.Getenv(config.EnvQwenAPIKey),
			baseURL: os.Getenv(config.EnvQwenBaseURL),
			model:   os.Getenv(config.EnvQwenModel),
		},
		{
			name:    "nvidia",
			apiKey:  os.Getenv(config.EnvNvidiaAPIKey),
			baseURL: os.Getenv(config.EnvNvidiaBaseURL),
			model:   os.Getenv(config.EnvNvidiaModel),
		},
	}
	for _, provider := range openAICompatible {
		if provider.apiKey == "" || provider.baseURL == "" || provider.model == "" {
			continue
		}
		providerConfig := config.AIConfig{
			APIKey:  provider.apiKey,
			BaseURL: provider.baseURL,
			Model:   provider.model,
		}
		providers = append(
			providers,
			NewOpenAIExtractor(&providerConfig, provider.name),
		)
	}

	return providers
}

func runBenchmarkAttempt(
	ctx context.Context,
	provider Extractor,
	description string,
	catalog string,
) benchmarkAttempt {
	startedAt := time.Now()
	attempt := benchmarkAttempt{
		generator:        provider.Generator(),
		baseCategorySlug: "gig",
	}
	attempt.extraction, attempt.err = provider.Extract(
		ctx,
		description,
		attempt.baseCategorySlug,
		catalog,
	)
	attempt.elapsed = time.Since(startedAt)
	return attempt
}

func persistBenchmarkAttempt(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	runID uuid.UUID,
	exampleNumber int,
	description string,
	isFallbackWinner bool,
	attempt benchmarkAttempt,
) {
	t.Helper()

	var extractionJSON []byte
	if attempt.extraction != nil {
		var err error
		extractionJSON, err = json.Marshal(attempt.extraction)
		if err != nil {
			t.Fatalf("encode attempt %d/%s: %v", exampleNumber, attempt.generator, err)
		}
	}

	var errorText *string
	if attempt.err != nil {
		text := attempt.err.Error()
		errorText = &text
	}

	if _, err := db.Exec(ctx, `
		INSERT INTO category_ai_benchmark_results (
			run_id,
			example_number,
			description,
			generator,
			is_fallback_winner,
			success,
			base_category_slug,
			extraction,
			error,
			duration_ms
		)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9, $10)
	`, runID, exampleNumber, description, attempt.generator,
		isFallbackWinner, attempt.err == nil, attempt.baseCategorySlug,
		extractionJSON, errorText, attempt.elapsed.Milliseconds()); err != nil {
		t.Fatalf("persist attempt %d/%s: %v", exampleNumber, attempt.generator, err)
	}
}

func readBenchmarkExamples(t *testing.T, path string) []string {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open benchmark examples: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)

	var examples []string
	for scanner.Scan() {
		description := strings.TrimSpace(scanner.Text())
		if description != "" {
			examples = append(examples, description)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read benchmark examples: %v", err)
	}
	return examples
}

func openBenchmarkDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	required := func(key string) string {
		t.Helper()
		value := os.Getenv(key)
		if value == "" {
			t.Fatalf("missing required environment variable %s", key)
		}
		return value
	}

	sslMode := os.Getenv(config.EnvDBSSLMode)
	if sslMode == "" {
		sslMode = "disable"
	}
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		required(config.EnvDBHost),
		required(config.EnvDBPort),
		required(config.EnvDBUser),
		required(config.EnvDBPassword),
		required(config.EnvDBName),
		sslMode,
	)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping database: %v", err)
	}
	return pool
}

func loadCategoryCatalog(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	baseCategorySlug string,
) string {
	t.Helper()

	rows, err := db.Query(ctx, `
		WITH RECURSIVE tree AS (
			SELECT c.id, c.canonical_name, c.canonical_description, c.depth,
			       ARRAY[c.canonical_slug]::text[] AS sort_path
			FROM categories c
			JOIN base_categories bc ON bc.id = c.base_category_id
			WHERE bc.slug = $1
			  AND bc.is_active = TRUE
			  AND c.parent_id IS NULL
			  AND c.is_active = TRUE

			UNION ALL

			SELECT c.id, c.canonical_name, c.canonical_description, c.depth,
			       tree.sort_path || c.canonical_slug
			FROM categories c
			JOIN tree ON c.parent_id = tree.id
			WHERE c.is_active = TRUE
		)
		SELECT canonical_name, canonical_description, is_selectable, depth
		FROM tree
		ORDER BY sort_path
	`, baseCategorySlug)
	if err != nil {
		t.Fatalf("load %s category catalog: %v", baseCategorySlug, err)
	}
	defer rows.Close()

	var catalog strings.Builder
	for rows.Next() {
		var (
			name         string
			description  string
			isSelectable bool
			depth        int
		)
		if err := rows.Scan(&name, &description, &isSelectable, &depth); err != nil {
			t.Fatalf("scan %s category catalog: %v", baseCategorySlug, err)
		}
		catalog.WriteString(strings.Repeat("  ", depth))
		catalog.WriteString("- ")
		catalog.WriteString(name)
		catalog.WriteString(": ")
		catalog.WriteString(description)
		catalog.WriteString(fmt.Sprintf(" [selectable=%t]", isSelectable))
		catalog.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read %s category catalog: %v", baseCategorySlug, err)
	}
	return strings.TrimSpace(catalog.String())
}

func createBenchmarkTables(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()

	_, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS category_ai_benchmark_runs (
			id UUID PRIMARY KEY,
			example_count INTEGER NOT NULL,
			provider_count INTEGER NOT NULL,
			successful_attempts INTEGER,
			fallback_successes INTEGER,
			started_at TIMESTAMPTZ NOT NULL,
			finished_at TIMESTAMPTZ
		);

		CREATE TABLE IF NOT EXISTS category_ai_benchmark_results (
			run_id UUID NOT NULL
				REFERENCES category_ai_benchmark_runs(id) ON DELETE CASCADE,
			example_number INTEGER NOT NULL,
			description TEXT NOT NULL,
			generator TEXT NOT NULL,
			is_fallback_winner BOOLEAN NOT NULL,
			success BOOLEAN NOT NULL,
			base_category_slug TEXT,
			extraction JSONB,
			error TEXT,
			duration_ms BIGINT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (run_id, example_number, generator)
		);

		CREATE INDEX IF NOT EXISTS category_ai_benchmark_results_run_success_idx
			ON category_ai_benchmark_results(run_id, success);

		CREATE INDEX IF NOT EXISTS category_ai_benchmark_results_generator_idx
			ON category_ai_benchmark_results(generator);
	`)
	if err != nil {
		t.Fatalf("create benchmark tables: %v", err)
	}
}
