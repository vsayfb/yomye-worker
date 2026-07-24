package extractor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
	"github.com/vsayfb/gig-platform-categorization-service/internal/prompter"
)

type GroqExtractor struct {
	cfg *config.Config
}

func NewGroqExtractor(cfg *config.Config) *GroqExtractor {
	return &GroqExtractor{cfg: cfg}
}

func (s *GroqExtractor) Extract(ctx context.Context, description string) (*ExtractedGigDetails, error) {

	slog.InfoContext(ctx,
		"extract request",
		"description", description,
		"len", len(description),
	)

	systemPrompt := prompter.ProfessionPrompt()

	slog.InfoContext(ctx,
		"extract system prompt",
		"len", len(systemPrompt),
	)

	payload := map[string]any{
		"model": s.cfg.AI.Model,
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": systemPrompt,
			},
			{
				"role":    "user",
				"content": description,
			},
		},
		"temperature": 0,
		"response_format": map[string]any{
			"type": "json_object",
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		s.cfg.AI.API_ENDPOINT,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+s.cfg.AI.API_KEY)
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{
		Timeout: 15 * time.Second,
	}).Do(req)

	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)

		slog.InfoContext(ctx, "groq raw http response", "body", string(b))

		slog.ErrorContext(ctx,
			"groq API error",
			"status", resp.StatusCode,
			"body", string(b),
		)

		switch resp.StatusCode {
		case http.StatusTooManyRequests:
			return nil, ErrRateLimited

		case http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout:
			return nil, ErrUnavailable

		default:
			return nil, fmt.Errorf("groq returned %d", resp.StatusCode)
		}
	}

	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		return nil, fmt.Errorf("decode completion: %w", err)
	}

	if len(completion.Choices) == 0 {
		return nil, errors.New("groq returned no choices")
	}

	raw := strings.TrimSpace(completion.Choices[0].Message.Content)

	slog.InfoContext(ctx,
		"groq raw response",
		"content", raw,
	)

	var extracted ExtractedGigDetails

	if err := json.Unmarshal([]byte(raw), &extracted); err != nil {
		return nil, fmt.Errorf(
			"unmarshal extraction: %w\nraw=%s",
			err,
			raw,
		)
	}

	slog.InfoContext(ctx,
		"parsed extraction",
		"profession", extracted.Profession,
		"title", extracted.Title,
		"duration_type", extracted.DurationType,
		"pay_amount", extracted.PayAmount,
		"pay_currency", extracted.PayCurrency,
	)

	if extracted.Title == "" &&
		extracted.Profession == "" &&
		extracted.DurationType == "" &&
		extracted.PayAmount == nil {
		return nil, errors.New("model returned empty extraction")
	}

	return &extracted, nil
}
