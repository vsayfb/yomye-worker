package extractor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/vsayfb/gig-platform-categorization-service/pkg/metrics"
)

const (
	// A provider gets enough time to complete both extraction calls, but not
	// enough to consume most of the worker's two-minute message deadline.
	defaultProviderAttemptTimeout = 25 * time.Second

	// The entire provider chain is bounded so embedding, database writes, and
	// downstream publication retain meaningful time inside the worker's
	// two-minute message deadline.
	defaultFallbackTotalTimeout = 75 * time.Second
)

type FallbackExtractor struct {
	extractors      []Extractor
	providerTimeout time.Duration
	totalTimeout    time.Duration
}

func NewFallbackExtractor(extractors ...Extractor) *FallbackExtractor {
	return &FallbackExtractor{
		extractors:      extractors,
		providerTimeout: defaultProviderAttemptTimeout,
		totalTimeout:    defaultFallbackTotalTimeout,
	}
}

func (f *FallbackExtractor) Extract(
	ctx context.Context,
	description, baseCategorySlug, catalog string,
) (*ExtractedListing, error) {
	if len(f.extractors) == 0 {
		return nil, fmt.Errorf("no extractors configured")
	}

	totalCtx, cancelTotal := context.WithTimeout(ctx, f.totalTimeout)
	defer cancelTotal()

	var errs []error

	for index, e := range f.extractors {
		attemptCtx, cancelAttempt := context.WithTimeout(totalCtx, f.providerTimeout)
		res, err := e.Extract(attemptCtx, description, baseCategorySlug, catalog)
		cancelAttempt()

		if err == nil {
			return res, nil
		}

		metrics.ObserveAIProviderFailure(ctx, e.Generator(), err)
		errs = append(errs, fmt.Errorf("%s: %w", e.Generator(), err))

		// Caller cancellation is authoritative. Do not fallback after the
		// message itself has been canceled or timed out.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// A per-provider timeout is a provider failure and falls through to the
		// next configured provider. The total fallback deadline is different:
		// once exhausted, stop the chain so the rest of message processing keeps
		// part of the worker's overall time budget.
		if totalCtx.Err() != nil {
			return nil, fmt.Errorf(
				"AI extraction exceeded total fallback deadline %s: %w",
				f.totalTimeout,
				errors.Join(errs...),
			)
		}

		if index+1 < len(f.extractors) {
			metrics.ObserveAIFallback(ctx, e.Generator(), f.extractors[index+1].Generator())
		}
	}

	return nil, fmt.Errorf("all extractors failed: %w", errors.Join(errs...))
}

func (f *FallbackExtractor) ExtractCategoryPath(
	ctx context.Context,
	description, baseCategorySlug, catalog string,
) (*ExtractedListing, error) {
	if len(f.extractors) == 0 {
		return nil, fmt.Errorf("no extractors configured")
	}

	totalCtx, cancelTotal := context.WithTimeout(ctx, f.totalTimeout)
	defer cancelTotal()

	var errs []error

	for index, e := range f.extractors {
		attemptCtx, cancelAttempt := context.WithTimeout(totalCtx, f.providerTimeout)
		res, err := e.ExtractCategoryPath(attemptCtx, description, baseCategorySlug, catalog)
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
				"AI category extraction exceeded total fallback deadline %s: %w",
				f.totalTimeout,
				errors.Join(errs...),
			)
		}

		if index+1 < len(f.extractors) {
			metrics.ObserveAIFallback(ctx, e.Generator(), f.extractors[index+1].Generator())
		}
	}

	return nil, fmt.Errorf("all category extractors failed: %w", errors.Join(errs...))
}

func (f *FallbackExtractor) Classify(ctx context.Context, description string) (string, error) {
	if len(f.extractors) == 0 {
		return "", fmt.Errorf("no extractors configured")
	}

	totalCtx, cancelTotal := context.WithTimeout(ctx, f.totalTimeout)
	defer cancelTotal()

	var errs []error

	for index, e := range f.extractors {
		attemptCtx, cancelAttempt := context.WithTimeout(totalCtx, f.providerTimeout)
		result, err := e.Classify(attemptCtx, description)
		cancelAttempt()

		if err == nil {
			return result, nil
		}

		metrics.ObserveAIProviderFailure(ctx, e.Generator(), err)
		errs = append(errs, fmt.Errorf("%s: %w", e.Generator(), err))

		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if totalCtx.Err() != nil {
			return "", fmt.Errorf(
				"AI classification exceeded total fallback deadline %s: %w",
				f.totalTimeout,
				errors.Join(errs...),
			)
		}

		if index+1 < len(f.extractors) {
			metrics.ObserveAIFallback(ctx, e.Generator(), f.extractors[index+1].Generator())
		}
	}

	return "", fmt.Errorf("all classifiers failed: %w", errors.Join(errs...))
}
