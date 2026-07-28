package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
)

type LocalClient struct {
	client *http.Client
	cfg    *config.Config
}

type OllamaEmbeddingRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type OllamaEmbeddingResponse struct {
	Embedding []float32 `json:"embedding"`
}

func NewLocalClient(cfg *config.Config) *LocalClient {
	return &LocalClient{
		client: &http.Client{Timeout: 2 * time.Second},
		cfg:    cfg,
	}
}

func (c *LocalClient) Embed(ctx context.Context, text string) ([]float32, error) {

	reqBody := OllamaEmbeddingRequest{
		Model:  c.cfg.AI.HuggingFaceAIModel,
		Prompt: text,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	endpoint, err := ollamaEmbeddingsURL(c.cfg.AI.LocalOllamaEndpoint)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))

	if err != nil {
		return nil, fmt.Errorf("create local ollama request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)

	if err != nil {
		return nil, fmt.Errorf("local ollama request failed: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned bad status: %s", resp.Status)
	}

	var result OllamaEmbeddingResponse

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode local embedding response: %w", err)
	}

	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("empty local embedding array returned")
	}

	return result.Embedding, nil
}

func ollamaEmbeddingsURL(endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", fmt.Errorf("local ollama endpoint is empty")
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "http://" + endpoint
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse local ollama endpoint: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("invalid local ollama endpoint %q", endpoint)
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/api/embeddings"
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return parsed.String(), nil
}
