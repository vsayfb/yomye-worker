package extractor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/vsayfb/gig-platform-categorization-service/internal/runtimepolicy"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var extractorTracer = otel.Tracer("categorization/extractor")

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
) (result *ExtractedListing, resultErr error) {
	ctx, span := extractorTracer.Start(
		ctx,
		"ai.fallback",
		trace.WithAttributes(
			attribute.String("ai.operation", operation),
			attribute.Int("ai.provider_count", len(f.extractors)),
			attribute.Int64("ai.total_timeout_ms", f.totalTimeout.Milliseconds()),
		),
	)
	defer func() { finishExtractorSpan(span, resultErr) }()

	if len(f.extractors) == 0 {
		return nil, fmt.Errorf("no extractors configured")
	}

	totalCtx, cancelTotal := context.WithTimeout(ctx, f.totalTimeout)
	defer cancelTotal()

	var errs []error

	for index, e := range f.extractors {
		attemptCtx, cancelAttempt := context.WithTimeout(totalCtx, f.providerTimeout)
		attemptCtx, attemptSpan := extractorTracer.Start(
			attemptCtx,
			"ai.provider.attempt",
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("ai.operation", operation),
				attribute.String("ai.generator", e.Generator()),
				attribute.Int("ai.attempt", index+1),
				attribute.Int64("ai.provider_timeout_ms", f.providerTimeout.Milliseconds()),
			),
		)
		res, err := extract(attemptCtx, e)
		cancelAttempt()
		finishExtractorSpan(attemptSpan, err)

		if err == nil {
			span.SetAttributes(
				attribute.String("ai.selected_generator", e.Generator()),
				attribute.Int("ai.attempts", index+1),
			)
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
			nextGenerator := f.extractors[index+1].Generator()
			metrics.ObserveAIFallback(ctx, e.Generator(), nextGenerator)
			span.AddEvent(
				"ai.provider.fallback",
				trace.WithAttributes(
					attribute.String("ai.from_generator", e.Generator()),
					attribute.String("ai.to_generator", nextGenerator),
				),
			)
		}
	}

	return nil, fmt.Errorf("%s: %w", allFailedMessage, errors.Join(errs...))
}

func finishExtractorSpan(span trace.Span, err error) {
	defer span.End()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}
	span.SetStatus(codes.Ok, "completed")
}
