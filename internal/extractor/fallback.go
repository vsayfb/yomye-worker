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
) (*ExtractedGigDetails, error) {

	var lastErr error

	for _, e := range f.extractors {
		res, err := e.Extract(ctx, description)

		if err == nil {
			return res, nil
		}

		if errors.Is(err, ErrRateLimited) ||
			errors.Is(err, ErrUnavailable) {

			lastErr = err
			continue
		}

		// Permanent error.
		return nil, err
	}

	if lastErr != nil {
		return nil, lastErr
	}

	return nil, fmt.Errorf("no extractors configured")
}
