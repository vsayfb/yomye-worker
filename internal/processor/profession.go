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
	"github.com/vsayfb/gig-platform-categorization-service/internal/notification"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/metrics"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const maxProfessionCategoryPathDepth = 4

var ErrInvalidProfessionExtraction = errors.New("invalid profession extraction")

type ProfessionProcessor struct {
	repo                  Repository
	embeddingClient       EmbeddingClient
	fallback              *extractor.FallbackExtractor
	notificationPublisher notification.Publisher
	cfg                   *config.Config
}

func NewProfessionProcessor(
	repo Repository,
	embeddingClient EmbeddingClient,
	fallback *extractor.FallbackExtractor,
	notificationPublisher notification.Publisher,
	cfg *config.Config,
) *ProfessionProcessor {
	return &ProfessionProcessor{
		repo:                  repo,
		embeddingClient:       embeddingClient,
		fallback:              fallback,
		notificationPublisher: notificationPublisher,
		cfg:                   cfg,
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
		return Permanent(fmt.Errorf("decode new-profession message: %w", err))
	}

	msg.Description = strings.TrimSpace(msg.Description)
	if msg.RequestID == uuid.Nil {
		return Permanent(errors.New("invalid new-profession message: missing request_id"))
	}
	if msg.AuthorID == uuid.Nil {
		return Permanent(errors.New("invalid new-profession message: missing author_id"))
	}
	if msg.Description == "" {
		return Permanent(errors.New("invalid new-profession message: empty description"))
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

	moderation, err := s.fallback.Moderate(ctx, msg.Description)
	if err != nil {
		return fmt.Errorf("moderate profession content: %w", err)
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.String("moderation.decision", string(moderation.Decision)),
		attribute.String("moderation.generator", moderation.GeneratedBy),
	)
	if moderation.Decision == extractor.ModerationReject {
		trace.SpanFromContext(ctx).SetAttributes(attribute.String("moderation.reason", moderation.Reason))
		slog.InfoContext(
			ctx,
			"profession content rejected",
			"request_id", msg.RequestID,
			"author_id", msg.AuthorID,
			"reason", moderation.Reason,
			"generated_by", moderation.GeneratedBy,
		)
		return publishContentRejected(
			ctx,
			s.notificationPublisher,
			msg.AuthorID,
			notification.RejectedSubjectProfession,
			msg.RequestID,
			moderation.Reason,
		)
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

	extracted, err := s.fallback.ExtractCategoryPath(
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

	embedCtx, embedSpan := tracer.Start(
		ctx,
		"category_path.embed",
		trace.WithAttributes(
			attribute.String("workflow", "profession"),
			attribute.Int("category.path_depth", len(extracted.CategoryPath)),
		),
	)
	embeddedPath := make([]embeddedProfessionCategoryNode, 0, len(extracted.CategoryPath))
	for index, node := range extracted.CategoryPath {
		embedding, err := s.embeddingClient.Embed(
			embedCtx,
			categoryEmbeddingText(extracted.CategoryPath, index),
		)
		if err != nil {
			metrics.ObserveEmbeddingFailure(ctx, "profession")
			err = fmt.Errorf("generate profession category embedding for %q: %w", node.CanonicalName, err)
			finishProcessorSpan(embedSpan, err)
			return err
		}
		if len(embedding) == 0 {
			metrics.ObserveEmbeddingFailure(ctx, "profession")
			err = fmt.Errorf("generate profession category embedding for %q: empty embedding", node.CanonicalName)
			finishProcessorSpan(embedSpan, err)
			return err
		}

		embeddedPath = append(embeddedPath, embeddedProfessionCategoryNode{
			node:      node,
			embedding: embedding,
		})
	}
	finishProcessorSpan(embedSpan, nil)

	var professionCategory *Category
	stale := false

	txCtx, txSpan := tracer.Start(
		ctx,
		"profession.persist_transaction",
		trace.WithAttributes(
			attribute.String("profession.request_id", msg.RequestID.String()),
			attribute.String("profession.author_id", msg.AuthorID.String()),
			attribute.String("base_category.id", baseCategoryID.String()),
			attribute.Int("category.path_depth", len(embeddedPath)),
		),
	)
	txErr := s.repo.RunInTx(txCtx, func(txCtx context.Context) error {
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

			category, err := s.resolveProfessionCategoryNode(
				txCtx, baseCategoryID, parentID, depth, embedded, extracted.GeneratedBy,
			)
			if err != nil {
				return err
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
	})
	finishProcessorSpan(txSpan, txErr)
	if txErr != nil {
		return txErr
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

func (s *ProfessionProcessor) resolveProfessionCategoryNode(
	ctx context.Context,
	baseCategoryID uuid.UUID,
	parentID *uuid.UUID,
	depth int,
	embedded embeddedProfessionCategoryNode,
	generatedBy string,
) (category *Category, resultErr error) {
	ctx, span := tracer.Start(
		ctx,
		"taxonomy.resolve_node",
		trace.WithAttributes(
			attribute.String("workflow", "profession"),
			attribute.Int("category.depth", depth),
			attribute.String("category.canonical_slug", embedded.node.CanonicalSlug),
			attribute.Bool("category.is_selectable", embedded.node.IsSelectable),
		),
	)
	defer func() { finishProcessorSpan(span, resultErr) }()

	category, err := s.repo.FindCategory(
		ctx,
		baseCategoryID,
		parentID,
		embedded.node.CanonicalName,
		embedded.node.CanonicalSlug,
		embedded.embedding,
	)
	if err != nil {
		return nil, fmt.Errorf("find profession category at depth %d: %w", depth, err)
	}

	resolutionAction := "reused"
	if category == nil {
		resolutionAction = "created"
		category, err = s.repo.CreateCategory(
			ctx,
			baseCategoryID,
			parentID,
			embedded.node.CanonicalName,
			embedded.node.CanonicalSlug,
			embedded.node.CanonicalDescription,
			generatedBy,
			embedded.node.IsSelectable,
			depth,
			embedded.embedding,
		)
		if err != nil {
			return nil, fmt.Errorf("create profession category at depth %d: %w", depth, err)
		}
	} else if embedded.node.IsSelectable && !category.IsSelectable {
		category, err = s.repo.PromoteCategorySelectable(ctx, category.ID, baseCategoryID)
		if err != nil {
			return nil, fmt.Errorf("promote profession category at depth %d: %w", depth, err)
		}
	}

	if category == nil || category.ID == uuid.Nil {
		return nil, fmt.Errorf(
			"persist profession category at depth %d: repository returned an invalid category",
			depth,
		)
	}

	span.SetAttributes(
		attribute.String("taxonomy.action", resolutionAction),
		attribute.String("category.id", category.ID.String()),
	)
	metrics.ObserveCategoryResolution(ctx, "profession", resolutionAction)

	if err := s.repo.EnsureCategoryLocalization(ctx, CategoryLocalization{
		CategoryID:  category.ID,
		Locale:      embedded.node.Locale,
		Name:        embedded.node.LocalizedName,
		Slug:        embedded.node.LocalizedSlug,
		Description: embedded.node.LocalizedDescription,
		GeneratedBy: generatedBy,
	}); err != nil {
		return nil, fmt.Errorf("ensure profession localization at depth %d: %w", depth, err)
	}

	return category, nil
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
	if len(extracted.GeneratedBy) > maxGeneratedByLength {
		return fmt.Errorf("%w: generator provenance exceeds %d characters", ErrInvalidProfessionExtraction, maxGeneratedByLength)
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
