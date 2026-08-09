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

type ExtractedCategoryNode struct {
	CanonicalName        string `json:"canonical_name"`
	CanonicalSlug        string `json:"canonical_slug"`
	CanonicalDescription string `json:"canonical_description"`
	LocalizedName        string `json:"localized_name"`
	LocalizedSlug        string `json:"localized_slug"`
	LocalizedDescription string `json:"localized_description"`
	Locale               string `json:"locale"`
	IsSelectable         bool   `json:"is_selectable"`
}

type ExtractedListing struct {
	BaseCategorySlug string                  `json:"base_category_slug"`
	SourceLocale     string                  `json:"source_locale"`
	CategoryPath     []ExtractedCategoryNode `json:"category_path"`
	Gig              *ExtractedGigDetails    `json:"gig"`
	ExpiresAt        *time.Time              `json:"expires_at"`
	GeneratedBy      string                  `json:"-"`
}

type categoryPathResult struct {
	SourceLocale string                  `json:"source_locale"`
	CategoryPath []ExtractedCategoryNode `json:"category_path"`
}

type gigResult struct {
	ExpiresAt *time.Time `json:"expires_at"`
	ExtractedGigDetails
}

func decodeCategoryPath(raw string) (string, []ExtractedCategoryNode, error) {
	var result categoryPathResult
	if err := decodeJSONObject(raw, &result); err != nil {
		return "", nil, fmt.Errorf("decode category path: %w", err)
	}
	if strings.TrimSpace(result.SourceLocale) == "" {
		return "", nil, fmt.Errorf("decode category path: missing source_locale")
	}
	if len(result.CategoryPath) == 0 {
		return "", nil, fmt.Errorf("decode category path: empty category_path")
	}
	for index, node := range result.CategoryPath {
		if strings.TrimSpace(node.CanonicalName) == "" ||
			strings.TrimSpace(node.CanonicalSlug) == "" ||
			strings.TrimSpace(node.CanonicalDescription) == "" ||
			strings.TrimSpace(node.LocalizedName) == "" ||
			strings.TrimSpace(node.LocalizedSlug) == "" ||
			strings.TrimSpace(node.LocalizedDescription) == "" ||
			strings.TrimSpace(node.Locale) == "" {
			return "", nil, fmt.Errorf(
				"decode category path: node %d is missing canonical or localized metadata",
				index,
			)
		}
	}
	if !result.CategoryPath[len(result.CategoryPath)-1].IsSelectable {
		return "", nil, fmt.Errorf("decode category path: final node is not selectable")
	}
	return result.SourceLocale, result.CategoryPath, nil
}

func decodeDetails(
	baseCategorySlug string,
	categoryPath []ExtractedCategoryNode,
	raw string,
) (*ExtractedListing, error) {
	if baseCategorySlug != "gig" {
		return nil, fmt.Errorf("unsupported base category %q", baseCategorySlug)
	}

	listing := &ExtractedListing{
		BaseCategorySlug: baseCategorySlug,
		CategoryPath:     categoryPath,
	}

	var result gigResult
	if err := decodeJSONObject(raw, &result); err != nil {
		return nil, fmt.Errorf("decode gig details: %w", err)
	}
	listing.Gig = &result.ExtractedGigDetails
	listing.ExpiresAt = result.ExpiresAt

	return listing, nil
}

func decodeJSONObject(raw string, dst any) error {
	raw = strings.TrimSpace(raw)
	if err := json.Unmarshal([]byte(raw), dst); err == nil {
		return nil
	}

	start := strings.IndexByte(raw, '{')
	end := strings.LastIndexByte(raw, '}')
	if start < 0 || end < start {
		return fmt.Errorf("response does not contain a JSON object")
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), dst); err != nil {
		return fmt.Errorf("decode JSON object: %w", err)
	}
	return nil
}

var (
	ErrRateLimited = errors.New("extractor: rate limited")
	ErrUnavailable = errors.New("extractor: unavailable")
)

type EmbeddingClient interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type Extractor interface {
	Generator() string
	ExtractCategoryPath(ctx context.Context, description, baseCategorySlug, catalog string) (*ExtractedListing, error)
	Extract(ctx context.Context, description, baseCategorySlug, catalog string) (*ExtractedListing, error)
}
