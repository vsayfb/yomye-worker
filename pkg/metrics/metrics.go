package metrics

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("categorization")

	HttpRequestsTotal        metric.Int64Counter
	HttpRequestDuration      metric.Float64Histogram
	WorkerMessagesTotal      metric.Int64Counter
	WorkerFailuresTotal      metric.Int64Counter
	WorkerRetriesTotal       metric.Int64Counter
	WorkerProcessingDuration metric.Float64Histogram
	AIProviderFailuresTotal  metric.Int64Counter
	AIFallbacksTotal         metric.Int64Counter
	CategoryResolutionsTotal metric.Int64Counter
	EmbeddingFailuresTotal   metric.Int64Counter
)

func Register() error {
	var err error

	HttpRequestsTotal, err = meter.Int64Counter(
		"categorization.http.requests_total",
		metric.WithDescription("Total HTTP requests handled, by route, method and status."),
		metric.WithUnit("1"),
	)

	if err != nil {
		return err
	}

	HttpRequestDuration, err = meter.Float64Histogram(
		"categorization.http.request.duration",
		metric.WithDescription("HTTP request latency in seconds."),
		metric.WithUnit("s"),
	)

	if err != nil {
		return err
	}

	WorkerMessagesTotal, err = meter.Int64Counter(
		"categorization.worker.messages_processed_total",
		metric.WithDescription("Total number of worker messages processed successfully."),
		metric.WithUnit("1"),
	)

	if err != nil {
		return err
	}

	WorkerFailuresTotal, err = meter.Int64Counter(
		"categorization.worker.messages_failed_total",
		metric.WithDescription("Total number of worker message processing failures."),
		metric.WithUnit("1"),
	)

	if err != nil {
		return err
	}

	WorkerRetriesTotal, err = meter.Int64Counter(
		"categorization.worker.message_retries_total",
		metric.WithDescription("Total number of transient failures left on SQS for redelivery."),
		metric.WithUnit("1"),
	)

	if err != nil {
		return err
	}

	WorkerProcessingDuration, err = meter.Float64Histogram(
		"categorization.worker.message_processing.duration",
		metric.WithDescription("Time spent processing worker messages."),
		metric.WithUnit("s"),
	)

	if err != nil {
		return err
	}

	AIProviderFailuresTotal, err = meter.Int64Counter(
		"categorization.ai.provider_failures_total",
		metric.WithDescription("Total AI extraction provider failures."),
		metric.WithUnit("1"),
	)

	if err != nil {
		return err
	}

	AIFallbacksTotal, err = meter.Int64Counter(
		"categorization.ai.fallbacks_total",
		metric.WithDescription("Total times AI extraction moved from one provider to the next."),
		metric.WithUnit("1"),
	)

	if err != nil {
		return err
	}

	CategoryResolutionsTotal, err = meter.Int64Counter(
		"categorization.taxonomy.category_resolutions_total",
		metric.WithDescription("Total category resolution decisions, split by created or reused."),
		metric.WithUnit("1"),
	)

	if err != nil {
		return err
	}

	EmbeddingFailuresTotal, err = meter.Int64Counter(
		"categorization.embedding.failures_total",
		metric.WithDescription("Total category embedding failures."),
		metric.WithUnit("1"),
	)

	if err != nil {
		return err
	}

	return nil
}

func ObserveWorkerProcessing(ctx context.Context, start time.Time, messageType string, err error) {
	if WorkerProcessingDuration == nil || WorkerMessagesTotal == nil || WorkerFailuresTotal == nil {
		return
	}

	attrs := metric.WithAttributes(attribute.String("message_type", messageType))
	WorkerProcessingDuration.Record(ctx, time.Since(start).Seconds(), attrs)

	if err == nil {
		WorkerMessagesTotal.Add(ctx, 1, attrs)
		return
	}

	WorkerFailuresTotal.Add(ctx, 1, attrs)
}

func ObserveWorkerRetry(ctx context.Context, messageType string) {
	if WorkerRetriesTotal == nil {
		return
	}

	WorkerRetriesTotal.Add(
		ctx,
		1,
		metric.WithAttributes(attribute.String("message_type", messageType)),
	)
}

func ObserveAIProviderFailure(ctx context.Context, generator string, err error) {
	if AIProviderFailuresTotal == nil {
		return
	}

	reason := "error"
	if errors.Is(err, context.DeadlineExceeded) {
		reason = "timeout"
	} else if errors.Is(err, context.Canceled) {
		reason = "canceled"
	}

	AIProviderFailuresTotal.Add(
		ctx,
		1,
		metric.WithAttributes(
			attribute.String("generator", generator),
			attribute.String("reason", reason),
		),
	)
}

func ObserveAIFallback(ctx context.Context, fromGenerator, toGenerator string) {
	if AIFallbacksTotal == nil {
		return
	}

	AIFallbacksTotal.Add(
		ctx,
		1,
		metric.WithAttributes(
			attribute.String("from_generator", fromGenerator),
			attribute.String("to_generator", toGenerator),
		),
	)
}

func ObserveCategoryResolution(ctx context.Context, workflow, action string) {
	if CategoryResolutionsTotal == nil {
		return
	}

	CategoryResolutionsTotal.Add(
		ctx,
		1,
		metric.WithAttributes(
			attribute.String("workflow", workflow),
			attribute.String("action", action),
		),
	)
}

func ObserveEmbeddingFailure(ctx context.Context, workflow string) {
	if EmbeddingFailuresTotal == nil {
		return
	}

	EmbeddingFailuresTotal.Add(
		ctx,
		1,
		metric.WithAttributes(attribute.String("workflow", workflow)),
	)
}
