package processor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
	"github.com/vsayfb/gig-platform-categorization-service/internal/extractor"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const maxProfessionCategoryPathDepth = 4

var ErrInvalidProfessionExtraction = errors.New("invalid profession extraction")

type ProfessionProcessor struct {
	repo            Repository
	embeddingClient EmbeddingClient
	fallback        *extractor.FallbackExtractor
	cfg             *config.Config
}

func NewProfessionProcessor(
	repo Repository,
	embeddingClient EmbeddingClient,
	fallback *extractor.FallbackExtractor,
	cfg *config.Config,
) *ProfessionProcessor {
	return &ProfessionProcessor{
		repo:            repo,
		embeddingClient: embeddingClient,
		fallback:        fallback,
		cfg:             cfg,
	}
}

type NewProfession struct {
	RequestID   uuid.UUID `json:"request_id"`
	AuthorID    uuid.UUID `json:"author_id"`
	Description string    `json:"description"`
}

type embeddedProfessionCategoryNode struct {
	node      extractor.ExtractedCategoryNode
	embedding []float32
}

func (s *ProfessionProcessor) Process(ctx context.Context, record Message) (err error) {
	ctx, span := tracer.Start(ctx, "process-profession")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	span.SetAttributes(
		attribute.String("message.id", record.ID),
		attribute.String("message.type", record.Type),
	)

	var msg NewProfession
	if err := json.Unmarshal([]byte(record.Body), &msg); err != nil {
		return fmt.Errorf("decode new-profession message: %w", err)
	}

	msg.Description = strings.TrimSpace(msg.Description)
	if msg.RequestID == uuid.Nil {
		return errors.New("invalid new-profession message: missing request_id")
	}
	if msg.AuthorID == uuid.Nil {
		return errors.New("invalid new-profession message: missing author_id")
	}
	if msg.Description == "" {
		return errors.New("invalid new-profession message: empty description")
	}

	span.SetAttributes(
		attribute.String("profession.request_id", msg.RequestID.String()),
		attribute.String("profession.author_id", msg.AuthorID.String()),
	)

	// Avoid extraction and embedding work for events that are already stale.
	currentRequestID, userExists, err := s.repo.GetCurrentProfessionRequest(ctx, msg.AuthorID)
	if err != nil {
		return fmt.Errorf("get current profession request: %w", err)
	}
	if !userExists || currentRequestID != msg.RequestID {
		logIgnoredProfessionEvent(ctx, msg, currentRequestID, userExists, "initial-check")
		span.SetStatus(codes.Ok, "stale profession event ignored")
		return nil
	}

	slog.InfoContext(
		ctx,
		"process profession",
		"request_id", msg.RequestID,
		"author_id", msg.AuthorID,
		"description_len", len(msg.Description),
	)

	baseCategoryID, err := s.repo.ResolveBaseCategory(ctx, BaseCategoryGig)
	if err != nil {
		return fmt.Errorf("resolve base category %q: %w", BaseCategoryGig, err)
	}

	activeCategories, err := s.repo.ListActiveCategories(ctx, baseCategoryID)
	if err != nil {
		return fmt.Errorf("list active gig categories: %w", err)
	}

	extracted, err := s.fallback.Extract(
		ctx,
		msg.Description,
		BaseCategoryGig,
		formatCategoryCatalog(activeCategories),
	)
	if err != nil {
		return fmt.Errorf("extract profession category path: %w", err)
	}

	if err := validateProfessionExtraction(extracted); err != nil {
		return err
	}

	embeddedPath := make([]embeddedProfessionCategoryNode, 0, len(extracted.CategoryPath))
	for index, node := range extracted.CategoryPath {
		embedding, err := s.embeddingClient.Embed(
			ctx,
			categoryEmbeddingText(extracted.CategoryPath, index),
		)
		if err != nil {
			return fmt.Errorf("generate profession category embedding for %q: %w", node.CanonicalName, err)
		}
		if len(embedding) == 0 {
			return fmt.Errorf("generate profession category embedding for %q: empty embedding", node.CanonicalName)
		}

		embeddedPath = append(embeddedPath, embeddedProfessionCategoryNode{
			node:      node,
			embedding: embedding,
		})
	}

	var professionCategory *Category
	stale := false

	if err := s.repo.RunInTx(ctx, func(txCtx context.Context) error {
		// This is the authoritative stale-event check. The row lock prevents the
		// request ID from changing while the category path and association commit.
		lockedRequestID, exists, err := s.repo.LockCurrentProfessionRequest(txCtx, msg.AuthorID)
		if err != nil {
			return fmt.Errorf("lock current profession request: %w", err)
		}
		if !exists || lockedRequestID != msg.RequestID {
			stale = true
			logIgnoredProfessionEvent(txCtx, msg, lockedRequestID, exists, "locked-check")
			return nil
		}

		var parentID *uuid.UUID

		for depth, embedded := range embeddedPath {
			categorySlug := embedded.node.CanonicalSlug
			if categorySlug == "" {
				return fmt.Errorf(
					"%w: category path node %d has an unusable name %q",
					ErrInvalidProfessionExtraction,
					depth,
					embedded.node.CanonicalName,
				)
			}

			category, err := s.repo.FindCategory(
				txCtx,
				baseCategoryID,
				parentID,
				embedded.node.CanonicalName,
				categorySlug,
				embedded.embedding,
			)
			if err != nil {
				return fmt.Errorf("find profession category at depth %d: %w", depth, err)
			}

			if category == nil {
				category, err = s.repo.CreateCategory(
					txCtx,
					baseCategoryID,
					parentID,
					embedded.node.CanonicalName,
					categorySlug,
					embedded.node.CanonicalDescription,
					extracted.GeneratedBy,
					embedded.node.IsSelectable,
					int16(depth),
					embedded.embedding,
				)
				if err != nil {
					return fmt.Errorf("create profession category at depth %d: %w", depth, err)
				}
			} else if embedded.node.IsSelectable && !category.IsSelectable {
				category, err = s.repo.PromoteCategorySelectable(
					txCtx,
					category.ID,
					baseCategoryID,
				)
				if err != nil {
					return fmt.Errorf("promote profession category at depth %d: %w", depth, err)
				}
			}

			if category == nil || category.ID == uuid.Nil {
				return fmt.Errorf(
					"persist profession category at depth %d: repository returned an invalid category",
					depth,
				)
			}

			if err := s.repo.EnsureCategoryLocalization(txCtx, CategoryLocalization{
				CategoryID:  category.ID,
				Locale:      embedded.node.Locale,
				Name:        embedded.node.LocalizedName,
				Slug:        embedded.node.LocalizedSlug,
				Description: embedded.node.LocalizedDescription,
				GeneratedBy: extracted.GeneratedBy,
			}); err != nil {
				return fmt.Errorf("ensure profession localization at depth %d: %w", depth, err)
			}

			professionCategory = category
			categoryID := category.ID
			parentID = &categoryID
		}

		if professionCategory == nil {
			return errors.New("profession category was not resolved")
		}

		// Only the final canonical UUID is attached. The database validates that
		// it is active, selectable, and belongs to the active gig base category.
		if err := s.repo.UpsertUserProfessionCategory(
			txCtx,
			msg.AuthorID,
			professionCategory.ID,
		); err != nil {
			return fmt.Errorf("upsert user profession category: %w", err)
		}

		return nil
	}); err != nil {
		return err
	}

	if stale {
		span.SetStatus(codes.Ok, "stale profession event ignored")
		return nil
	}

	span.SetAttributes(
		attribute.String("profession.category_id", professionCategory.ID.String()),
		attribute.String("profession.category_name", professionCategory.CanonicalName),
	)
	span.SetStatus(codes.Ok, "profession processed successfully")

	slog.InfoContext(
		ctx,
		"profession categorized",
		"request_id", msg.RequestID,
		"author_id", msg.AuthorID,
		"category_id", professionCategory.ID,
		"category", professionCategory.CanonicalName,
	)

	return nil
}

func logIgnoredProfessionEvent(
	ctx context.Context,
	msg NewProfession,
	currentRequestID uuid.UUID,
	userExists bool,
	check string,
) {
	slog.InfoContext(
		ctx,
		"ignore stale profession event",
		"check", check,
		"author_id", msg.AuthorID,
		"event_request_id", msg.RequestID,
		"current_request_id", currentRequestID,
		"user_exists", userExists,
	)
}

func validateProfessionExtraction(extracted *extractor.ExtractedListing) error {
	if extracted == nil {
		return fmt.Errorf("%w: empty response", ErrInvalidProfessionExtraction)
	}

	extracted.BaseCategorySlug = strings.ToLower(strings.TrimSpace(extracted.BaseCategorySlug))
	extracted.SourceLocale = normalizeLocale(extracted.SourceLocale)
	if extracted.BaseCategorySlug != BaseCategoryGig {
		return fmt.Errorf(
			"%w: expected base category %q, got %q",
			ErrInvalidProfessionExtraction,
			BaseCategoryGig,
			extracted.BaseCategorySlug,
		)
	}

	extracted.GeneratedBy = strings.TrimSpace(extracted.GeneratedBy)
	if extracted.GeneratedBy == "" {
		return fmt.Errorf("%w: missing generator provenance", ErrInvalidProfessionExtraction)
	}

	if len(extracted.CategoryPath) == 0 {
		return fmt.Errorf("%w: empty category path", ErrInvalidProfessionExtraction)
	}
	if len(extracted.CategoryPath) > maxProfessionCategoryPathDepth {
		return fmt.Errorf(
			"%w: category path exceeds %d levels",
			ErrInvalidProfessionExtraction,
			maxProfessionCategoryPathDepth,
		)
	}

	if _, err := validateCategoryPath(extracted.CategoryPath, extracted.SourceLocale); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidProfessionExtraction, err)
	}

	return nil
}
