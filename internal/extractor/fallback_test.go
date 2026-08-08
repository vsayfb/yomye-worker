package extractor

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type fallbackTestExtractor struct {
	generator             string
	extractFn             func(context.Context) (*ExtractedListing, error)
	extractCategoryPathFn func(context.Context) (*ExtractedListing, error)
	classifyFn            func(context.Context) (string, error)
}

func (f *fallbackTestExtractor) Generator() string { return f.generator }

func (f *fallbackTestExtractor) Extract(
	ctx context.Context,
	_, _, _ string,
) (*ExtractedListing, error) {
	return f.extractFn(ctx)
}

func (f *fallbackTestExtractor) ExtractCategoryPath(
	ctx context.Context,
	_, _, _ string,
) (*ExtractedListing, error) {
	if f.extractCategoryPathFn != nil {
		return f.extractCategoryPathFn(ctx)
	}
	return f.extractFn(ctx)
}

func (f *fallbackTestExtractor) Classify(ctx context.Context, _ string) (string, error) {
	if f.classifyFn == nil {
		return "", errors.New("classify not configured")
	}
	return f.classifyFn(ctx)
}

func TestFallbackExtractor_DefaultBudgetsFitWorkerDeadline(t *testing.T) {
	if defaultProviderAttemptTimeout != 25*time.Second {
		t.Fatalf("provider timeout = %s, want 25s", defaultProviderAttemptTimeout)
	}
	if defaultFallbackTotalTimeout != 75*time.Second {
		t.Fatalf("total fallback timeout = %s, want 75s", defaultFallbackTotalTimeout)
	}
	if defaultProviderAttemptTimeout >= defaultFallbackTotalTimeout {
		t.Fatalf(
			"provider timeout %s must be lower than total fallback timeout %s",
			defaultProviderAttemptTimeout,
			defaultFallbackTotalTimeout,
		)
	}
}

func TestFallbackExtractor_ProviderTimeoutFallsBack(t *testing.T) {
	var secondCalled atomic.Bool

	first := &fallbackTestExtractor{
		generator: "first:model",
		extractFn: func(ctx context.Context) (*ExtractedListing, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	second := &fallbackTestExtractor{
		generator: "second:model",
		extractFn: func(context.Context) (*ExtractedListing, error) {
			secondCalled.Store(true)
			return &ExtractedListing{GeneratedBy: "second:model"}, nil
		},
	}

	fallback := &FallbackExtractor{
		extractors:      []Extractor{first, second},
		providerTimeout: 20 * time.Millisecond,
		totalTimeout:    200 * time.Millisecond,
	}

	result, err := fallback.Extract(context.Background(), "description", "gig", "catalog")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !secondCalled.Load() {
		t.Fatal("expected second provider to be called after first timed out")
	}
	if result == nil || result.GeneratedBy != "second:model" {
		t.Fatalf("unexpected fallback result: %#v", result)
	}
}

func TestFallbackExtractor_TotalTimeoutStopsProviderChain(t *testing.T) {
	var secondCalled atomic.Bool

	first := &fallbackTestExtractor{
		generator: "first:model",
		extractFn: func(ctx context.Context) (*ExtractedListing, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	second := &fallbackTestExtractor{
		generator: "second:model",
		extractFn: func(context.Context) (*ExtractedListing, error) {
			secondCalled.Store(true)
			return &ExtractedListing{}, nil
		},
	}

	fallback := &FallbackExtractor{
		extractors:      []Extractor{first, second},
		providerTimeout: time.Second,
		totalTimeout:    20 * time.Millisecond,
	}

	_, err := fallback.Extract(context.Background(), "description", "gig", "catalog")
	if err == nil {
		t.Fatal("expected total fallback deadline error")
	}
	if secondCalled.Load() {
		t.Fatal("second provider must not run after total fallback deadline is exhausted")
	}
}

func TestFallbackExtractor_CallerCancellationStopsFallback(t *testing.T) {
	var secondCalled atomic.Bool

	first := &fallbackTestExtractor{
		generator: "first:model",
		extractFn: func(ctx context.Context) (*ExtractedListing, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	second := &fallbackTestExtractor{
		generator: "second:model",
		extractFn: func(context.Context) (*ExtractedListing, error) {
			secondCalled.Store(true)
			return &ExtractedListing{}, nil
		},
	}

	fallback := &FallbackExtractor{
		extractors:      []Extractor{first, second},
		providerTimeout: time.Second,
		totalTimeout:    time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fallback.Extract(ctx, "description", "gig", "catalog")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if secondCalled.Load() {
		t.Fatal("second provider must not run after caller cancellation")
	}
}

func TestFallbackExtractor_CategoryPathDoesNotRunFullExtraction(t *testing.T) {
	provider := &fallbackTestExtractor{
		generator: "category-only:model",
		extractFn: func(context.Context) (*ExtractedListing, error) {
			t.Fatal("full listing extraction must not run for category-path-only requests")
			return nil, nil
		},
		extractCategoryPathFn: func(context.Context) (*ExtractedListing, error) {
			return &ExtractedListing{
				BaseCategorySlug: "gig",
				GeneratedBy:      "category-only:model",
			}, nil
		},
	}

	fallback := NewFallbackExtractor(provider)
	result, err := fallback.ExtractCategoryPath(context.Background(), "description", "gig", "catalog")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.GeneratedBy != "category-only:model" {
		t.Fatalf("generated_by = %q, want category-only:model", result.GeneratedBy)
	}
}
