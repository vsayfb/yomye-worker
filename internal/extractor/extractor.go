package extractor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type ExtractedGigDetails struct {
	Title        string   `json:"title"`
	Profession   string   `json:"profession"`
	DurationType string   `json:"duration_type"`
	PayAmount    *float64 `json:"pay_amount"`
	PayCurrency  string   `json:"pay_currency"`
}

type ExtractedSaleDetails struct {
	Title         string   `json:"title"`
	PriceAmount   *float64 `json:"price_amount"`
	PriceCurrency *string  `json:"price_currency"`
	PricingType   string   `json:"pricing_type"`
	Condition     *string  `json:"condition"`
	Brand         *string  `json:"brand"`
	Model         *string  `json:"model"`
}

type ExtractedListing struct {
	BaseCategorySlug string                `json:"base_category_slug"`
	Category         string                `json:"category"`
	Gig              *ExtractedGigDetails  `json:"gig"`
	Sale             *ExtractedSaleDetails `json:"sale"`
	ExpiresAt        *time.Time            `json:"expires_at"`
}

type baseCategoryResult struct {
	BaseCategorySlug string `json:"base_category_slug"`
}

type gigResult struct {
	Category  string     `json:"category"`
	ExpiresAt *time.Time `json:"expires_at"`
	ExtractedGigDetails
}

type saleResult struct {
	Category  string     `json:"category"`
	ExpiresAt *time.Time `json:"expires_at"`
	ExtractedSaleDetails
}

func decodeBaseCategory(raw string) (string, error) {
	var result baseCategoryResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &result); err != nil {
		return "", fmt.Errorf("decode base category: %w", err)
	}
	switch result.BaseCategorySlug {
	case "gig", "for-sale":
		return result.BaseCategorySlug, nil
	default:
		return "", fmt.Errorf("unsupported base category %q", result.BaseCategorySlug)
	}
}

func decodeDetails(baseCategorySlug, raw string) (*ExtractedListing, error) {
	listing := &ExtractedListing{BaseCategorySlug: baseCategorySlug}

	switch baseCategorySlug {
	case "gig":
		var result gigResult
		if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &result); err != nil {
			return nil, fmt.Errorf("decode gig details: %w", err)
		}
		listing.Category = result.Category
		listing.Gig = &result.ExtractedGigDetails
		listing.ExpiresAt = result.ExpiresAt
	case "for-sale":
		var result saleResult
		if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &result); err != nil {
			return nil, fmt.Errorf("decode sale details: %w", err)
		}
		listing.Category = result.Category
		listing.Sale = &result.ExtractedSaleDetails
		listing.ExpiresAt = result.ExpiresAt
	default:
		return nil, fmt.Errorf("unsupported base category %q", baseCategorySlug)
	}

	return listing, nil
}

var (
	ErrRateLimited = errors.New("extractor: rate limited")
	ErrUnavailable = errors.New("extractor: unavailable")
)

type EmbeddingClient interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type Extractor interface {
	Extract(ctx context.Context, description string) (*ExtractedListing, error)
}
