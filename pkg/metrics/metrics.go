package metrics

import (
	"context"
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
	WorkerProcessingDuration metric.Float64Histogram
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
		metric.WithDescription("Total number of processed worker messages."),
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

	return nil
}

func ObserveWorkerProcessing(ctx context.Context, start time.Time, err error) {
	outcome := "success"

	if err != nil {
		outcome = "failure"
	}

	attrs := metric.WithAttributes(attribute.String("outcome", outcome))

	WorkerProcessingDuration.Record(ctx, time.Since(start).Seconds(), attrs)

	WorkerMessagesTotal.Add(ctx, 1, attrs)
}
