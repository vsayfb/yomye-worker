package extractor

import (
	"context"
	"encoding/json"
	"strings"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/vsayfb/gig-platform-categorization-service/internal/prompter"
)

type OpenAIExtractor struct {
	client openai.Client
	model  string
}

func NewOpenAIExtractor(
	apiKey string,
	baseURL string,
	model string,
) *OpenAIExtractor {
	client := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(baseURL),
	)

	return &OpenAIExtractor{
		client: client,
		model:  model,
	}
}

func (o *OpenAIExtractor) Extract(
	ctx context.Context,
	description string,
) (*ExtractedGigDetails, error) {

	systemPrompt := prompter.ProfessionPrompt()

	resp, err := o.client.Chat.Completions.New(
		ctx,
		openai.ChatCompletionNewParams{
			Model:       o.model,
			Temperature: openai.Float(0),
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.SystemMessage(systemPrompt),
				openai.UserMessage(description),
			},
		},
	)

	if err != nil {
		return nil, err
	}

	raw := strings.TrimSpace(resp.Choices[0].Message.Content)

	var extracted ExtractedGigDetails

	if err := json.Unmarshal([]byte(raw), &extracted); err != nil {
		return nil, err
	}

	return &extracted, nil
}
