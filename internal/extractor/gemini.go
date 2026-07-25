package extractor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
	"github.com/vsayfb/gig-platform-categorization-service/internal/prompter"
	"google.golang.org/genai"
)

type GeminiExtractor struct {
	cfg *config.Config
}

func NewGeminiExtractor(cfg *config.Config) *GeminiExtractor {
	return &GeminiExtractor{
		cfg: cfg,
	}
}

func (g *GeminiExtractor) Extract(
	ctx context.Context,
	description string,
) (*ExtractedGigDetails, error) {

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  g.cfg.AI.Gemini.API_KEY,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("creating gemini client: %w", err)
	}

	prompt := prompter.ProfessionPrompt() + "\n\n" + description

	resp, err := client.Models.GenerateContent(
		ctx,
		g.cfg.AI.Gemini.Model,
		genai.Text(prompt),
		nil, // optional GenerateContentConfig
	)

	if err != nil {
		return nil, fmt.Errorf("generate content: %w", err)
	}

	raw := strings.TrimSpace(resp.Text())

	var extracted ExtractedGigDetails

	if err := json.Unmarshal([]byte(raw), &extracted); err != nil {
		return nil, fmt.Errorf("decode Gemini response: %w", err)
	}

	return &extracted, nil
}
