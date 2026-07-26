package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gosimple/slug"
	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
	"github.com/vsayfb/gig-platform-categorization-service/internal/extractor"
)

const similarityThreshold = 0.75

const (
	BaseCategoryGig     = "gig"
	BaseCategoryForSale = "for-sale"
)

var (
	ErrCategoryNotDetected = errors.New("category not detected")
	ErrInvalidExtraction   = errors.New("invalid listing extraction")
)

type EmbeddingClient interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type ProcessResult struct {
	Category         *Category
	BaseCategorySlug string
	Title            string
	AlreadyOpen      bool
}

type Service struct {
	repo            Repository
	embeddingClient EmbeddingClient
	fallback        *extractor.FallbackExtractor
	cfg             *config.Config
}

func NewService(
	repo Repository,
	embeddingClient EmbeddingClient,
	fallback *extractor.FallbackExtractor,
	cfg *config.Config,
) *Service {
	return &Service{
		repo:            repo,
		embeddingClient: embeddingClient,
		fallback:        fallback,
		cfg:             cfg,
	}
}

func (s *Service) ProcessListing(
	ctx context.Context,
	listingID uuid.UUID,
	description string,
) (*ProcessResult, error) {
	slog.InfoContext(ctx, "process listing", "listing_id", listingID, "description_len", len(description))

	extracted, err := s.fallback.Extract(ctx, description)

	if err != nil {
		return nil, fmt.Errorf("extract listing details: %w", err)
	}

	if err := validateExtraction(extracted); err != nil {
		return nil, err
	}

	embedding, err := s.embeddingClient.Embed(ctx, extracted.Category)
	if err != nil {
		return nil, fmt.Errorf("generate category embedding: %w", err)
	}

	result := &ProcessResult{
		BaseCategorySlug: extracted.BaseCategorySlug,
		Title:            extractedTitle(extracted),
	}

	err = s.repo.RunInTx(ctx, func(txCtx context.Context) error {
		state, err := s.repo.LockListing(txCtx, listingID)
		if err != nil {
			return fmt.Errorf("lock listing: %w", err)
		}

		if state.Status == "OPEN" {
			result.Category = state.Category
			result.BaseCategorySlug = state.BaseCategorySlug
			result.Title = state.Title
			result.AlreadyOpen = true
			return nil
		}
		if state.Status != "PROCESSING" {
			return fmt.Errorf("listing has non-processable status %q", state.Status)
		}

		baseCategoryID, err := s.repo.ResolveBaseCategory(txCtx, extracted.BaseCategorySlug)
		if err != nil {
			return fmt.Errorf("resolve base category %q: %w", extracted.BaseCategorySlug, err)
		}

		if err := s.repo.ClassifyListing(
			txCtx,
			listingID,
			baseCategoryID,
			strings.TrimSpace(description),
		); err != nil {
			return fmt.Errorf("classify listing: %w", err)
		}

		category, err := s.repo.FindSimilar(txCtx, baseCategoryID, embedding, similarityThreshold)
		if err != nil {
			return fmt.Errorf("find similar category: %w", err)
		}
		if category == nil {
			category, err = s.repo.CreateCategory(
				txCtx,
				baseCategoryID,
				extracted.Category,
				slug.Make(extracted.Category),
				embedding,
			)
			if err != nil {
				return fmt.Errorf("create category: %w", err)
			}
		}
		result.Category = category

		if err := s.repo.AttachCategory(txCtx, listingID, category.ID, baseCategoryID); err != nil {
			return fmt.Errorf("attach category: %w", err)
		}

		switch extracted.BaseCategorySlug {
		case BaseCategoryGig:
			expiresAt := extracted.ExpiresAt
			if expiresAt == nil {
				t := time.Now().UTC().AddDate(0, 1, 0)
				expiresAt = &t
			}
			if err := s.repo.UpsertGigDetails(txCtx, listingID, extracted.Gig, expiresAt); err != nil {
				return fmt.Errorf("persist gig details: %w", err)
			}
		case BaseCategoryForSale:
			if err := s.repo.UpsertSaleDetails(txCtx, listingID, extracted.Sale, extracted.ExpiresAt); err != nil {
				return fmt.Errorf("persist sale details: %w", err)
			}
		}

		if err := s.repo.OpenListing(txCtx, listingID); err != nil {
			return fmt.Errorf("open listing: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func validateExtraction(e *extractor.ExtractedListing) error {
	if e == nil {
		return fmt.Errorf("%w: empty response", ErrInvalidExtraction)
	}
	e.BaseCategorySlug = strings.TrimSpace(e.BaseCategorySlug)
	e.Category = strings.TrimSpace(e.Category)
	if e.Category == "" {
		return ErrCategoryNotDetected
	}

	switch e.BaseCategorySlug {
	case BaseCategoryGig:
		if e.Gig == nil || e.Sale != nil {
			return fmt.Errorf("%w: gig details shape", ErrInvalidExtraction)
		}
		e.Gig.Title = strings.TrimSpace(e.Gig.Title)
		e.Gig.Profession = strings.TrimSpace(e.Gig.Profession)
		e.Gig.PayCurrency = strings.ToUpper(strings.TrimSpace(e.Gig.PayCurrency))
		if e.Gig.Title == "" || e.Gig.Profession == "" {
			return fmt.Errorf("%w: missing gig title or profession", ErrInvalidExtraction)
		}
		if e.Gig.PayCurrency == "" {
			e.Gig.PayCurrency = "TRY"
		}
		if e.Gig.PayAmount != nil && *e.Gig.PayAmount < 0 {
			return fmt.Errorf("%w: negative gig pay", ErrInvalidExtraction)
		}
	case BaseCategoryForSale:
		if e.Sale == nil || e.Gig != nil {
			return fmt.Errorf("%w: sale details shape", ErrInvalidExtraction)
		}
		normalizeSaleStrings(e.Sale)
		if e.Sale.Title == "" {
			return fmt.Errorf("%w: missing sale title", ErrInvalidExtraction)
		}
		switch e.Sale.PricingType {
		case "fixed", "negotiable":
			if e.Sale.PriceAmount == nil || *e.Sale.PriceAmount <= 0 ||
				e.Sale.PriceCurrency == nil || *e.Sale.PriceCurrency == "" {
				return fmt.Errorf("%w: priced sale requires positive amount and currency", ErrInvalidExtraction)
			}
		case "free", "contact":
			if e.Sale.PriceAmount != nil || e.Sale.PriceCurrency != nil {
				return fmt.Errorf("%w: free/contact sale cannot contain price", ErrInvalidExtraction)
			}
		default:
			return fmt.Errorf("%w: unknown pricing type %q", ErrInvalidExtraction, e.Sale.PricingType)
		}
		if e.Sale.Condition != nil {
			switch *e.Sale.Condition {
			case "new", "like_new", "good", "fair", "poor":
			default:
				return fmt.Errorf("%w: unknown sale condition %q", ErrInvalidExtraction, *e.Sale.Condition)
			}
		}
	default:
		return fmt.Errorf("%w: unknown base category %q", ErrInvalidExtraction, e.BaseCategorySlug)
	}

	return nil
}

func normalizeSaleStrings(s *extractor.ExtractedSaleDetails) {
	s.Title = strings.TrimSpace(s.Title)
	s.PricingType = strings.ToLower(strings.TrimSpace(s.PricingType))
	trimOptional := func(value **string, upper bool) {
		if *value == nil {
			return
		}
		v := strings.TrimSpace(**value)
		if upper {
			v = strings.ToUpper(v)
		}
		if v == "" {
			*value = nil
			return
		}
		*value = &v
	}
	trimOptional(&s.PriceCurrency, true)
	trimOptional(&s.Condition, false)
	trimOptional(&s.Brand, false)
	trimOptional(&s.Model, false)
}

func extractedTitle(e *extractor.ExtractedListing) string {
	if e.Gig != nil {
		return e.Gig.Title
	}
	if e.Sale != nil {
		return e.Sale.Title
	}
	return ""
}
