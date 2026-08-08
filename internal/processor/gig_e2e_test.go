package processor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestGigProcessor_E2E_ClassifiesAndOpensListing runs the real pipeline: a
// live AI provider classifies and extracts the listing, real embeddings are
// generated for the category path, everything is persisted to a real
// database, and real SQS notifications are published. Replays of an OPEN
// listing intentionally publish again to provide at-least-once event delivery.
//
// Yövmiye is gig-only for launch, so the processor resolves the gig base
// category directly and requires a selectable gig category association.
func TestGigProcessor_E2E_ClassifiesAndOpensListing(t *testing.T) {
	env := newE2EEnv(t)
	ctx := context.Background()

	env.seedBaseCategories(t, ctx)

	description := "Experienced freelance logo and brand identity designer available for short-term gigs. " +
		"Fixed price $150, delivered within 3 days, unlimited revisions."
	listingID := env.seedListing(t, ctx, description)

	msg := ListingCreatedMessage{
		ListingID:   listingID,
		Description: description,
		AuthorID:    uuid.New(),
	}
	body, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	record := Message{ID: uuid.New().String(), Type: "GigPublished", Body: string(body)}

	if err := env.gig.Process(ctx, record); err != nil {
		t.Fatalf("process listing: %v", err)
	}

	var status string
	if err := env.db.QueryRow(ctx, `SELECT status FROM listings WHERE id = $1`, listingID).Scan(&status); err != nil {
		t.Fatalf("read listing status: %v", err)
	}
	if status != listingStatusOpen {
		t.Fatalf("expected listing status %q, got %q", listingStatusOpen, status)
	}

	var categoryCount int
	if err := env.db.QueryRow(ctx,
		`SELECT count(*) FROM listing_categories WHERE listing_id = $1`, listingID,
	).Scan(&categoryCount); err != nil {
		t.Fatalf("count listing_categories: %v", err)
	}
	if categoryCount == 0 {
		t.Fatalf("expected the listing to be linked to a category")
	}

	var baseCategorySlug string
	if err := env.db.QueryRow(ctx, `
		SELECT bc.slug
		FROM listings l
		JOIN base_categories bc ON bc.id = l.base_category_id
		WHERE l.id = $1
	`, listingID).Scan(&baseCategorySlug); err != nil {
		t.Fatalf("read listing base category: %v", err)
	}

	if baseCategorySlug != BaseCategoryGig {
		t.Fatalf("expected gig base category, got %q", baseCategorySlug)
	}

	var title string
	if err := env.db.QueryRow(ctx,
		`SELECT title FROM gig_details WHERE gig_id = $1`, listingID,
	).Scan(&title); err != nil {
		t.Fatalf("read gig details: %v", err)
	}
	if strings.TrimSpace(title) == "" {
		t.Fatalf("expected a non-empty gig title")
	}

	var selectable bool
	if err := env.db.QueryRow(ctx, `
		SELECT c.is_selectable
		FROM listing_categories lc
		JOIN categories c ON c.id = lc.category_id
		WHERE lc.listing_id = $1
	`, listingID).Scan(&selectable); err != nil {
		t.Fatalf("read selected category: %v", err)
	}
	if !selectable {
		t.Fatal("listing was associated with a non-selectable category")
	}

	// Idempotent database replay: SQS redelivery must not change persisted
	// categorization. Notifications are intentionally re-published so a prior
	// post-commit publication failure can recover on redelivery.
	replay := Message{ID: uuid.New().String(), Type: "GigPublished", Body: string(body)}
	if err := env.gig.Process(ctx, replay); err != nil {
		t.Fatalf("reprocess already-open listing: %v", err)
	}

	var categoryCountAfterReplay int
	if err := env.db.QueryRow(ctx,
		`SELECT count(*) FROM listing_categories WHERE listing_id = $1`, listingID,
	).Scan(&categoryCountAfterReplay); err != nil {
		t.Fatalf("count listing_categories after replay: %v", err)
	}
	if categoryCountAfterReplay != categoryCount {
		t.Fatalf("replay changed the number of linked categories: before=%d after=%d",
			categoryCount, categoryCountAfterReplay)
	}
}
