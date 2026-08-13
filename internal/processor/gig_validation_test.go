package processor

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/vsayfb/gig-platform-categorization-service/internal/extractor"
)

func validGigExtraction() *extractor.ExtractedListing {
	payAmount := 150.0
	return &extractor.ExtractedListing{
		BaseCategorySlug: BaseCategoryGig,
		SourceLocale:     "en",
		GeneratedBy:      "test-model",
		CategoryPath: []extractor.ExtractedCategoryNode{
			testCategoryNode("Design", "Design", false),
			testCategoryNode("Logo Design", "Logo Design", true),
		},
		Gig: &extractor.ExtractedGigDetails{
			Title:        "Logo design",
			Profession:   "placeholder", // overwritten by the leaf category name
			DurationType: "one-time",
			PayAmount:    &payAmount,
			PayCurrency:  "usd",
		},
	}
}

func TestValidateExtraction_NilExtraction(t *testing.T) {
	if err := validateExtraction(nil); !errors.Is(err, ErrInvalidExtraction) {
		t.Fatalf("expected ErrInvalidExtraction for nil extraction, got %v", err)
	}
}

func TestValidateExtraction_ValidGig(t *testing.T) {
	extracted := validGigExtraction()

	if err := validateExtraction(extracted); err != nil {
		t.Fatalf("expected valid gig extraction to pass, got: %v", err)
	}
	if extracted.Gig.Profession != "Logo Design" {
		t.Errorf("expected profession to be normalized to the leaf category name, got %q", extracted.Gig.Profession)
	}
	if extracted.Gig.PayCurrency != "USD" {
		t.Errorf("expected currency to be normalized to uppercase, got %q", extracted.Gig.PayCurrency)
	}
}

func TestValidateExtraction_GigTableDriven(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*extractor.ExtractedListing)
	}{
		{
			name:   "empty category path",
			mutate: func(e *extractor.ExtractedListing) { e.CategoryPath = nil },
		},
		{
			name:   "missing source locale",
			mutate: func(e *extractor.ExtractedListing) { e.SourceLocale = "" },
		},
		{
			name: "node locale differs from source",
			mutate: func(e *extractor.ExtractedListing) {
				e.CategoryPath[len(e.CategoryPath)-1].Locale = "de"
			},
		},
		{
			name: "category path too deep",
			mutate: func(e *extractor.ExtractedListing) {
				path := make([]extractor.ExtractedCategoryNode, maxCategoryPathDepth+1)
				for i := range path {
					path[i] = testCategoryNode(fmt.Sprintf("Level %d", i), fmt.Sprintf("Level %d", i), i == len(path)-1)
				}
				e.CategoryPath = path
			},
		},
		{
			name:   "category node missing description",
			mutate: func(e *extractor.ExtractedListing) { e.CategoryPath[0].CanonicalDescription = "" },
		},
		{
			name: "canonical name exceeds schema limit",
			mutate: func(e *extractor.ExtractedListing) {
				e.CategoryPath[0].CanonicalName = strings.Repeat("a", maxCanonicalFieldLength+1)
			},
		},
		{
			name: "duplicate adjacent categories",
			mutate: func(e *extractor.ExtractedListing) {
				e.CategoryPath = []extractor.ExtractedCategoryNode{
					testCategoryNode("Design", "Design", false),
					testCategoryNode("design", "design", true),
				}
			},
		},
		{
			name:   "unknown base category",
			mutate: func(e *extractor.ExtractedListing) { e.BaseCategorySlug = "unknown" },
		},
		{
			name:   "missing gig details",
			mutate: func(e *extractor.ExtractedListing) { e.Gig = nil },
		},
		{
			name:   "gig missing title",
			mutate: func(e *extractor.ExtractedListing) { e.Gig.Title = "" },
		},
		{
			name: "gig negative pay",
			mutate: func(e *extractor.ExtractedListing) {
				neg := -5.0
				e.Gig.PayAmount = &neg
			},
		},
		{
			name:   "gig invalid currency length",
			mutate: func(e *extractor.ExtractedListing) { e.Gig.PayCurrency = "US" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extracted := validGigExtraction()
			tt.mutate(extracted)

			if err := validateExtraction(extracted); !errors.Is(err, ErrInvalidExtraction) && !errors.Is(err, ErrCategoryNotDetected) {
				t.Fatalf("expected a validation error, got: %v", err)
			}
		})
	}
}
