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
	description string,
) (*ExtractedListing, error) {

	var errs []error

	for _, e := range f.extractors {
		res, err := e.Extract(ctx, description)

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
