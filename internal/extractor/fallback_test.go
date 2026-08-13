package extractor

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vsayfb/gig-platform-categorization-service/internal/runtimepolicy"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type fallbackTestExtractor struct {
	generator             string
	moderateFn            func(context.Context) (*ModerationResult, error)
	extractFn             func(context.Context) (*ExtractedListing, error)
	extractCategoryPathFn func(context.Context) (*ExtractedListing, error)
}

func (f *fallbackTestExtractor) Generator() string { return f.generator }

func (f *fallbackTestExtractor) Moderate(ctx context.Context, _ string) (*ModerationResult, error) {
	if f.moderateFn != nil {
		return f.moderateFn(ctx)
	}
	return &ModerationResult{Decision: ModerationAllow, GeneratedBy: f.generator}, nil
}

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

func TestFallbackExtractor_DefaultBudgetsFitWorkerDeadline(t *testing.T) {
	if runtimepolicy.AIProviderAttemptTimeout != 25*time.Second {
		t.Fatalf("provider timeout = %s, want 25s", runtimepolicy.AIProviderAttemptTimeout)
	}
	if runtimepolicy.AIFallbackTotalTimeout != 75*time.Second {
		t.Fatalf("total fallback timeout = %s, want 75s", runtimepolicy.AIFallbackTotalTimeout)
	}
	if runtimepolicy.AIProviderAttemptTimeout >= runtimepolicy.AIFallbackTotalTimeout {
		t.Fatalf(
			"provider timeout %s must be lower than total fallback timeout %s",
			runtimepolicy.AIProviderAttemptTimeout,
			runtimepolicy.AIFallbackTotalTimeout,
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

func TestFallbackExtractor_EmitsFallbackAndProviderAttemptSpans(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previousProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		_ = provider.Shutdown(context.Background())
	})

	first := &fallbackTestExtractor{
		generator: "first:model",
		extractFn: func(context.Context) (*ExtractedListing, error) {
			return nil, errors.New("provider unavailable")
		},
	}
	second := &fallbackTestExtractor{
		generator: "second:model",
		extractFn: func(context.Context) (*ExtractedListing, error) {
			return &ExtractedListing{GeneratedBy: "second:model"}, nil
		},
	}

	fallback := NewFallbackExtractor(first, second)
	if _, err := fallback.Extract(context.Background(), "description", "gig", "catalog"); err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	spans := recorder.Ended()
	var fallbackSpan sdktrace.ReadOnlySpan
	var attempts []sdktrace.ReadOnlySpan
	for _, span := range spans {
		switch span.Name() {
		case "ai.fallback":
			fallbackSpan = span
		case "ai.provider.attempt":
			attempts = append(attempts, span)
		}
	}

	if fallbackSpan == nil {
		t.Fatal("missing ai.fallback span")
	}
	if len(attempts) != 2 {
		t.Fatalf("provider attempt spans = %d, want 2", len(attempts))
	}
	if got := spanAttribute(fallbackSpan.Attributes(), "ai.selected_generator"); got != "second:model" {
		t.Fatalf("selected generator = %v, want second:model", got)
	}
	for _, attempt := range attempts {
		if attempt.Parent().SpanID() != fallbackSpan.SpanContext().SpanID() {
			t.Fatalf("attempt %q is not a child of fallback span", spanAttribute(attempt.Attributes(), "ai.generator"))
		}
	}
}

func TestFallbackExtractor_ModerationFailureDoesNotBecomeRejection(t *testing.T) {
	provider := &fallbackTestExtractor{
		generator: "limited:model",
		moderateFn: func(context.Context) (*ModerationResult, error) {
			return nil, ErrRateLimited
		},
	}

	result, err := NewFallbackExtractor(provider).Moderate(context.Background(), "content")
	if err == nil {
		t.Fatalf("Moderate() = %#v, want provider failure", result)
	}
	if result != nil {
		t.Fatalf("Moderate() result = %#v, want nil", result)
	}
}

func TestFallbackExtractor_InvalidSuccessfulModerationResponseIsFailure(t *testing.T) {
	provider := &fallbackTestExtractor{
		generator: "broken:model",
		moderateFn: func(context.Context) (*ModerationResult, error) {
			return nil, nil
		},
	}

	result, err := NewFallbackExtractor(provider).Moderate(context.Background(), "content")
	if err == nil || result != nil {
		t.Fatalf("Moderate() = (%#v, %v), want invalid provider response failure", result, err)
	}
}

func TestFallbackExtractor_ModerationFallsBackToExplicitReject(t *testing.T) {
	first := &fallbackTestExtractor{
		generator: "limited:model",
		moderateFn: func(context.Context) (*ModerationResult, error) {
			return nil, ErrRateLimited
		},
	}
	second := &fallbackTestExtractor{
		generator: "healthy:model",
		moderateFn: func(context.Context) (*ModerationResult, error) {
			return &ModerationResult{
				Decision:    ModerationReject,
				Reason:      "controlled_substances",
				GeneratedBy: "healthy:model",
			}, nil
		},
	}

	result, err := NewFallbackExtractor(first, second).Moderate(context.Background(), "content")
	if err != nil {
		t.Fatalf("Moderate() error = %v", err)
	}
	if result.Decision != ModerationReject || result.GeneratedBy != "healthy:model" {
		t.Fatalf("Moderate() = %#v", result)
	}
}

func spanAttribute(attributes []attribute.KeyValue, key attribute.Key) any {
	for _, value := range attributes {
		if value.Key == key {
			return value.Value.AsInterface()
		}
	}
	return nil
}
