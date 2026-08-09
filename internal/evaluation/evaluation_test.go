package evaluation

import (
	"testing"

	"github.com/vsayfb/gig-platform-categorization-service/internal/extractor"
)

func TestCompareExactCategoryBehavior(t *testing.T) {
	expected := Case{
		Description:  "Musluğum bozuldu.",
		SourceLocale: "tr",
		CategoryPath: []CategoryNode{
			{CanonicalSlug: "home-services", IsSelectable: false},
			{CanonicalSlug: "plumbing", IsSelectable: false},
			{CanonicalSlug: "plumber", IsSelectable: true},
		},
	}
	actual := &extractor.ExtractedListing{
		SourceLocale: "tr",
		GeneratedBy:  "another-provider:model",
		CategoryPath: []extractor.ExtractedCategoryNode{
			{CanonicalSlug: "home-services", CanonicalDescription: "changed wording", IsSelectable: false},
			{CanonicalSlug: "plumbing", CanonicalDescription: "different wording", IsSelectable: false},
			{CanonicalSlug: "plumber", CanonicalDescription: "also different", IsSelectable: true},
		},
	}

	comparison, err := Compare(expected, actual)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	if !comparison.Exact {
		t.Fatalf("comparison = %#v, want exact", comparison)
	}
}

func TestCompareReportsLeafMatchWhenParentPathDrifts(t *testing.T) {
	expected := Case{
		Description:  "Musluğum bozuldu.",
		SourceLocale: "tr",
		CategoryPath: []CategoryNode{
			{CanonicalSlug: "home-services"},
			{CanonicalSlug: "plumber", IsSelectable: true},
		},
	}
	actual := &extractor.ExtractedListing{
		SourceLocale: "tr",
		CategoryPath: []extractor.ExtractedCategoryNode{
			{CanonicalSlug: "repair-services"},
			{CanonicalSlug: "plumber", IsSelectable: true},
		},
	}

	comparison, err := Compare(expected, actual)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	if comparison.Exact || comparison.PathMatch || !comparison.LeafMatch {
		t.Fatalf("comparison = %#v, want non-exact path with matching leaf", comparison)
	}
}

func TestBaselineValidateRejectsDuplicateDescriptions(t *testing.T) {
	baseline := Baseline{
		Version: BaselineVersion,
		Cases: []Case{
			{Description: "same", SourceLocale: "tr", CategoryPath: []CategoryNode{{CanonicalSlug: "a"}}},
			{Description: "same", SourceLocale: "tr", CategoryPath: []CategoryNode{{CanonicalSlug: "b"}}},
		},
	}

	if err := baseline.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want duplicate-description error")
	}
}
