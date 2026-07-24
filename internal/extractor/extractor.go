package extractor

import (
	"context"
	"errors"
	"time"
)

type ExtractedGigDetails struct {
	Title        string     `json:"title"`
	Profession   string     `json:"profession"`
	DurationType string     `json:"duration_type"`
	PayAmount    *float64   `json:"pay_amount"`
	PayCurrency  string     `json:"pay_currency"`
	ExpiresAt    *time.Time `json:"expires_at"`
}

var (
	ErrRateLimited = errors.New("extractor: rate limited")
	ErrUnavailable = errors.New("extractor: unavailable")
)


type EmbeddingClient interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type Extractor interface {
	Extract(ctx context.Context, description string) (*ExtractedGigDetails, error)
}
