package extractor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
	"github.com/vsayfb/gig-platform-categorization-service/internal/prompter"
)

type OpenAIExtractor struct {
	client      openai.Client
	model       string
	generatedBy string
}

func NewOpenAIExtractor(cfg *config.AIConfig, provider string) *OpenAIExtractor {
	client := openai.NewClient(
		option.WithAPIKey(cfg.APIKey),
		option.WithBaseURL(cfg.BaseURL),
	)

	return &OpenAIExtractor{
		client:      client,
		model:       cfg.Model,
		generatedBy: provider + ":" + cfg.Model,
	}
}

func (o *OpenAIExtractor) Generator() string {
	return o.generatedBy
}

func (o *OpenAIExtractor) Classify(ctx context.Context, description string) (string, error) {
	rawCategory, err := o.complete(ctx, prompter.BaseCategoryPrompt(), description)
	if err != nil {
		return "", fmt.Errorf("classify base category: %w", err)
	}

	return decodeBaseCategory(rawCategory)
}

func (o *OpenAIExtractor) Extract(
	ctx context.Context,
	description, baseCategorySlug, catalog string,
) (*ExtractedListing, error) {
	listing, err := o.ExtractCategoryPath(ctx, description, baseCategorySlug, catalog)
	if err != nil {
		return nil, err
	}

	detailsPrompt := prompter.GigPrompt()
	if baseCategorySlug == "for-sale" {
		detailsPrompt = prompter.SalePrompt()
	}

	rawDetails, err := o.complete(ctx, detailsPrompt, description)
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

func (o *OpenAIExtractor) ExtractCategoryPath(
	ctx context.Context,
	description, baseCategorySlug, catalog string,
) (*ExtractedListing, error) {
	rawPath, err := o.complete(
		ctx,
		prompter.CategoryPathPrompt(baseCategorySlug, catalog),
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
		GeneratedBy:      o.generatedBy,
	}, nil
}

func (o *OpenAIExtractor) complete(
	ctx context.Context,
	systemPrompt string,
	description string,
) (string, error) {
	jsonObjectFormat := shared.NewResponseFormatJSONObjectParam()

	resp, err := o.client.Chat.Completions.New(
		ctx,
		openai.ChatCompletionNewParams{
			Model:       o.model,
			MaxTokens:   openai.Int(1024),
			Temperature: openai.Float(0),
			ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
				OfJSONObject: &jsonObjectFormat,
			},
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.SystemMessage(systemPrompt),
				openai.UserMessage(description),
			},
		},
	)

	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("AI response contains no choices")
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}
