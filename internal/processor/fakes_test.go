package processor

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gosimple/slug"
	"github.com/vsayfb/gig-platform-categorization-service/internal/extractor"
)

// fakeRepository is a hand-rolled test double for Repository. Every method
// is backed by an overridable func field; leave a field nil in a test that
// never exercises that method. RunInTx defaults to just invoking fn inline
// (no real transaction/rollback semantics), which is enough for testing the
// processors' control flow without a database.
type fakeRepository struct {
	runInTxFn                      func(ctx context.Context, fn func(context.Context) error) error
	lockListingFn                  func(ctx context.Context, listingID uuid.UUID) (*ListingState, error)
	resolveBaseCategoryFn          func(ctx context.Context, slug string) (uuid.UUID, error)
	listActiveCategoriesFn         func(ctx context.Context, baseCategoryID uuid.UUID) ([]Category, error)
	classifyListingFn              func(ctx context.Context, listingID, baseCategoryID uuid.UUID, descriptionClean string) error
	findCategoryFn                 func(ctx context.Context, baseCategoryID uuid.UUID, parentID *uuid.UUID, canonicalName, canonicalSlug string, embedding []float32) (*Category, error)
	createCategoryFn               func(ctx context.Context, baseCategoryID uuid.UUID, parentID *uuid.UUID, name, slug, description, generatedBy string, isSelectable bool, depth int16, embedding []float32) (*Category, error)
	promoteCategorySelectableFn    func(ctx context.Context, categoryID, baseCategoryID uuid.UUID) (*Category, error)
	ensureCategoryLocalizationFn   func(ctx context.Context, localization CategoryLocalization) error
	attachCategoryFn               func(ctx context.Context, listingID, categoryID, baseCategoryID uuid.UUID) error
	attachUserCategoryFn           func(ctx context.Context, userID, categoryID uuid.UUID) error
	upsertGigDetailsFn             func(ctx context.Context, listingID uuid.UUID, details *extractor.ExtractedGigDetails, expiresAt *time.Time) error
	upsertSaleDetailsFn            func(ctx context.Context, listingID uuid.UUID, details *extractor.ExtractedSaleDetails, expiresAt *time.Time) error
	openListingFn                  func(ctx context.Context, listingID uuid.UUID) error
	getCurrentProfessionRequestFn  func(ctx context.Context, userID uuid.UUID) (uuid.UUID, bool, error)
	lockCurrentProfessionRequestFn func(ctx context.Context, userID uuid.UUID) (uuid.UUID, bool, error)
	upsertUserProfessionCategoryFn func(ctx context.Context, userID, categoryID uuid.UUID) error

	upsertUserProfessionCategoryCalls int
}

func (f *fakeRepository) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	if f.runInTxFn != nil {
		return f.runInTxFn(ctx, fn)
	}
	return fn(ctx)
}

func (f *fakeRepository) LockListing(ctx context.Context, listingID uuid.UUID) (*ListingState, error) {
	return f.lockListingFn(ctx, listingID)
}

func (f *fakeRepository) ResolveBaseCategory(ctx context.Context, slug string) (uuid.UUID, error) {
	return f.resolveBaseCategoryFn(ctx, slug)
}

func (f *fakeRepository) ListActiveCategories(ctx context.Context, baseCategoryID uuid.UUID) ([]Category, error) {
	return f.listActiveCategoriesFn(ctx, baseCategoryID)
}

func (f *fakeRepository) ClassifyListing(ctx context.Context, listingID, baseCategoryID uuid.UUID, descriptionClean string) error {
	return f.classifyListingFn(ctx, listingID, baseCategoryID, descriptionClean)
}

func (f *fakeRepository) FindCategory(ctx context.Context, baseCategoryID uuid.UUID, parentID *uuid.UUID, canonicalName, canonicalSlug string, embedding []float32) (*Category, error) {
	return f.findCategoryFn(ctx, baseCategoryID, parentID, canonicalName, canonicalSlug, embedding)
}

func (f *fakeRepository) EnsureCategoryLocalization(ctx context.Context, localization CategoryLocalization) error {
	if f.ensureCategoryLocalizationFn != nil {
		return f.ensureCategoryLocalizationFn(ctx, localization)
	}
	return nil
}

func (f *fakeRepository) CreateCategory(ctx context.Context, baseCategoryID uuid.UUID, parentID *uuid.UUID, name, slug, description, generatedBy string, isSelectable bool, depth int16, embedding []float32) (*Category, error) {
	return f.createCategoryFn(ctx, baseCategoryID, parentID, name, slug, description, generatedBy, isSelectable, depth, embedding)
}

func (f *fakeRepository) PromoteCategorySelectable(ctx context.Context, categoryID, baseCategoryID uuid.UUID) (*Category, error) {
	if f.promoteCategorySelectableFn != nil {
		return f.promoteCategorySelectableFn(ctx, categoryID, baseCategoryID)
	}
	return &Category{ID: categoryID, IsSelectable: true, IsActive: true}, nil
}

func (f *fakeRepository) AttachCategory(ctx context.Context, listingID, categoryID, baseCategoryID uuid.UUID) error {
	return f.attachCategoryFn(ctx, listingID, categoryID, baseCategoryID)
}

func (f *fakeRepository) AttachUserCategory(ctx context.Context, userID, categoryID uuid.UUID) error {
	return f.attachUserCategoryFn(ctx, userID, categoryID)
}

func (f *fakeRepository) UpsertGigDetails(ctx context.Context, listingID uuid.UUID, details *extractor.ExtractedGigDetails, expiresAt *time.Time) error {
	return f.upsertGigDetailsFn(ctx, listingID, details, expiresAt)
}

func (f *fakeRepository) UpsertSaleDetails(ctx context.Context, listingID uuid.UUID, details *extractor.ExtractedSaleDetails, expiresAt *time.Time) error {
	return f.upsertSaleDetailsFn(ctx, listingID, details, expiresAt)
}

func (f *fakeRepository) OpenListing(ctx context.Context, listingID uuid.UUID) error {
	return f.openListingFn(ctx, listingID)
}

func (f *fakeRepository) GetCurrentProfessionRequest(ctx context.Context, userID uuid.UUID) (uuid.UUID, bool, error) {
	return f.getCurrentProfessionRequestFn(ctx, userID)
}

func (f *fakeRepository) LockCurrentProfessionRequest(ctx context.Context, userID uuid.UUID) (uuid.UUID, bool, error) {
	return f.lockCurrentProfessionRequestFn(ctx, userID)
}

func (f *fakeRepository) UpsertUserProfessionCategory(ctx context.Context, userID, categoryID uuid.UUID) error {
	f.upsertUserProfessionCategoryCalls++
	if f.upsertUserProfessionCategoryFn != nil {
		return f.upsertUserProfessionCategoryFn(ctx, userID, categoryID)
	}
	return nil
}

// fakeExtractor is a test double for extractor.Extractor, letting tests
// control what a "real" AI extraction call would return without any
// network access.
type fakeExtractor struct {
	generator           string
	classify            func(ctx context.Context, description string) (string, error)
	extract             func(ctx context.Context, description, baseCategorySlug, catalog string) (*extractor.ExtractedListing, error)
	extractCategoryPath func(ctx context.Context, description, baseCategorySlug, catalog string) (*extractor.ExtractedListing, error)
}

func (f *fakeExtractor) Generator() string {
	if f.generator != "" {
		return f.generator
	}
	return "fake"
}

func (f *fakeExtractor) Classify(ctx context.Context, description string) (string, error) {
	return f.classify(ctx, description)
}

func (f *fakeExtractor) Extract(ctx context.Context, description, baseCategorySlug, catalog string) (*extractor.ExtractedListing, error) {
	return f.extract(ctx, description, baseCategorySlug, catalog)
}

func (f *fakeExtractor) ExtractCategoryPath(ctx context.Context, description, baseCategorySlug, catalog string) (*extractor.ExtractedListing, error) {
	if f.extractCategoryPath != nil {
		return f.extractCategoryPath(ctx, description, baseCategorySlug, catalog)
	}
	return f.extract(ctx, description, baseCategorySlug, catalog)
}

// fakeEmbeddingClient satisfies EmbeddingClient without a real embeddings
// endpoint. The vector's content doesn't matter to any of the code under
// test - only that it's non-empty, since processor.go/profession.go treat
// an empty embedding as an error.
type fakeEmbeddingClient struct{}

func (f *fakeEmbeddingClient) Embed(ctx context.Context, text string) ([]float32, error) {
	return []float32{0.1, 0.2, 0.3}, nil
}

func testCategoryNode(canonicalName, localizedName string, selectable bool) extractor.ExtractedCategoryNode {
	return extractor.ExtractedCategoryNode{
		CanonicalName:        canonicalName,
		CanonicalSlug:        slug.Make(canonicalName),
		CanonicalDescription: canonicalName + " services",
		LocalizedName:        localizedName,
		LocalizedSlug:        slug.Make(localizedName),
		LocalizedDescription: localizedName + " services",
		Locale:               "en",
		IsSelectable:         selectable,
	}
}
