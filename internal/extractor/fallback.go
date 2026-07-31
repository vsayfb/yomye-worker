package extractor

import (
	"context"
	"errors"
	"fmt"
)

type FallbackExtractor struct {
	extractors []Extractor
}

func NewFallbackExtractor(extractors ...Extractor) *FallbackExtractor {
	return &FallbackExtractor{
		extractors: extractors,
	}
}

func (f *FallbackExtractor) Extract(
	ctx context.Context,
	description, baseCategorySlug, catalog string,
) (*ExtractedListing, error) {

	var errs []error

	for _, e := range f.extractors {
		res, err := e.Extract(ctx, description, baseCategorySlug, catalog)

		if err == nil {
			return res, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("all extractors failed: %w", errors.Join(errs...))
	}

	return nil, fmt.Errorf("no extractors configured")
}

func (f *FallbackExtractor) Classify(ctx context.Context, description string) (string, error) {
	var errs []error

	for _, e := range f.extractors {
		result, err := e.Classify(ctx, description)
		if err == nil {
			return result, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return "", fmt.Errorf("all classifiers failed: %w", errors.Join(errs...))
	}
	return "", fmt.Errorf("no extractors configured")
}
