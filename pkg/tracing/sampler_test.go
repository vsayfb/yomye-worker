package tracing

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var testTraceID = trace.TraceID{1}
var testSpanID = trace.SpanID{1}

func TestSamplerForEnvironment_DevelopmentSamplesRoot(t *testing.T) {
	result := SamplerForEnvironment("development").ShouldSample(sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		TraceID:       testTraceID,
		Name:          "test",
	})
	if result.Decision != sdktrace.RecordAndSample {
		t.Fatalf("decision = %v, want RecordAndSample", result.Decision)
	}
}

func TestSamplerForEnvironment_StagingHonorsSampledParent(t *testing.T) {
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    testTraceID,
		SpanID:     testSpanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), parent)

	result := SamplerForEnvironment("staging").ShouldSample(sdktrace.SamplingParameters{
		ParentContext: ctx,
		TraceID:       testTraceID,
		Name:          "test",
	})
	if result.Decision != sdktrace.RecordAndSample {
		t.Fatalf("decision = %v, want RecordAndSample", result.Decision)
	}
}

func TestSamplerForEnvironment_StagingHonorsUnsampledParent(t *testing.T) {
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: testTraceID,
		SpanID:  testSpanID,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), parent)

	result := SamplerForEnvironment("staging").ShouldSample(sdktrace.SamplingParameters{
		ParentContext: ctx,
		TraceID:       testTraceID,
		Name:          "test",
	})
	if result.Decision != sdktrace.Drop {
		t.Fatalf("decision = %v, want Drop", result.Decision)
	}
}
