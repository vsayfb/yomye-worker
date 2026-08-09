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
	cfg         *config.AIConfig
	generatedBy string
}

func NewGeminiExtractor(cfg *config.AIConfig) *GeminiExtractor {
	return &GeminiExtractor{
		cfg:         cfg,
		generatedBy: "gemini:" + cfg.Model,
	}
}

func (g *GeminiExtractor) Generator() string {
	return g.generatedBy
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

func (g *GeminiExtractor) Extract(
	ctx context.Context,
	description, baseCategorySlug, catalog string,
) (*ExtractedListing, error) {
	client, err := g.client(ctx)
	if err != nil {
		return nil, err
	}
	listing, err := g.extractCategoryPath(ctx, client, description, baseCategorySlug, catalog)
	if err != nil {
		return nil, err
	}

	rawDetails, err := g.generate(ctx, client, prompter.GigPrompt(), description)
	if err != nil {
		return nil, fmt.Errorf("extract %s details: %w", baseCategorySlug, err)
	}
	details, err := decodeDetails(baseCategorySlug, listing.CategoryPath, rawDetails)
	if err != nil {
		return nil, err
	}
	details.GeneratedBy = listing.GeneratedBy
	details.SourceLocale = listing.SourceLocale

	return details, nil
}

func (g *GeminiExtractor) ExtractCategoryPath(
	ctx context.Context,
	description, baseCategorySlug, catalog string,
) (*ExtractedListing, error) {
	client, err := g.client(ctx)
	if err != nil {
		return nil, err
	}
	return g.extractCategoryPath(ctx, client, description, baseCategorySlug, catalog)
}

func (g *GeminiExtractor) extractCategoryPath(
	ctx context.Context,
	client *genai.Client,
	description, baseCategorySlug, catalog string,
) (*ExtractedListing, error) {
	rawPath, err := g.generate(
		ctx,
		client,
		prompter.CategoryPathPrompt(catalog),
		description,
	)
	if err != nil {
		return nil, fmt.Errorf("extract category path: %w", err)
	}
	sourceLocale, categoryPath, err := decodeCategoryPath(rawPath)
	if err != nil {
		return nil, err
	}

	return &ExtractedListing{
		BaseCategorySlug: baseCategorySlug,
		SourceLocale:     sourceLocale,
		CategoryPath:     categoryPath,
		GeneratedBy:      g.generatedBy,
	}, nil
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
		&genai.GenerateContentConfig{
			MaxOutputTokens:  1024,
			ResponseMIMEType: "application/json",
		},
	)
	if err != nil {
		return "", fmt.Errorf("generate content: %w", err)
	}
	return strings.TrimSpace(resp.Text()), nil
}
