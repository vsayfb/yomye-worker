package worker

import (
	"errors"
	"testing"

	"github.com/vsayfb/gig-platform-categorization-service/internal/extractor"
)

func TestValidateExtractionAcceptsSaleShapes(t *testing.T) {
	t.Parallel()

	currency := "try"
	amount := 7500.0
	condition := "good"

	tests := []struct {
		name string
		sale *extractor.ExtractedSaleDetails
	}{
		{
			name: "fixed price",
			sale: &extractor.ExtractedSaleDetails{
				Title:         "Bisiklet",
				PriceAmount:   &amount,
				PriceCurrency: &currency,
				PricingType:   "fixed",
				Condition:     &condition,
			},
		},
		{
			name: "contact",
			sale: &extractor.ExtractedSaleDetails{
				Title:       "Bisiklet",
				PricingType: "contact",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := &extractor.ExtractedListing{
				BaseCategorySlug: BaseCategoryForSale,
				Category:         "Bisiklet",
				Sale:             tt.sale,
			}
			if err := validateExtraction(got); err != nil {
				t.Fatalf("validateExtraction() error = %v", err)
			}
		})
	}
}

func TestValidateExtractionRejectsInvalidSalePriceShapes(t *testing.T) {
	t.Parallel()

	currency := "TRY"
	amount := 100.0

	tests := []struct {
		name string
		sale *extractor.ExtractedSaleDetails
	}{
		{
			name: "fixed without amount",
			sale: &extractor.ExtractedSaleDetails{
				Title:         "Bisiklet",
				PriceCurrency: &currency,
				PricingType:   "fixed",
			},
		},
		{
			name: "free with amount",
			sale: &extractor.ExtractedSaleDetails{
				Title:       "Bisiklet",
				PriceAmount: &amount,
				PricingType: "free",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := &extractor.ExtractedListing{
				BaseCategorySlug: BaseCategoryForSale,
				Category:         "Bisiklet",
				Sale:             tt.sale,
			}
			err := validateExtraction(got)
			if !errors.Is(err, ErrInvalidExtraction) {
				t.Fatalf("validateExtraction() error = %v, want ErrInvalidExtraction", err)
			}
		})
	}
}

func TestValidateExtractionRejectsMismatchedDetails(t *testing.T) {
	t.Parallel()

	got := &extractor.ExtractedListing{
		BaseCategorySlug: BaseCategoryGig,
		Category:         "Elektrikçi",
		Sale: &extractor.ExtractedSaleDetails{
			Title:       "Kablo",
			PricingType: "contact",
		},
	}

	err := validateExtraction(got)
	if !errors.Is(err, ErrInvalidExtraction) {
		t.Fatalf("validateExtraction() error = %v, want ErrInvalidExtraction", err)
	}
}

func TestValidateExtractionNormalizesGigDefaults(t *testing.T) {
	t.Parallel()

	got := &extractor.ExtractedListing{
		BaseCategorySlug: BaseCategoryGig,
		Category:         "Elektrikçi",
		Gig: &extractor.ExtractedGigDetails{
			Title:      "Elektrikçi Aranıyor",
			Profession: "Elektrikçi",
		},
	}

	if err := validateExtraction(got); err != nil {
		t.Fatalf("validateExtraction() error = %v", err)
	}
	if got.Gig.PayCurrency != "TRY" {
		t.Fatalf("PayCurrency = %q, want TRY", got.Gig.PayCurrency)
	}
}
