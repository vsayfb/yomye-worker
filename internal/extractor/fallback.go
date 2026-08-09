package extractor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/vsayfb/gig-platform-categorization-service/internal/runtimepolicy"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/metrics"
)

type FallbackExtractor struct {
	extractors      []Extractor
	providerTimeout time.Duration
	totalTimeout    time.Duration
}

func NewFallbackExtractor(extractors ...Extractor) *FallbackExtractor {
	return &FallbackExtractor{
		extractors:      extractors,
		providerTimeout: runtimepolicy.AIProviderAttemptTimeout,
		totalTimeout:    runtimepolicy.AIFallbackTotalTimeout,
	}
}

func (f *FallbackExtractor) Extract(
	ctx context.Context,
	description, baseCategorySlug, catalog string,
) (*ExtractedListing, error) {
	return f.extractWithFallback(
		ctx,
		"AI extraction",
		"all extractors failed",
		func(attemptCtx context.Context, e Extractor) (*ExtractedListing, error) {
			return e.Extract(attemptCtx, description, baseCategorySlug, catalog)
		},
	)
}

func (f *FallbackExtractor) ExtractCategoryPath(
	ctx context.Context,
	description, baseCategorySlug, catalog string,
) (*ExtractedListing, error) {
	return f.extractWithFallback(
		ctx,
		"AI category extraction",
		"all category extractors failed",
		func(attemptCtx context.Context, e Extractor) (*ExtractedListing, error) {
			return e.ExtractCategoryPath(attemptCtx, description, baseCategorySlug, catalog)
		},
	)
}

func (f *FallbackExtractor) extractWithFallback(
	ctx context.Context,
	operation string,
	allFailedMessage string,
	extract func(context.Context, Extractor) (*ExtractedListing, error),
) (*ExtractedListing, error) {
	if len(f.extractors) == 0 {
		return nil, fmt.Errorf("no extractors configured")
	}

	totalCtx, cancelTotal := context.WithTimeout(ctx, f.totalTimeout)
	defer cancelTotal()

	var errs []error

	for index, e := range f.extractors {
		attemptCtx, cancelAttempt := context.WithTimeout(totalCtx, f.providerTimeout)
		res, err := extract(attemptCtx, e)
		cancelAttempt()

		if err == nil {
			return res, nil
		}

		metrics.ObserveAIProviderFailure(ctx, e.Generator(), err)
		errs = append(errs, fmt.Errorf("%s: %w", e.Generator(), err))

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if totalCtx.Err() != nil {
			return nil, fmt.Errorf(
				"%s exceeded total fallback deadline %s: %w",
				operation,
				f.totalTimeout,
				errors.Join(errs...),
			)
		}

		if index+1 < len(f.extractors) {
			metrics.ObserveAIFallback(ctx, e.Generator(), f.extractors[index+1].Generator())
		}
	}

	return nil, fmt.Errorf("%s: %w", allFailedMessage, errors.Join(errs...))
}
