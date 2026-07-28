package embeddings

import "testing"

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
			want:     "http://localhost:11434/api/embeddings",
		},
		{
			name:     "HTTP endpoint",
			endpoint: "http://localhost:11434",
			want:     "http://localhost:11434/api/embeddings",
		},
		{
			name:     "trailing slash",
			endpoint: "https://ollama.internal/",
			want:     "https://ollama.internal/api/embeddings",
		},
		{
			name:     "base path",
			endpoint: "http://ollama.internal/v1/",
			want:     "http://ollama.internal/v1/api/embeddings",
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
