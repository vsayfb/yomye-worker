package extractor

import (
	"context"
	"fmt"
	"strings"

	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
	"github.com/vsayfb/gig-platform-categorization-service/internal/prompter"
	"google.golang.org/genai"
)

type GeminiExtractor struct {
	cfg *config.AIConfig
}

func NewGeminiExtractor(cfg *config.AIConfig) *GeminiExtractor {
	return &GeminiExtractor{
		cfg: cfg,
	}
}

func (g *GeminiExtractor) client(ctx context.Context) (*genai.Client, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  g.cfg.APIKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("creating gemini client: %w", err)
	}
	return client, nil
}

func (g *GeminiExtractor) Classify(ctx context.Context, description string) (string, error) {
	client, err := g.client(ctx)
	if err != nil {
		return "", err
	}
	rawCategory, err := g.generate(ctx, client, prompter.BaseCategoryPrompt(), description)
	if err != nil {
		return "", fmt.Errorf("classify base category: %w", err)
	}
	return decodeBaseCategory(rawCategory)
}

func (g *GeminiExtractor) Extract(
	ctx context.Context,
	description, baseCategorySlug, catalog string,
) (*ExtractedListing, error) {
	client, err := g.client(ctx)
	if err != nil {
		return nil, err
	}

	detailsPrompt := prompter.GigPrompt()
	if baseCategorySlug == "for-sale" {
		detailsPrompt = prompter.SalePrompt()
	}

	rawPath, err := g.generate(
		ctx,
		client,
		prompter.CategoryPathPrompt(baseCategorySlug, catalog),
		description,
	)
	if err != nil {
		return nil, fmt.Errorf("extract category path: %w", err)
	}
	categoryPath, err := decodeCategoryPath(rawPath)
	if err != nil {
		return nil, err
	}

	rawDetails, err := g.generate(ctx, client, detailsPrompt, description)
	if err != nil {
		return nil, fmt.Errorf("extract %s details: %w", baseCategorySlug, err)
	}

	return decodeDetails(baseCategorySlug, categoryPath, rawDetails)
}

func (g *GeminiExtractor) generate(
	ctx context.Context,
	client *genai.Client,
	systemPrompt string,
	description string,
) (string, error) {
	prompt := systemPrompt + "\n\nInput:\n" + description
	resp, err := client.Models.GenerateContent(
		ctx,
		g.cfg.Model,
		genai.Text(prompt),
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("generate content: %w", err)
	}
	return strings.TrimSpace(resp.Text()), nil
}
