package processor

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
	"github.com/vsayfb/gig-platform-categorization-service/internal/extractor"
)

func fakeExtractedProfession() *extractor.ExtractedListing {
	return &extractor.ExtractedListing{
		BaseCategorySlug: BaseCategoryGig,
		SourceLocale:     "en",
		GeneratedBy:      "fake",
		CategoryPath: []extractor.ExtractedCategoryNode{
			testCategoryNode("Design", "Design", true),
		},
	}
}

// TestProfessionProcessor_LockedCheckCatchesRaceAfterEarlyCheckPasses covers
// exactly the race an e2e/integration test can't reliably land on: the
// cheap early check (GetCurrentProfessionRequest) sees a message that's
// still current, but by the time extraction/embedding finish and the
// authoritative locked check (LockCurrentProfessionRequest) runs inside the
// transaction, a newer request has superseded it. A mock Repository can
// simulate that deterministically by just returning a different id from
// the two calls - no real timing race required.
func TestProfessionProcessor_LockedCheckCatchesRaceAfterEarlyCheckPasses(t *testing.T) {
	userID := uuid.New()
	requestID := uuid.New()
	newerRequestID := uuid.New()

	repo := &fakeRepository{
		getCurrentProfessionRequestFn: func(ctx context.Context, u uuid.UUID) (uuid.UUID, bool, error) {
			// Early check: the message still looks current here.
			return requestID, true, nil
		},
		resolveBaseCategoryFn: func(ctx context.Context, slug string) (uuid.UUID, error) {
			return uuid.New(), nil
		},
		listActiveCategoriesFn: func(ctx context.Context, baseCategoryID uuid.UUID) ([]Category, error) {
			return nil, nil
		},
		lockCurrentProfessionRequestFn: func(ctx context.Context, u uuid.UUID) (uuid.UUID, bool, error) {
			// Authoritative check: a newer request has since superseded
			// this one.
			return newerRequestID, true, nil
		},
		findCategoryFn: func(ctx context.Context, baseCategoryID uuid.UUID, parentID *uuid.UUID, name, slug string, embedding []float32) (*Category, error) {
			t.Fatal("FindCategory should not be reached once the locked check finds a newer request")
			return nil, nil
		},
	}

	fallback := extractor.NewFallbackExtractor(&fakeExtractor{
		extract: func(context.Context, string, string, string) (*extractor.ExtractedListing, error) {
			t.Fatal("profession processing must not invoke full listing/gig-details extraction")
			return nil, nil
		},
		extractCategoryPath: func(ctx context.Context, description, baseCategorySlug, catalog string) (*extractor.ExtractedListing, error) {
			return fakeExtractedProfession(), nil
		},
	})

	proc := NewProfessionProcessor(repo, &fakeEmbeddingClient{}, fallback, &config.Config{})

	msg := NewProfession{RequestID: requestID, AuthorID: userID, Description: "graphic designer"}
	body, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}

	err = proc.Process(context.Background(), Message{ID: "1", Type: "ProfessionPublished", Body: string(body)})
	if err != nil {
		t.Fatalf("expected the race to be handled as a clean no-op, got error: %v", err)
	}

	if repo.upsertUserProfessionCategoryCalls != 0 {
		t.Fatalf(
			"expected no category to be linked once the locked check finds a newer request, got %d link calls",
			repo.upsertUserProfessionCategoryCalls,
		)
	}
}

// TestProfessionProcessor_LinksCategoryWhenNotStale is the positive control
// for the race test above: with the same wiring, when the locked check
// still agrees with the early check, the category must actually get linked.
// Without this, the negative test could pass vacuously (e.g. due to an
// unrelated bug that stops linking from happening at all).
func TestProfessionProcessor_LinksCategoryWhenNotStale(t *testing.T) {
	userID := uuid.New()
	requestID := uuid.New()
	leafCategoryID := uuid.New()

	var linkedUserID, linkedCategoryID uuid.UUID

	repo := &fakeRepository{
		getCurrentProfessionRequestFn: func(ctx context.Context, u uuid.UUID) (uuid.UUID, bool, error) {
			return requestID, true, nil
		},
		resolveBaseCategoryFn: func(ctx context.Context, slug string) (uuid.UUID, error) {
			return uuid.New(), nil
		},
		listActiveCategoriesFn: func(ctx context.Context, baseCategoryID uuid.UUID) ([]Category, error) {
			return nil, nil
		},
		lockCurrentProfessionRequestFn: func(ctx context.Context, u uuid.UUID) (uuid.UUID, bool, error) {
			return requestID, true, nil // still matches - not stale
		},
		findCategoryFn: func(ctx context.Context, baseCategoryID uuid.UUID, parentID *uuid.UUID, name, slug string, embedding []float32) (*Category, error) {
			return &Category{ID: leafCategoryID, CanonicalName: name, CanonicalSlug: slug, IsSelectable: true, IsActive: true}, nil
		},
		upsertUserProfessionCategoryFn: func(ctx context.Context, u, categoryID uuid.UUID) error {
			linkedUserID = u
			linkedCategoryID = categoryID
			return nil
		},
	}

	fallback := extractor.NewFallbackExtractor(&fakeExtractor{
		extract: func(context.Context, string, string, string) (*extractor.ExtractedListing, error) {
			t.Fatal("profession processing must not invoke full listing/gig-details extraction")
			return nil, nil
		},
		extractCategoryPath: func(ctx context.Context, description, baseCategorySlug, catalog string) (*extractor.ExtractedListing, error) {
			return fakeExtractedProfession(), nil
		},
	})

	proc := NewProfessionProcessor(repo, &fakeEmbeddingClient{}, fallback, &config.Config{})

	msg := NewProfession{RequestID: requestID, AuthorID: userID, Description: "graphic designer"}
	body, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}

	err = proc.Process(context.Background(), Message{ID: "1", Type: "ProfessionPublished", Body: string(body)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.upsertUserProfessionCategoryCalls != 1 {
		t.Fatalf("expected exactly one link call, got %d", repo.upsertUserProfessionCategoryCalls)
	}
	if linkedUserID != userID {
		t.Fatalf("expected category linked to user %s, got %s", userID, linkedUserID)
	}
	if linkedCategoryID != leafCategoryID {
		t.Fatalf("expected leaf category %s to be linked, got %s", leafCategoryID, linkedCategoryID)
	}
}
