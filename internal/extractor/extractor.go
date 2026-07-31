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

type ExtractedCategoryNode struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ExtractedListing struct {
	BaseCategorySlug string                  `json:"base_category_slug"`
	CategoryPath     []ExtractedCategoryNode `json:"category_path"`
	Gig              *ExtractedGigDetails    `json:"gig"`
	Sale             *ExtractedSaleDetails   `json:"sale"`
	ExpiresAt        *time.Time              `json:"expires_at"`
}

type baseCategoryResult struct {
	BaseCategorySlug string `json:"base_category_slug"`
}

type categoryPathResult struct {
	CategoryPath []ExtractedCategoryNode `json:"category_path"`
}

type gigResult struct {
	ExpiresAt *time.Time `json:"expires_at"`
	ExtractedGigDetails
}

type saleResult struct {
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

func decodeCategoryPath(raw string) ([]ExtractedCategoryNode, error) {
	var result categoryPathResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &result); err != nil {
		return nil, fmt.Errorf("decode category path: %w", err)
	}
	return result.CategoryPath, nil
}

func decodeDetails(
	baseCategorySlug string,
	categoryPath []ExtractedCategoryNode,
	raw string,
) (*ExtractedListing, error) {
	listing := &ExtractedListing{
		BaseCategorySlug: baseCategorySlug,
		CategoryPath:     categoryPath,
	}

	switch baseCategorySlug {
	case "gig":
		var result gigResult
		if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &result); err != nil {
			return nil, fmt.Errorf("decode gig details: %w", err)
		}
		listing.Gig = &result.ExtractedGigDetails
		listing.ExpiresAt = result.ExpiresAt
	case "for-sale":
		var result saleResult
		if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &result); err != nil {
			return nil, fmt.Errorf("decode sale details: %w", err)
		}
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
	Classify(ctx context.Context, description string) (string, error)
	Extract(ctx context.Context, description, baseCategorySlug, catalog string) (*ExtractedListing, error)
}
