package tracing

import sdktrace "go.opentelemetry.io/otel/sdk/trace"

const stagingTraceSampleRatio = 0.10

func SamplerForEnvironment(environment string) sdktrace.Sampler {
	if environment == "development" {
		return sdktrace.AlwaysSample()
	}

	return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(stagingTraceSampleRatio))
}
