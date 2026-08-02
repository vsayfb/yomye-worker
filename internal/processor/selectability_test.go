package processor

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/vsayfb/gig-platform-categorization-service/internal/extractor"
)

func TestValidateCategoryPathRequiresSelectableFinalNode(t *testing.T) {
	path := []extractor.ExtractedCategoryNode{
		testCategoryNode("Construction and Technical Services", "Yapı ve Teknik Hizmetler", false),
		testCategoryNode("Plumber", "Tesisatçı", true),
		testCategoryNode("Water Plumber", "Su Tesisatçısı", false),
	}

	if _, err := validateCategoryPath(path, "en"); err == nil {
		t.Fatal("expected a non-selectable final category to be rejected")
	}
}

func TestValidateCategoryPathAllowsSelectableParentWithSelectableChild(t *testing.T) {
	path := []extractor.ExtractedCategoryNode{
		testCategoryNode("Construction and Technical Services", "Construction and Technical Services", false),
		testCategoryNode("Plumber", "Plumber", true),
		testCategoryNode("Water Plumber", "Water Plumber", true),
	}

	if _, err := validateCategoryPath(path, "en"); err != nil {
		t.Fatalf("expected selectable parent and child to remain valid: %v", err)
	}
}

func TestFindOrCreateCategoryNeverDemotesSelectableCategory(t *testing.T) {
	baseCategoryID := uuid.New()
	existing := &Category{ID: uuid.New(), IsSelectable: true, IsActive: true}
	promoted := false

	repo := &fakeRepository{
		findCategoryFn: func(context.Context, uuid.UUID, *uuid.UUID, string, string, []float32) (*Category, error) {
			return existing, nil
		},
		promoteCategorySelectableFn: func(context.Context, uuid.UUID, uuid.UUID) (*Category, error) {
			promoted = true
			return nil, nil
		},
	}
	processor := &GigProcessor{repo: repo}

	got, err := processor.findOrCreateCategory(
		context.Background(), baseCategoryID, nil, 0,
		embeddedCategoryNode{Node: testCategoryNode("Plumber", "Tesisatçı", false)},
		"test-model",
	)
	if err != nil {
		t.Fatalf("findOrCreateCategory() error = %v", err)
	}
	if got != existing {
		t.Fatalf("findOrCreateCategory() returned %#v, want existing category", got)
	}
	if promoted {
		t.Fatal("selectable category was unexpectedly updated for a non-selectable path occurrence")
	}
}

func TestFindOrCreateCategoryPromotesProfession(t *testing.T) {
	baseCategoryID := uuid.New()
	existing := &Category{ID: uuid.New(), IsActive: true}

	repo := &fakeRepository{
		findCategoryFn: func(context.Context, uuid.UUID, *uuid.UUID, string, string, []float32) (*Category, error) {
			return existing, nil
		},
		promoteCategorySelectableFn: func(_ context.Context, categoryID, gotBaseID uuid.UUID) (*Category, error) {
			if categoryID != existing.ID || gotBaseID != baseCategoryID {
				t.Fatalf("unexpected promotion target category=%s base=%s", categoryID, gotBaseID)
			}
			promoted := *existing
			promoted.IsSelectable = true
			return &promoted, nil
		},
	}
	processor := &GigProcessor{repo: repo}

	got, err := processor.findOrCreateCategory(
		context.Background(), baseCategoryID, nil, 0,
		embeddedCategoryNode{Node: testCategoryNode("Plumber", "Tesisatçı", true)},
		"test-model",
	)
	if err != nil {
		t.Fatalf("findOrCreateCategory() error = %v", err)
	}
	if !got.IsSelectable {
		t.Fatal("profession category was not promoted to selectable")
	}
}
