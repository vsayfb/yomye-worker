package category

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/gosimple/slug"
	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
	"github.com/vsayfb/gig-platform-categorization-service/internal/extractor"
)

const similarityThreshold = 0.75

var ErrProfessionNotDetected = errors.New("profession not detected")

type EmbeddingClient interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type Service struct {
	repo            Repository
	embeddingClient EmbeddingClient
	fallback        *extractor.FallbackExtractor
	cfg             *config.Config
}

func NewService(repo Repository, embeddingClient EmbeddingClient, fallback *extractor.FallbackExtractor, cfg *config.Config) *Service {
	return &Service{repo: repo, embeddingClient: embeddingClient, fallback: fallback, cfg: cfg}
}

func (s *Service) ProcessGig(
	ctx context.Context,
	posterID uuid.UUID,
	gigID uuid.UUID,
	description string,
) (*Category, error) {

	slog.InfoContext(ctx, "process gig", "description_len", len(description))

	extracted, err := s.fallback.Extract(ctx, description)
	if err != nil {
		return nil, fmt.Errorf("extract gig details: %w", err)
	}

	slog.InfoContext(ctx, "ai extraction", "extracted", extracted)

	// Business defaults.
	if extracted.ExpiresAt == nil {
		t := time.Now().UTC().AddDate(0, 1, 0)
		extracted.ExpiresAt = &t
	}

	if extracted.PayCurrency == "" {
		extracted.PayCurrency = "TRY"
	}

	if extracted.Profession == "" {
		return nil, ErrProfessionNotDetected
	}

	embedding, err := s.embeddingClient.Embed(ctx, extracted.Profession)

	if err != nil {
		return nil, fmt.Errorf("generate embedding: %w", err)
	}

	var category *Category

	err = s.repo.RunInTx(ctx, func(txCtx context.Context) error {

		if err := s.repo.CreateGigDetails(txCtx, &GigDetails{
			GigID:   gigID,
			Details: *extracted,
		}); err != nil {
			return fmt.Errorf("create gig details: %w", err)
		}

		existing, err := s.repo.FindSimilar(txCtx, embedding, similarityThreshold)

		if err != nil {
			return fmt.Errorf("find similar profession: %w", err)
		}

		if existing != nil {
			category = existing
		} else {

			category, err = s.repo.Create(
				txCtx,
				extracted.Profession,
				slug.Make(extracted.Profession),
				embedding,
			)

			if err != nil {
				return fmt.Errorf("create category: %w", err)
			}
		}

		if err := s.repo.CreateGigCategory(txCtx, gigID, category.ID); err != nil {
			return fmt.Errorf("attach category: %w", err)
		}

		if err := s.repo.UpdateGigStatus(txCtx, gigID, "OPEN"); err != nil {
			return fmt.Errorf("open gig: %w", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return category, nil
}
