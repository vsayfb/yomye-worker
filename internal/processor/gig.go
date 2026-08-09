package processor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gosimple/slug"
	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
	"github.com/vsayfb/gig-platform-categorization-service/internal/extractor"
	"github.com/vsayfb/gig-platform-categorization-service/internal/notification"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	BaseCategoryGig = "gig"

	listingStatusOpen       = "OPEN"
	listingStatusProcessing = "PROCESSING"
	maxCategoryPathDepth    = 4
)

var tracer = otel.Tracer("worker")

var (
	ErrCategoryNotDetected = errors.New("category not detected")
	ErrInvalidExtraction   = errors.New("invalid listing extraction")
)

var localePattern = regexp.MustCompile(`^[a-z]{2,3}(?:-[a-z0-9]{2,8})*$`)

type EmbeddingClient interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type ProcessResult struct {
	Category         *Category
	BaseCategorySlug string
	Title            string
	AlreadyOpen      bool
}

type embeddedCategoryNode struct {
	Node      extractor.ExtractedCategoryNode
	Embedding []float32
}

type preparedListing struct {
	baseCategoryID uuid.UUID
	extraction     *extractor.ExtractedListing
	embeddedPath   []embeddedCategoryNode
}

type GigProcessor struct {
	repo                  Repository
	embeddingClient       EmbeddingClient
	fallback              *extractor.FallbackExtractor
	notificationPublisher notification.Publisher
	cfg                   *config.Config
}

func NewGigProcessor(
	repo Repository,
	embeddingClient EmbeddingClient,
	fallback *extractor.FallbackExtractor,
	notificationPublisher notification.Publisher,
	cfg *config.Config,
) *GigProcessor {
	return &GigProcessor{
		repo:                  repo,
		embeddingClient:       embeddingClient,
		fallback:              fallback,
		notificationPublisher: notificationPublisher,
		cfg:                   cfg,
	}
}

type ListingCreatedMessage struct {
	ListingID   uuid.UUID  `json:"listing_id"`
	Description string     `json:"description"`
	AuthorID    uuid.UUID  `json:"author_id"`
	CountryID   *uuid.UUID `json:"country_id,omitempty"`
	PlaceID     *uuid.UUID `json:"place_id,omitempty"`
}

func (s *GigProcessor) Process(ctx context.Context, record Message) (err error) {
	ctx, span := tracer.Start(ctx, "process-listing")
	defer finishProcessSpan(span, &err)

	setMessageSpanAttributes(span, record)

	msg, err := decodeListingCreatedMessage(record.Body)
	if err != nil {
		return Permanent(err)
	}

	span.SetAttributes(attribute.String("listing.id", msg.ListingID.String()))

	return s.processListing(ctx, msg)
}

func (s *GigProcessor) processListing(ctx context.Context, msg ListingCreatedMessage) error {
	description := strings.TrimSpace(msg.Description)
	logListingProcessing(ctx, msg.ListingID, description)

	prepared, err := s.prepareListing(ctx, description)
	if errors.Is(err, ErrCategoryNotDetected) {
		logCategoryNotDetected(ctx, msg.ListingID)
		return nil
	}
	if err != nil {
		return err
	}

	result, err := s.persistListing(ctx, msg.ListingID, description, prepared)
	if err != nil {
		return err
	}

	if result.AlreadyOpen {
		logListingAlreadyOpen(ctx, msg.ListingID)
	} else {
		logListingCategorized(ctx, msg.ListingID, result)
	}

	return s.publishNotifications(ctx, msg, result)
}

func decodeListingCreatedMessage(body string) (ListingCreatedMessage, error) {
	var msg ListingCreatedMessage
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		return ListingCreatedMessage{}, fmt.Errorf("decode listing-created message: %w", err)
	}
	if err := validateListingCreatedMessage(msg); err != nil {
		return ListingCreatedMessage{}, err
	}
	return msg, nil
}

func validateListingCreatedMessage(msg ListingCreatedMessage) error {
	if msg.ListingID == uuid.Nil {
		return errors.New("invalid listing-created message: missing listing_id")
	}
	if msg.AuthorID == uuid.Nil {
		return errors.New("invalid listing-created message: missing author_id")
	}
	if strings.TrimSpace(msg.Description) == "" {
		return errors.New("invalid listing-created message: empty description")
	}
	if msg.PlaceID != nil && msg.CountryID == nil {
		return errors.New("invalid listing-created message: place_id requires country_id")
	}
	return nil
}

func (s *GigProcessor) prepareListing(ctx context.Context, description string) (*preparedListing, error) {
	baseCategorySlug := BaseCategoryGig

	baseCategoryID, err := s.resolveBaseCategory(ctx, baseCategorySlug)
	if err != nil {
		return nil, err
	}

	extracted, err := s.extractListing(ctx, description, baseCategorySlug, baseCategoryID)
	if err != nil {
		return nil, err
	}

	if err := validateExtractionForBaseCategory(extracted, baseCategorySlug); err != nil {
		return nil, err
	}

	embeddedPath, err := s.embedCategoryPath(ctx, extracted.CategoryPath)
	if err != nil {
		return nil, err
	}

	return &preparedListing{
		baseCategoryID: baseCategoryID,
		extraction:     extracted,
		embeddedPath:   embeddedPath,
	}, nil
}

func (s *GigProcessor) resolveBaseCategory(ctx context.Context, baseCategorySlug string) (uuid.UUID, error) {
	baseCategoryID, err := s.repo.ResolveBaseCategory(ctx, baseCategorySlug)
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve base category %q: %w", baseCategorySlug, err)
	}
	return baseCategoryID, nil
}

func (s *GigProcessor) extractListing(
	ctx context.Context,
	description string,
	baseCategorySlug string,
	baseCategoryID uuid.UUID,
) (*extractor.ExtractedListing, error) {
	activeCategories, err := s.repo.ListActiveCategories(ctx, baseCategoryID)
	if err != nil {
		return nil, fmt.Errorf("list active categories: %w", err)
	}

	extracted, err := s.fallback.Extract(
		ctx,
		description,
		baseCategorySlug,
		formatCategoryCatalog(activeCategories),
	)
	if err != nil {
		return nil, fmt.Errorf("extract listing details: %w", err)
	}

	return extracted, nil
}

func (s *GigProcessor) embedCategoryPath(
	ctx context.Context,
	categoryPath []extractor.ExtractedCategoryNode,
) ([]embeddedCategoryNode, error) {
	embeddedPath := make([]embeddedCategoryNode, 0, len(categoryPath))
	for index, node := range categoryPath {
		embedded, err := s.embedCategoryNode(ctx, categoryPath, index, node)
		if err != nil {
			return nil, err
		}
		embeddedPath = append(embeddedPath, embedded)
	}
	return embeddedPath, nil
}

func (s *GigProcessor) embedCategoryNode(
	ctx context.Context,
	categoryPath []extractor.ExtractedCategoryNode,
	index int,
	node extractor.ExtractedCategoryNode,
) (embeddedCategoryNode, error) {
	embedding, err := s.embeddingClient.Embed(ctx, categoryEmbeddingText(categoryPath, index))
	if err != nil {
		metrics.ObserveEmbeddingFailure(ctx, "listing")
		return embeddedCategoryNode{}, fmt.Errorf("generate category embedding for %q: %w", node.CanonicalName, err)
	}
	if len(embedding) == 0 {
		metrics.ObserveEmbeddingFailure(ctx, "listing")
		return embeddedCategoryNode{}, fmt.Errorf("generate category embedding for %q: empty embedding", node.CanonicalName)
	}
	return embeddedCategoryNode{Node: node, Embedding: embedding}, nil
}

func categoryEmbeddingText(path []extractor.ExtractedCategoryNode, index int) string {
	breadcrumb := make([]string, 0, index+1)
	for _, node := range path[:index+1] {
		breadcrumb = append(breadcrumb, node.CanonicalName)
	}
	return strings.Join(breadcrumb, " > ") + "\n" +
		path[index].CanonicalName + "\n" +
		path[index].CanonicalDescription
}

func (s *GigProcessor) persistListing(
	ctx context.Context,
	listingID uuid.UUID,
	description string,
	prepared *preparedListing,
) (*ProcessResult, error) {
	result := newProcessResult(prepared.extraction)

	err := s.repo.RunInTx(ctx, func(txCtx context.Context) error {
		return s.persistListingInTx(txCtx, listingID, description, prepared, result)
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (s *GigProcessor) persistListingInTx(
	ctx context.Context,
	listingID uuid.UUID,
	description string,
	prepared *preparedListing,
	result *ProcessResult,
) error {
	state, err := s.repo.LockListing(ctx, listingID)
	if err != nil {
		return fmt.Errorf("lock listing: %w", err)
	}

	if state.Status == listingStatusOpen {
		copyOpenListingState(result, state.Category, state.BaseCategorySlug, state.Title)
		return nil
	}
	if state.Status != listingStatusProcessing {
		return fmt.Errorf("listing has non-processable status %q", state.Status)
	}

	if err := s.classifyListingInTx(ctx, listingID, prepared.baseCategoryID, description); err != nil {
		return err
	}

	category, err := s.resolveCategoryPath(ctx, prepared)
	if err != nil {
		return err
	}

	if err := s.attachCategory(ctx, listingID, category.ID, prepared.baseCategoryID); err != nil {
		return err
	}

	if err := s.persistExtractedDetails(ctx, listingID, prepared.extraction); err != nil {
		return err
	}

	if err := s.openListing(ctx, listingID); err != nil {
		return err
	}

	result.Category = category
	return nil
}

func newProcessResult(extracted *extractor.ExtractedListing) *ProcessResult {
	return &ProcessResult{
		BaseCategorySlug: extracted.BaseCategorySlug,
		Title:            extractedTitle(extracted),
	}
}

func copyOpenListingState(
	result *ProcessResult,
	category *Category,
	baseCategorySlug string,
	title string,
) {
	result.Category = category
	result.BaseCategorySlug = baseCategorySlug
	result.Title = title
	result.AlreadyOpen = true
}

func (s *GigProcessor) classifyListingInTx(
	ctx context.Context,
	listingID uuid.UUID,
	baseCategoryID uuid.UUID,
	description string,
) error {
	if err := s.repo.ClassifyListing(ctx, listingID, baseCategoryID, description); err != nil {
		return fmt.Errorf("classify listing: %w", err)
	}
	return nil
}

func (s *GigProcessor) resolveCategoryPath(
	ctx context.Context,
	prepared *preparedListing,
) (*Category, error) {
	var parentID *uuid.UUID
	var leaf *Category

	for depth, embedded := range prepared.embeddedPath {
		category, err := s.findOrCreateCategory(
			ctx,
			prepared.baseCategoryID,
			parentID,
			depth,
			embedded,
			prepared.extraction.GeneratedBy,
		)
		if err != nil {
			return nil, err
		}

		leaf = category
		parentID = uuidPointer(category.ID)
	}

	if leaf == nil {
		return nil, ErrCategoryNotDetected
	}
	return leaf, nil
}

func (s *GigProcessor) findOrCreateCategory(
	ctx context.Context,
	baseCategoryID uuid.UUID,
	parentID *uuid.UUID,
	depth int,
	embedded embeddedCategoryNode,
	generatedBy string,
) (*Category, error) {
	categorySlug := embedded.Node.CanonicalSlug
	category, err := s.repo.FindCategory(
		ctx,
		baseCategoryID,
		parentID,
		embedded.Node.CanonicalName,
		categorySlug,
		embedded.Embedding,
	)
	if err != nil {
		return nil, fmt.Errorf("find similar category at depth %d: %w", depth, err)
	}
	if category != nil {
		metrics.ObserveCategoryResolution(ctx, "listing", "reused")
		if embedded.Node.IsSelectable && !category.IsSelectable {
			category, err = s.repo.PromoteCategorySelectable(ctx, category.ID, baseCategoryID)
			if err != nil {
				return nil, fmt.Errorf("promote category at depth %d: %w", depth, err)
			}
		}
		if err := s.ensureLocalization(ctx, category.ID, embedded.Node, generatedBy); err != nil {
			return nil, fmt.Errorf("ensure category localization at depth %d: %w", depth, err)
		}
		return category, nil
	}

	category, err = s.repo.CreateCategory(
		ctx,
		baseCategoryID,
		parentID,
		embedded.Node.CanonicalName,
		categorySlug,
		embedded.Node.CanonicalDescription,
		generatedBy,
		embedded.Node.IsSelectable,
		int16(depth),
		embedded.Embedding,
	)
	if err != nil {
		return nil, fmt.Errorf("create category at depth %d: %w", depth, err)
	}
	if category == nil {
		return nil, fmt.Errorf("create category at depth %d: repository returned nil category", depth)
	}
	metrics.ObserveCategoryResolution(ctx, "listing", "created")
	if err := s.ensureLocalization(ctx, category.ID, embedded.Node, generatedBy); err != nil {
		return nil, fmt.Errorf("ensure category localization at depth %d: %w", depth, err)
	}
	return category, nil
}

func (s *GigProcessor) ensureLocalization(
	ctx context.Context,
	categoryID uuid.UUID,
	node extractor.ExtractedCategoryNode,
	generatedBy string,
) error {
	return s.repo.EnsureCategoryLocalization(ctx, CategoryLocalization{
		CategoryID:  categoryID,
		Locale:      node.Locale,
		Name:        node.LocalizedName,
		Slug:        node.LocalizedSlug,
		Description: node.LocalizedDescription,
		GeneratedBy: generatedBy,
	})
}

func uuidPointer(id uuid.UUID) *uuid.UUID {
	return &id
}

func (s *GigProcessor) attachCategory(
	ctx context.Context,
	listingID uuid.UUID,
	categoryID uuid.UUID,
	baseCategoryID uuid.UUID,
) error {
	if err := s.repo.AttachCategory(ctx, listingID, categoryID, baseCategoryID); err != nil {
		return fmt.Errorf("attach category: %w", err)
	}
	return nil
}

func (s *GigProcessor) persistExtractedDetails(
	ctx context.Context,
	listingID uuid.UUID,
	extracted *extractor.ExtractedListing,
) error {
	if extracted.BaseCategorySlug != BaseCategoryGig {
		return fmt.Errorf("%w: unknown base category %q", ErrInvalidExtraction, extracted.BaseCategorySlug)
	}
	return s.persistGigDetails(ctx, listingID, extracted)
}

func (s *GigProcessor) persistGigDetails(
	ctx context.Context,
	listingID uuid.UUID,
	extracted *extractor.ExtractedListing,
) error {
	expiresAt := gigExpiry(extracted.ExpiresAt, time.Now().UTC())
	if err := s.repo.UpsertGigDetails(ctx, listingID, extracted.Gig, expiresAt); err != nil {
		return fmt.Errorf("persist gig details: %w", err)
	}
	return nil
}

func gigExpiry(expiresAt *time.Time, now time.Time) *time.Time {
	if expiresAt != nil {
		return expiresAt
	}
	defaultExpiry := now.AddDate(0, 1, 0)
	return &defaultExpiry
}

func (s *GigProcessor) openListing(ctx context.Context, listingID uuid.UUID) error {
	if err := s.repo.OpenListing(ctx, listingID); err != nil {
		return fmt.Errorf("open listing: %w", err)
	}
	return nil
}

func (s *GigProcessor) publishNotifications(
	ctx context.Context,
	msg ListingCreatedMessage,
	result *ProcessResult,
) error {
	if err := s.publishListingApproved(ctx, msg, result); err != nil {
		return err
	}
	return s.publishGigCategoryMatched(ctx, msg, result.Category)
}

func (s *GigProcessor) publishListingApproved(
	ctx context.Context,
	msg ListingCreatedMessage,
	result *ProcessResult,
) error {
	occurredAt := time.Now().UTC()
	event := notification.ListingApprovedEnvelope{
		Type:    notification.EventListingApproved,
		EventID: notification.StableEventID(notification.EventListingApproved, msg.ListingID),
		Version: notification.CurrentEventVersion,
		Payload: notification.ListingApprovedEvent{
			RecipientID:      msg.AuthorID,
			ListingID:        msg.ListingID,
			BaseCategorySlug: result.BaseCategorySlug,
			Title:            result.Title,
			OccurredAt:       occurredAt,
		},
	}

	if err := s.notificationPublisher.PublishListingApproved(ctx, event); err != nil {
		return fmt.Errorf("publish listing-approved event: %w", err)
	}
	return nil
}

func (s *GigProcessor) publishGigCategoryMatched(
	ctx context.Context,
	msg ListingCreatedMessage,
	category *Category,
) error {
	if category == nil {
		return errors.New("publish gig-category-matched event: missing category")
	}

	event := notification.GigCategoryMatchedEventEnvelope{
		Type:    notification.EventGigCategoryMatched,
		EventID: notification.StableEventID(notification.EventGigCategoryMatched, msg.ListingID, category.ID),
		Version: notification.CurrentEventVersion,
		Payload: notification.GigCategoryMatchedEvent{
			GigID:      msg.ListingID,
			CategoryID: category.ID,
			CountryID:  msg.CountryID,
			PlaceID:    msg.PlaceID,
			OccurredAt: time.Now().UTC(),
		},
	}

	if err := s.notificationPublisher.PublishGigCategoryMatched(ctx, event); err != nil {
		return fmt.Errorf("publish gig-category-matched event: %w", err)
	}
	return nil
}

func validateExtractionForBaseCategory(
	extracted *extractor.ExtractedListing,
	expectedBaseCategorySlug string,
) error {
	if err := validateExtraction(extracted); err != nil {
		return err
	}
	if extracted.BaseCategorySlug != expectedBaseCategorySlug {
		return fmt.Errorf(
			"%w: classified base category %q does not match extracted base category %q",
			ErrInvalidExtraction,
			expectedBaseCategorySlug,
			extracted.BaseCategorySlug,
		)
	}
	return nil
}

func validateExtraction(extracted *extractor.ExtractedListing) error {
	if extracted == nil {
		return fmt.Errorf("%w: empty response", ErrInvalidExtraction)
	}

	normalizeExtractionHeader(extracted)
	if extracted.GeneratedBy == "" {
		return fmt.Errorf("%w: missing generator provenance", ErrInvalidExtraction)
	}

	leafName, err := validateCategoryPath(extracted.CategoryPath, extracted.SourceLocale)
	if err != nil {
		return err
	}

	if extracted.BaseCategorySlug != BaseCategoryGig {
		return fmt.Errorf("%w: unknown base category %q", ErrInvalidExtraction, extracted.BaseCategorySlug)
	}
	return validateGigExtraction(extracted, leafName)
}

func normalizeExtractionHeader(extracted *extractor.ExtractedListing) {
	extracted.BaseCategorySlug = strings.ToLower(strings.TrimSpace(extracted.BaseCategorySlug))
	extracted.SourceLocale = normalizeLocale(extracted.SourceLocale)
	extracted.GeneratedBy = strings.TrimSpace(extracted.GeneratedBy)
}

func validateCategoryPath(
	path []extractor.ExtractedCategoryNode,
	sourceLocale string,
) (string, error) {
	if len(path) == 0 {
		return "", ErrCategoryNotDetected
	}
	if len(path) > maxCategoryPathDepth {
		return "", fmt.Errorf(
			"%w: category path exceeds %d levels",
			ErrInvalidExtraction,
			maxCategoryPathDepth,
		)
	}

	for i := range path {
		if err := normalizeAndValidateCategoryNode(path, i, sourceLocale); err != nil {
			return "", err
		}
	}
	if !path[len(path)-1].IsSelectable {
		return "", fmt.Errorf(
			"%w: final category %q is not selectable",
			ErrInvalidExtraction,
			path[len(path)-1].CanonicalName,
		)
	}

	return path[len(path)-1].CanonicalName, nil
}

func normalizeAndValidateCategoryNode(
	path []extractor.ExtractedCategoryNode,
	index int,
	sourceLocale string,
) error {
	node := &path[index]
	node.CanonicalName = strings.TrimSpace(node.CanonicalName)
	node.CanonicalDescription = strings.TrimSpace(node.CanonicalDescription)
	node.LocalizedName = strings.TrimSpace(node.LocalizedName)
	node.LocalizedDescription = strings.TrimSpace(node.LocalizedDescription)
	node.Locale = normalizeLocale(node.Locale)
	node.CanonicalSlug = slug.Make(node.CanonicalName)
	node.LocalizedSlug = slug.Make(node.LocalizedName)

	if node.CanonicalName == "" || node.CanonicalDescription == "" ||
		node.LocalizedName == "" || node.LocalizedDescription == "" {
		return fmt.Errorf("%w: category path node %d is incomplete", ErrInvalidExtraction, index)
	}
	if node.CanonicalSlug == "" || node.LocalizedSlug == "" {
		return fmt.Errorf("%w: category path node %d has no usable slug", ErrInvalidExtraction, index)
	}
	if !localePattern.MatchString(node.Locale) || node.Locale != sourceLocale {
		return fmt.Errorf(
			"%w: category path node %d locale %q does not match source locale %q",
			ErrInvalidExtraction,
			index,
			node.Locale,
			sourceLocale,
		)
	}
	if index > 0 && strings.EqualFold(path[index-1].CanonicalName, node.CanonicalName) {
		return fmt.Errorf(
			"%w: duplicate adjacent category %q",
			ErrInvalidExtraction,
			node.CanonicalName,
		)
	}
	return nil
}

func normalizeLocale(locale string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(locale), "_", "-"))
}

func validateGigExtraction(extracted *extractor.ExtractedListing, leafName string) error {
	if extracted.Gig == nil {
		return fmt.Errorf("%w: gig details shape", ErrInvalidExtraction)
	}

	normalizeGigDetails(extracted, leafName)
	if extracted.Gig.Title == "" || extracted.Gig.Profession == "" {
		return fmt.Errorf("%w: missing gig title or profession", ErrInvalidExtraction)
	}
	if extracted.Gig.PayAmount != nil && *extracted.Gig.PayAmount < 0 {
		return fmt.Errorf("%w: negative gig pay", ErrInvalidExtraction)
	}
	return nil
}

func normalizeGigDetails(extracted *extractor.ExtractedListing, profession string) {
	extracted.Gig.Title = strings.TrimSpace(extracted.Gig.Title)
	extracted.Gig.Profession = profession
	extracted.Gig.PayCurrency = strings.ToUpper(strings.TrimSpace(extracted.Gig.PayCurrency))
	if extracted.Gig.PayCurrency == "" {
		extracted.Gig.PayCurrency = "TRY"
	}
}

func extractedTitle(extracted *extractor.ExtractedListing) string {
	if extracted == nil {
		return ""
	}
	if extracted.Gig != nil {
		return extracted.Gig.Title
	}
	return ""
}

func formatCategoryCatalog(categories []Category) string {
	var builder strings.Builder
	for _, category := range categories {
		writeCategoryCatalogLine(&builder, category)
	}
	return strings.TrimSpace(builder.String())
}

func writeCategoryCatalogLine(builder *strings.Builder, category Category) {
	builder.WriteString(categoryIndent(category.Depth))
	builder.WriteString("- ")
	builder.WriteString(category.CanonicalName)
	builder.WriteString(": ")
	builder.WriteString(category.CanonicalDescription)
	builder.WriteString(" [selectable=")
	builder.WriteString(fmt.Sprintf("%t", category.IsSelectable))
	builder.WriteByte(']')
	builder.WriteByte('\n')
}

func categoryIndent(depth int16) string {
	if depth <= 0 {
		return ""
	}
	return strings.Repeat("  ", int(depth))
}

func setMessageSpanAttributes(span trace.Span, record Message) {
	span.SetAttributes(
		attribute.String("message.id", record.ID),
		attribute.String("message.type", record.Type),
	)
}

func finishProcessSpan(span trace.Span, processErr *error) {
	defer span.End()
	if *processErr != nil {
		span.RecordError(*processErr)
		span.SetStatus(codes.Error, (*processErr).Error())
		return
	}
	span.SetStatus(codes.Ok, "listing processed successfully")
}

func logListingProcessing(ctx context.Context, listingID uuid.UUID, description string) {
	slog.InfoContext(
		ctx,
		"process listing",
		"listing_id", listingID,
		"description_len", len(description),
	)
}

func logCategoryNotDetected(ctx context.Context, listingID uuid.UUID) {
	slog.InfoContext(ctx, "category not detected", "listing_id", listingID)
}

func logListingAlreadyOpen(ctx context.Context, listingID uuid.UUID) {
	slog.InfoContext(ctx, "listing already open", "listing_id", listingID)
}

func logListingCategorized(ctx context.Context, listingID uuid.UUID, result *ProcessResult) {
	slog.InfoContext(
		ctx,
		"listing categorized",
		"listing_id", listingID,
		"base_category", result.BaseCategorySlug,
		"category", result.Category,
	)
}
