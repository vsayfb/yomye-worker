package processor

import (
	"errors"
	"fmt"
	"testing"

	"github.com/vsayfb/gig-platform-categorization-service/internal/extractor"
)

func validProfessionExtraction() *extractor.ExtractedListing {
	return &extractor.ExtractedListing{
		BaseCategorySlug: BaseCategoryGig,
		SourceLocale:     "en",
		GeneratedBy:      "test-model",
		CategoryPath: []extractor.ExtractedCategoryNode{
			testCategoryNode("Design", "Design", false),
			testCategoryNode("Logo Design", "Logo Design", true),
		},
	}
}

func TestValidateProfessionExtraction_Nil(t *testing.T) {
	if err := validateProfessionExtraction(nil); !errors.Is(err, ErrInvalidProfessionExtraction) {
		t.Fatalf("expected ErrInvalidProfessionExtraction for nil input, got %v", err)
	}
}

func TestValidateProfessionExtraction_Valid(t *testing.T) {
	if err := validateProfessionExtraction(validProfessionExtraction()); err != nil {
		t.Fatalf("expected a valid extraction to pass, got: %v", err)
	}
}

func TestValidateProfessionExtraction_TableDriven(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*extractor.ExtractedListing)
	}{
		{
			name:   "wrong base category",
			mutate: func(e *extractor.ExtractedListing) { e.BaseCategorySlug = "unexpected" },
		},
		{
			name:   "missing generator provenance",
			mutate: func(e *extractor.ExtractedListing) { e.GeneratedBy = "" },
		},
		{
			name:   "empty category path",
			mutate: func(e *extractor.ExtractedListing) { e.CategoryPath = nil },
		},
		{
			name: "category path exceeds max depth",
			mutate: func(e *extractor.ExtractedListing) {
				path := make([]extractor.ExtractedCategoryNode, maxProfessionCategoryPathDepth+1)
				for i := range path {
					path[i] = testCategoryNode(fmt.Sprintf("Level %d", i), fmt.Sprintf("Level %d", i), i == len(path)-1)
				}
				e.CategoryPath = path
			},
		},
		{
			name:   "node missing name",
			mutate: func(e *extractor.ExtractedListing) { e.CategoryPath[0].CanonicalName = "" },
		},
		{
			name:   "node missing description",
			mutate: func(e *extractor.ExtractedListing) { e.CategoryPath[0].CanonicalDescription = "" },
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extracted := validProfessionExtraction()
			tt.mutate(extracted)

			if err := validateProfessionExtraction(extracted); !errors.Is(err, ErrInvalidProfessionExtraction) {
				t.Fatalf("expected ErrInvalidProfessionExtraction, got: %v", err)
			}
		})
	}
}
