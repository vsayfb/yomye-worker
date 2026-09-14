package embeddings

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestOllamaEmbeddingsURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		endpoint string
		want     string
	}{
		{
			name:     "scheme-less endpoint",
			endpoint: "localhost:11434",
			want:     "http://localhost:11434/api/embed",
		},
		{
			name:     "HTTP endpoint",
			endpoint: "http://localhost:11434",
			want:     "http://localhost:11434/api/embed",
		},
		{
			name:     "trailing slash",
			endpoint: "https://ollama.internal/",
			want:     "https://ollama.internal/api/embed",
		},
		{
			name:     "base path",
			endpoint: "http://ollama.internal/v1/",
			want:     "http://ollama.internal/v1/api/embed",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ollamaEmbeddingsURL(tt.endpoint)
			if err != nil {
				t.Fatalf("ollamaEmbeddingsURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ollamaEmbeddingsURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOllamaEmbeddingsURLRejectsInvalidEndpoint(t *testing.T) {
	t.Parallel()

	for _, endpoint := range []string{"", "ftp://localhost:11434"} {
		if _, err := ollamaEmbeddingsURL(endpoint); err == nil {
			t.Fatalf("ollamaEmbeddingsURL(%q) returned no error", endpoint)
		}
	}
}

func TestLocalClientEmbedEmitsDependencySpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previousProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		_ = provider.Shutdown(context.Background())
	})

	cfg := &config.Config{}
	cfg.AI.HuggingFaceAIModel = "all-minilm:l12-v2"
	cfg.AI.LocalOllamaEndpoint = "http://ollama.test:11434"
	client := NewLocalClient(cfg)
	client.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"embeddings":[[0.1,0.2,0.3]]}`)),
			Request:    request,
		}, nil
	})}

	result, err := client.Embed(context.Background(), "Plumbing > Car Wash")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("embedding dimension = %d, want 3", len(result))
	}

	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "ollama.embed" {
		t.Fatalf("ended spans = %#v, want one ollama.embed span", spans)
	}
	if spans[0].SpanKind() != trace.SpanKindClient {
		t.Fatalf("span kind = %v, want client", spans[0].SpanKind())
	}
	if got := embeddingSpanAttribute(spans[0].Attributes(), "embedding.dimension"); got != int64(3) {
		t.Fatalf("embedding.dimension = %v, want 3", got)
	}
	if got := embeddingSpanAttribute(spans[0].Attributes(), "ai.model"); got != "all-minilm:l12-v2" {
		t.Fatalf("ai.model = %v, want all-minilm:l12-v2", got)
	}
}

func embeddingSpanAttribute(attributes []attribute.KeyValue, key attribute.Key) any {
	for _, value := range attributes {
		if value.Key == key {
			return value.Value.AsInterface()
		}
	}
	return nil
}
