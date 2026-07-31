package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/vsayfb/gig-platform-categorization-service/internal/extractor"
)

func TestValidateExtractionAcceptsSaleShapes(t *testing.T) {
	t.Parallel()

	currency := "try"
	amount := 7500.0
	condition := "good"

	tests := []struct {
		name string
		sale *extractor.ExtractedSaleDetails
	}{
		{
			name: "fixed price",
			sale: &extractor.ExtractedSaleDetails{
				Title:         "Bisiklet",
				PriceAmount:   &amount,
				PriceCurrency: &currency,
				PricingType:   "fixed",
				Condition:     &condition,
			},
		},
		{
			name: "contact",
			sale: &extractor.ExtractedSaleDetails{
				Title:       "Bisiklet",
				PricingType: "contact",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := &extractor.ExtractedListing{
				BaseCategorySlug: BaseCategoryForSale,
				CategoryPath: []extractor.ExtractedCategoryNode{
					{Name: "Spor ve Outdoor", Description: "Spor ürünleri"},
					{Name: "Bisiklet", Description: "Bisiklet ve ekipmanları"},
				},
				Sale: tt.sale,
			}
			if err := validateExtraction(got); err != nil {
				t.Fatalf("validateExtraction() error = %v", err)
			}
		})
	}
}

func TestValidateExtractionRejectsInvalidSalePriceShapes(t *testing.T) {
	t.Parallel()

	currency := "TRY"
	amount := 100.0

	tests := []struct {
		name string
		sale *extractor.ExtractedSaleDetails
	}{
		{
			name: "fixed without amount",
			sale: &extractor.ExtractedSaleDetails{
				Title:         "Bisiklet",
				PriceCurrency: &currency,
				PricingType:   "fixed",
			},
		},
		{
			name: "free with amount",
			sale: &extractor.ExtractedSaleDetails{
				Title:       "Bisiklet",
				PriceAmount: &amount,
				PricingType: "free",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := &extractor.ExtractedListing{
				BaseCategorySlug: BaseCategoryForSale,
				CategoryPath: []extractor.ExtractedCategoryNode{
					{Name: "Spor ve Outdoor", Description: "Spor ürünleri"},
					{Name: "Bisiklet", Description: "Bisiklet ve ekipmanları"},
				},
				Sale: tt.sale,
			}
			err := validateExtraction(got)
			if !errors.Is(err, ErrInvalidExtraction) {
				t.Fatalf("validateExtraction() error = %v, want ErrInvalidExtraction", err)
			}
		})
	}
}

func TestValidateExtractionRejectsMismatchedDetails(t *testing.T) {
	t.Parallel()

	got := &extractor.ExtractedListing{
		BaseCategorySlug: BaseCategoryGig,
		CategoryPath: []extractor.ExtractedCategoryNode{
			{Name: "Teknik Hizmetler", Description: "Teknik işler"},
			{Name: "Elektrikçi", Description: "Elektrik hizmetleri"},
		},
		Sale: &extractor.ExtractedSaleDetails{
			Title:       "Kablo",
			PricingType: "contact",
		},
	}

	err := validateExtraction(got)
	if !errors.Is(err, ErrInvalidExtraction) {
		t.Fatalf("validateExtraction() error = %v, want ErrInvalidExtraction", err)
	}
}

func TestValidateExtractionNormalizesGigDefaults(t *testing.T) {
	t.Parallel()

	got := &extractor.ExtractedListing{
		BaseCategorySlug: BaseCategoryGig,
		CategoryPath: []extractor.ExtractedCategoryNode{
			{Name: "Teknik Hizmetler", Description: "Teknik işler"},
			{Name: "Elektrikçi", Description: "Elektrik hizmetleri"},
		},
		Gig: &extractor.ExtractedGigDetails{
			Title:      "Elektrikçi Aranıyor",
			Profession: "Elektrikçi",
		},
	}

	if err := validateExtraction(got); err != nil {
		t.Fatalf("validateExtraction() error = %v", err)
	}
	if got.Gig.PayCurrency != "TRY" {
		t.Fatalf("PayCurrency = %q, want TRY", got.Gig.PayCurrency)
	}
	if got.Gig.Profession != "Elektrikçi" {
		t.Fatalf("Profession = %q, want leaf category Elektrikçi", got.Gig.Profession)
	}
}

func TestProcessListingResolvesCategoryPathParentByParent(t *testing.T) {
	t.Parallel()

	baseID := uuid.New()
	categoryIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	repo := &categoryPathRepository{
		baseID:      baseID,
		categoryIDs: categoryIDs,
	}
	embeddingClient := &recordingEmbeddingClient{}
	fallback := extractor.NewFallbackExtractor(staticExtractor{
		result: &extractor.ExtractedListing{
			BaseCategorySlug: BaseCategoryGig,
			CategoryPath: []extractor.ExtractedCategoryNode{
				{Name: "Bilişim ve Teknoloji", Description: "Teknoloji hizmetleri"},
				{Name: "Yazılım", Description: "Yazılım geliştirme"},
				{Name: "Backend Developer", Description: "Sunucu tarafı geliştirme"},
			},
			Gig: &extractor.ExtractedGigDetails{
				Title:       "Backend Developer Aranıyor",
				Profession:  "Backend Developer",
				PayCurrency: "TRY",
			},
		},
	})

	service := NewService(repo, embeddingClient, fallback, nil)
	result, err := service.ProcessListing(context.Background(), uuid.New(), "Backend geliştirici arıyorum")
	if err != nil {
		t.Fatalf("ProcessListing() error = %v", err)
	}

	if result.Category == nil || result.Category.ID != categoryIDs[2] {
		t.Fatalf("ProcessListing() category = %#v, want leaf %s", result.Category, categoryIDs[2])
	}
	if len(repo.parents) != 3 {
		t.Fatalf("resolved %d category levels, want 3", len(repo.parents))
	}
	if repo.parents[0] != nil {
		t.Fatalf("root parent = %v, want nil", repo.parents[0])
	}
	if repo.parents[1] == nil || *repo.parents[1] != categoryIDs[0] {
		t.Fatalf("second parent = %v, want %s", repo.parents[1], categoryIDs[0])
	}
	if repo.parents[2] == nil || *repo.parents[2] != categoryIDs[1] {
		t.Fatalf("leaf parent = %v, want %s", repo.parents[2], categoryIDs[1])
	}
	if repo.attachedCategoryID != categoryIDs[2] {
		t.Fatalf("attached category = %s, want leaf %s", repo.attachedCategoryID, categoryIDs[2])
	}
	if len(embeddingClient.texts) != 3 {
		t.Fatalf("embedding calls = %d, want 3", len(embeddingClient.texts))
	}
}

func TestFormatCategoryCatalogPreservesHierarchy(t *testing.T) {
	t.Parallel()

	got := formatCategoryCatalog([]Category{
		{
			Name:        "Yapı ve Teknik Hizmetler",
			Description: "Yapı ve bakım hizmetleri",
			Depth:       0,
		},
		{
			Name:        "Tesisat",
			Description: "Su ve sıhhi tesisat hizmetleri",
			Depth:       1,
		},
		{
			Name:        "Tesisatçı",
			Description: "Tesisat bakım ve onarım hizmetleri",
			Depth:       2,
		},
	})

	want := "- Yapı ve Teknik Hizmetler: Yapı ve bakım hizmetleri\n" +
		"  - Tesisat: Su ve sıhhi tesisat hizmetleri\n" +
		"    - Tesisatçı: Tesisat bakım ve onarım hizmetleri"
	if got != want {
		t.Fatalf("formatCategoryCatalog() =\n%s\nwant:\n%s", got, want)
	}
}

type staticExtractor struct {
	result *extractor.ExtractedListing
}

func (s staticExtractor) Classify(context.Context, string) (string, error) {
	return s.result.BaseCategorySlug, nil
}

func (s staticExtractor) Extract(
	context.Context,
	string,
	string,
	string,
) (*extractor.ExtractedListing, error) {
	return s.result, nil
}

type recordingEmbeddingClient struct {
	texts []string
}

func (c *recordingEmbeddingClient) Embed(_ context.Context, text string) ([]float32, error) {
	c.texts = append(c.texts, text)
	return []float32{1, 0, 0}, nil
}

type categoryPathRepository struct {
	baseID             uuid.UUID
	categoryIDs        []uuid.UUID
	parents            []*uuid.UUID
	attachedCategoryID uuid.UUID
}

func (r *categoryPathRepository) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func (r *categoryPathRepository) LockListing(context.Context, uuid.UUID) (*ListingState, error) {
	return &ListingState{Status: "PROCESSING"}, nil
}

func (r *categoryPathRepository) ResolveBaseCategory(context.Context, string) (uuid.UUID, error) {
	return r.baseID, nil
}

func (r *categoryPathRepository) ListActiveCategories(
	context.Context,
	uuid.UUID,
) ([]Category, error) {
	return nil, nil
}

func (r *categoryPathRepository) ClassifyListing(
	context.Context,
	uuid.UUID,
	uuid.UUID,
	string,
) error {
	return nil
}

func (r *categoryPathRepository) FindCategory(
	_ context.Context,
	_ uuid.UUID,
	parentID *uuid.UUID,
	_ string,
	_ string,
) (*Category, error) {
	r.parents = append(r.parents, cloneUUID(parentID))
	return nil, nil
}

func (r *categoryPathRepository) CreateCategory(
	_ context.Context,
	_ uuid.UUID,
	parentID *uuid.UUID,
	name, slug, description string,
	depth int16,
	_ []float32,
) (*Category, error) {
	id := r.categoryIDs[int(depth)]
	return &Category{
		ID:          id,
		ParentID:    cloneUUID(parentID),
		Name:        name,
		Slug:        slug,
		Description: description,
		Depth:       depth,
		CreatedAt:   time.Now(),
	}, nil
}

func (r *categoryPathRepository) AttachCategory(
	_ context.Context,
	_ uuid.UUID,
	categoryID uuid.UUID,
	_ uuid.UUID,
) error {
	r.attachedCategoryID = categoryID
	return nil
}

func (r *categoryPathRepository) UpsertGigDetails(
	context.Context,
	uuid.UUID,
	*extractor.ExtractedGigDetails,
	*time.Time,
) error {
	return nil
}

func (r *categoryPathRepository) UpsertSaleDetails(
	context.Context,
	uuid.UUID,
	*extractor.ExtractedSaleDetails,
	*time.Time,
) error {
	return nil
}

func (r *categoryPathRepository) OpenListing(context.Context, uuid.UUID) error {
	return nil
}

func cloneUUID(id *uuid.UUID) *uuid.UUID {
	if id == nil {
		return nil
	}
	cloned := *id
	return &cloned
}
