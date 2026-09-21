package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
	"github.com/vsayfb/gig-platform-categorization-service/internal/evaluation"
	"github.com/vsayfb/gig-platform-categorization-service/internal/extractor"
)

const baseCategorySlug = "gig"

func main() {
	var (
		baselinePath = flag.String("baseline", "testdata/evaluation/golden.json", "approved golden baseline to compare against")
		capturePath  = flag.String("capture", "", "capture current behavior into a candidate baseline instead of comparing")
		examplesPath = flag.String("examples", "internal/extractor/example.txt", "line-delimited descriptions used when capturing")
		force        = flag.Bool("force", false, "allow capture to overwrite an existing output file")
		limit        = flag.Int("limit", 0, "process only the first N cases; 0 means all")
	)
	flag.Parse()

	ctx := context.Background()
	if err := run(ctx, options{
		baselinePath: *baselinePath,
		capturePath:  *capturePath,
		examplesPath: *examplesPath,
		force:        *force,
		limit:        *limit,
	}); err != nil {
		log.Fatal(err)
	}
}

type options struct {
	baselinePath string
	capturePath  string
	examplesPath string
	force        bool
	limit        int
}

func run(ctx context.Context, opts options) error {
	cfg, err := config.Load(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	providers := configuredExtractors(cfg)
	if len(providers) == 0 {
		return fmt.Errorf("no AI extraction provider is configured")
	}
	fallback := extractor.NewFallbackExtractor(providers...)

	db, err := pgxpool.New(ctx, cfg.DB.DSN())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	catalog, err := loadCategoryCatalog(ctx, db, baseCategorySlug)
	if err != nil {
		return err
	}
	catalogHash := sha256.Sum256([]byte(catalog))

	if opts.capturePath != "" {
		return captureBaseline(ctx, fallback, catalog, hex.EncodeToString(catalogHash[:]), opts)
	}
	return evaluateBaseline(ctx, fallback, catalog, hex.EncodeToString(catalogHash[:]), opts)
}

func configuredExtractors(cfg *config.Config) []extractor.Extractor {
	return extractor.ConfiguredProviders(&cfg.AI)
}

func captureBaseline(
	ctx context.Context,
	fallback *extractor.FallbackExtractor,
	catalog string,
	catalogHash string,
	opts options,
) error {
	descriptions, err := readExamples(opts.examplesPath)
	if err != nil {
		return err
	}
	descriptions = applyLimit(descriptions, opts.limit)
	if len(descriptions) == 0 {
		return fmt.Errorf("no evaluation descriptions loaded")
	}

	baseline := evaluation.Baseline{
		Version:       evaluation.BaselineVersion,
		CapturedAt:    time.Now().UTC(),
		CatalogSHA256: catalogHash,
		Cases:         make([]evaluation.Case, 0, len(descriptions)),
	}

	for index, description := range descriptions {
		result, err := fallback.ExtractCategoryPath(ctx, description, baseCategorySlug, catalog)
		if err != nil {
			return fmt.Errorf("capture case %d/%d: %w", index+1, len(descriptions), err)
		}
		item, err := evaluation.CaseFromExtraction(description, result)
		if err != nil {
			return fmt.Errorf("capture case %d/%d: %w", index+1, len(descriptions), err)
		}
		baseline.Cases = append(baseline.Cases, item)
		fmt.Printf("capture %d/%d: %s\n", index+1, len(descriptions), item.GeneratedBy)
	}

	if err := evaluation.Write(opts.capturePath, baseline, opts.force); err != nil {
		return err
	}
	fmt.Printf("candidate baseline written to %s (%d cases)\n", opts.capturePath, len(baseline.Cases))
	fmt.Println("review it before promoting it to testdata/evaluation/golden.json")
	return nil
}

func evaluateBaseline(
	ctx context.Context,
	fallback *extractor.FallbackExtractor,
	catalog string,
	catalogHash string,
	opts options,
) error {
	baseline, err := evaluation.Load(opts.baselinePath)
	if err != nil {
		return err
	}
	if baseline.CatalogSHA256 != "" && baseline.CatalogSHA256 != catalogHash {
		fmt.Printf("WARN taxonomy catalog differs from baseline capture (%s -> %s)\n", baseline.CatalogSHA256, catalogHash)
	}
	cases := applyLimit(baseline.Cases, opts.limit)

	var exact, leaf, locale, failures int
	for index, expected := range cases {
		result, extractErr := fallback.ExtractCategoryPath(ctx, expected.Description, baseCategorySlug, catalog)
		if extractErr != nil {
			failures++
			fmt.Printf("FAIL %d/%d extraction error: %v\n", index+1, len(cases), extractErr)
			continue
		}

		comparison, compareErr := evaluation.Compare(expected, result)
		if compareErr != nil {
			failures++
			fmt.Printf("FAIL %d/%d comparison error: %v\n", index+1, len(cases), compareErr)
			continue
		}
		if comparison.LocaleMatch {
			locale++
		}
		if comparison.LeafMatch {
			leaf++
		}
		if comparison.Exact {
			exact++
			fmt.Printf("PASS %d/%d %s\n", index+1, len(cases), result.GeneratedBy)
			continue
		}

		failures++
		fmt.Printf(
			"FAIL %d/%d locale=%s->%s path=%s -> %s generator=%s\n",
			index+1,
			len(cases),
			comparison.ExpectedLocale,
			comparison.ActualLocale,
			comparison.ExpectedPath,
			comparison.ActualPath,
			result.GeneratedBy,
		)
	}

	fmt.Println()
	fmt.Printf("cases:        %d\n", len(cases))
	fmt.Printf("exact:        %d (%.1f%%)\n", exact, percent(exact, len(cases)))
	fmt.Printf("leaf match:   %d (%.1f%%)\n", leaf, percent(leaf, len(cases)))
	fmt.Printf("locale match: %d (%.1f%%)\n", locale, percent(locale, len(cases)))
	fmt.Printf("failures:     %d\n", failures)

	if failures > 0 {
		return fmt.Errorf("categorization regression detected: %d/%d cases differ from the approved baseline", failures, len(cases))
	}
	return nil
}

func loadCategoryCatalog(ctx context.Context, db *pgxpool.Pool, baseSlug string) (string, error) {
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
	`, baseSlug)
	if err != nil {
		return "", fmt.Errorf("load %s category catalog: %w", baseSlug, err)
	}
	defer rows.Close()

	var catalog strings.Builder
	for rows.Next() {
		var name, description string
		var selectable bool
		var depth int
		if err := rows.Scan(&name, &description, &selectable, &depth); err != nil {
			return "", fmt.Errorf("scan %s category catalog: %w", baseSlug, err)
		}
		catalog.WriteString(strings.Repeat("  ", depth))
		catalog.WriteString("- ")
		catalog.WriteString(name)
		catalog.WriteString(": ")
		catalog.WriteString(description)
		catalog.WriteString(fmt.Sprintf(" [selectable=%t]", selectable))
		catalog.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("read %s category catalog: %w", baseSlug, err)
	}
	return strings.TrimSpace(catalog.String()), nil
}

func readExamples(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open evaluation examples: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	var examples []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			examples = append(examples, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read evaluation examples: %w", err)
	}
	return examples, nil
}

func applyLimit[T any](items []T, limit int) []T {
	if limit <= 0 || limit >= len(items) {
		return items
	}
	return items[:limit]
}

func percent(value, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(value) * 100 / float64(total)
}
