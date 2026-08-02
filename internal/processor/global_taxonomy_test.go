package processor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/vsayfb/gig-platform-categorization-service/internal/extractor"
)

type taxonomyMemory struct {
	categories    map[string]*Category
	localizations map[uuid.UUID]map[string]CategoryLocalization
	created       int
}

func newTaxonomyMemory() *taxonomyMemory {
	return &taxonomyMemory{
		categories:    make(map[string]*Category),
		localizations: make(map[uuid.UUID]map[string]CategoryLocalization),
	}
}

func (m *taxonomyMemory) repository() *fakeRepository {
	return &fakeRepository{
		findCategoryFn: func(
			_ context.Context,
			_ uuid.UUID,
			parentID *uuid.UUID,
			_, canonicalSlug string,
			_ []float32,
		) (*Category, error) {
			return m.categories[taxonomyKey(parentID, canonicalSlug)], nil
		},
		createCategoryFn: func(
			_ context.Context,
			_ uuid.UUID,
			parentID *uuid.UUID,
			canonicalName, canonicalSlug, canonicalDescription, _ string,
			isSelectable bool,
			depth int16,
			_ []float32,
		) (*Category, error) {
			category := &Category{
				ID:                   uuid.New(),
				ParentID:             cloneUUID(parentID),
				CanonicalName:        canonicalName,
				CanonicalSlug:        canonicalSlug,
				CanonicalDescription: canonicalDescription,
				IsSelectable:         isSelectable,
				IsActive:             true,
				Depth:                depth,
			}
			m.categories[taxonomyKey(parentID, canonicalSlug)] = category
			m.created++
			return category, nil
		},
		ensureCategoryLocalizationFn: func(_ context.Context, localization CategoryLocalization) error {
			byLocale := m.localizations[localization.CategoryID]
			if byLocale == nil {
				byLocale = make(map[string]CategoryLocalization)
				m.localizations[localization.CategoryID] = byLocale
			}
			if _, exists := byLocale[localization.Locale]; !exists {
				byLocale[localization.Locale] = localization
			}
			return nil
		},
	}
}

func taxonomyKey(parentID *uuid.UUID, canonicalSlug string) string {
	parent := "root"
	if parentID != nil {
		parent = parentID.String()
	}
	return parent + ":" + canonicalSlug
}

func localizedNode(
	canonicalName, localizedName, locale string,
	selectable bool,
) extractor.ExtractedCategoryNode {
	node := testCategoryNode(canonicalName, localizedName, selectable)
	node.Locale = locale
	node.LocalizedDescription = localizedName + " profession"
	return node
}

func TestGlobalTaxonomyLanguagesReuseOneCanonicalCategory(t *testing.T) {
	memory := newTaxonomyMemory()
	processor := &GigProcessor{repo: memory.repository()}
	baseID := uuid.New()

	inputs := []extractor.ExtractedCategoryNode{
		localizedNode("Lawyer", "Avukat", "tr", true),
		localizedNode("Lawyer", "Lawyer", "en", true),
		localizedNode("Lawyer", "Rechtsanwalt", "de", true),
	}

	var categoryID uuid.UUID
	for _, node := range inputs {
		category, err := processor.findOrCreateCategory(
			context.Background(), baseID, nil, 0,
			embeddedCategoryNode{Node: node, Embedding: []float32{1, 0, 0}},
			"test-model",
		)
		if err != nil {
			t.Fatalf("resolve %s localization: %v", node.Locale, err)
		}
		if categoryID == uuid.Nil {
			categoryID = category.ID
		} else if category.ID != categoryID {
			t.Fatalf("locale %s created category %s, want shared %s", node.Locale, category.ID, categoryID)
		}
	}

	if memory.created != 1 {
		t.Fatalf("created %d canonical categories, want 1", memory.created)
	}
	category := memory.categories[taxonomyKey(nil, "lawyer")]
	if category == nil || category.CanonicalName != "Lawyer" || category.CanonicalSlug != "lawyer" {
		t.Fatalf("canonical category is not English Lawyer metadata: %#v", category)
	}
	if got := len(memory.localizations[categoryID]); got != 3 {
		t.Fatalf("created %d localizations, want tr/en/de", got)
	}
	for _, locale := range []string{"tr", "en", "de"} {
		if _, exists := memory.localizations[categoryID][locale]; !exists {
			t.Fatalf("missing %s localization", locale)
		}
	}
}

func TestExistingLocalizationIsReused(t *testing.T) {
	memory := newTaxonomyMemory()
	processor := &GigProcessor{repo: memory.repository()}
	baseID := uuid.New()
	node := localizedNode("Lawyer", "Avukat", "tr", true)

	for range 2 {
		if _, err := processor.findOrCreateCategory(
			context.Background(), baseID, nil, 0,
			embeddedCategoryNode{Node: node, Embedding: []float32{1, 0, 0}},
			"test-model",
		); err != nil {
			t.Fatalf("resolve Turkish lawyer: %v", err)
		}
	}

	category := memory.categories[taxonomyKey(nil, "lawyer")]
	if memory.created != 1 || len(memory.localizations[category.ID]) != 1 {
		t.Fatalf("created categories=%d localizations=%d, want 1 and 1", memory.created, len(memory.localizations[category.ID]))
	}
}

func TestGlobalTaxonomyCreatesCanonicalSpecializationChild(t *testing.T) {
	memory := newTaxonomyMemory()
	processor := &GigProcessor{repo: memory.repository()}
	baseID := uuid.New()

	lawyer, err := processor.findOrCreateCategory(
		context.Background(), baseID, nil, 0,
		embeddedCategoryNode{
			Node:      localizedNode("Lawyer", "Avukat", "tr", true),
			Embedding: []float32{1, 0, 0},
		},
		"test-model",
	)
	if err != nil {
		t.Fatalf("create lawyer: %v", err)
	}

	child, err := processor.findOrCreateCategory(
		context.Background(), baseID, &lawyer.ID, 1,
		embeddedCategoryNode{
			Node:      localizedNode("Immigration Lawyer", "Göçmenlik Avukatı", "tr", true),
			Embedding: []float32{0, 1, 0},
		},
		"test-model",
	)
	if err != nil {
		t.Fatalf("create specialization: %v", err)
	}
	if child.ID == lawyer.ID || child.ParentID == nil || *child.ParentID != lawyer.ID {
		t.Fatalf("specialization = %#v, want distinct child of %s", child, lawyer.ID)
	}
	if memory.created != 2 {
		t.Fatalf("created %d canonical categories, want lawyer plus specialization", memory.created)
	}
}

func TestResolveCategoryPathLocalizesEveryNode(t *testing.T) {
	memory := newTaxonomyMemory()
	processor := &GigProcessor{repo: memory.repository()}
	path := []extractor.ExtractedCategoryNode{
		localizedNode("Event Services", "Etkinlik Hizmetleri", "tr", false),
		localizedNode("Photography", "Fotoğrafçılık", "tr", false),
		localizedNode("Wedding Photographer", "Düğün Fotoğrafçısı", "tr", true),
	}
	prepared := &preparedListing{
		baseCategoryID: uuid.New(),
		extraction: &extractor.ExtractedListing{
			GeneratedBy:  "test-model",
			SourceLocale: "tr",
			CategoryPath: path,
		},
	}
	for _, node := range path {
		prepared.embeddedPath = append(prepared.embeddedPath, embeddedCategoryNode{
			Node: node, Embedding: []float32{1, 0, 0},
		})
	}

	leaf, err := processor.resolveCategoryPath(context.Background(), prepared)
	if err != nil {
		t.Fatalf("resolveCategoryPath() error = %v", err)
	}
	if leaf.CanonicalName != "Wedding Photographer" {
		t.Fatalf("leaf = %q, want Wedding Photographer", leaf.CanonicalName)
	}
	if memory.created != 3 || len(memory.localizations) != 3 {
		t.Fatalf("created categories=%d localized nodes=%d, want 3 and 3", memory.created, len(memory.localizations))
	}
}

type recordingCanonicalEmbeddingClient struct {
	texts []string
}

func (c *recordingCanonicalEmbeddingClient) Embed(_ context.Context, text string) ([]float32, error) {
	c.texts = append(c.texts, text)
	return []float32{1, 0, 0}, nil
}

func TestCategoryEmbeddingsUseCanonicalEnglishBreadcrumb(t *testing.T) {
	client := &recordingCanonicalEmbeddingClient{}
	processor := &GigProcessor{embeddingClient: client}
	path := []extractor.ExtractedCategoryNode{
		localizedNode("Legal Services", "Hukuk Hizmetleri", "tr", false),
		localizedNode("Lawyer", "Avukat", "tr", true),
	}

	if _, err := processor.embedCategoryPath(context.Background(), path); err != nil {
		t.Fatalf("embedCategoryPath() error = %v", err)
	}
	if len(client.texts) != 2 {
		t.Fatalf("embedding calls = %d, want 2", len(client.texts))
	}
	if !strings.Contains(client.texts[1], "Legal Services > Lawyer") ||
		strings.Contains(client.texts[1], "Avukat") {
		t.Fatalf("embedding text is not canonical English: %q", client.texts[1])
	}
}

func TestListingAssignsOnlyFinalSelectableCanonicalCategory(t *testing.T) {
	memory := newTaxonomyMemory()
	repo := memory.repository()
	listingID := uuid.New()
	baseID := uuid.New()
	var attached []uuid.UUID

	repo.lockListingFn = func(context.Context, uuid.UUID) (*ListingState, error) {
		return &ListingState{Status: listingStatusProcessing}, nil
	}
	repo.classifyListingFn = func(context.Context, uuid.UUID, uuid.UUID, string) error {
		return nil
	}
	repo.attachCategoryFn = func(_ context.Context, _, categoryID, _ uuid.UUID) error {
		attached = append(attached, categoryID)
		return nil
	}
	repo.upsertGigDetailsFn = func(context.Context, uuid.UUID, *extractor.ExtractedGigDetails, *time.Time) error {
		return nil
	}
	repo.openListingFn = func(context.Context, uuid.UUID) error { return nil }

	path := []extractor.ExtractedCategoryNode{
		localizedNode("Legal Services", "Hukuk Hizmetleri", "tr", false),
		localizedNode("Lawyer", "Avukat", "tr", true),
		localizedNode("Immigration Lawyer", "Göçmenlik Avukatı", "tr", true),
	}
	extracted := &extractor.ExtractedListing{
		BaseCategorySlug: BaseCategoryGig,
		SourceLocale:     "tr",
		GeneratedBy:      "test-model",
		CategoryPath:     path,
		Gig:              &extractor.ExtractedGigDetails{Title: "Göçmenlik avukatı arıyorum", PayCurrency: "TRY"},
	}
	prepared := &preparedListing{baseCategoryID: baseID, extraction: extracted}
	for _, node := range path {
		prepared.embeddedPath = append(prepared.embeddedPath, embeddedCategoryNode{
			Node: node, Embedding: []float32{1, 0, 0},
		})
	}
	result := newProcessResult(extracted)

	processor := &GigProcessor{repo: repo}
	if err := processor.persistListingInTx(
		context.Background(), listingID, "Göçmenlik avukatı arıyorum", prepared, result,
	); err != nil {
		t.Fatalf("persistListingInTx() error = %v", err)
	}
	if len(attached) != 1 || result.Category == nil || attached[0] != result.Category.ID {
		t.Fatalf("attached=%v result=%#v, want only final category", attached, result.Category)
	}
	if result.Category.CanonicalName != "Immigration Lawyer" || !result.Category.IsSelectable {
		t.Fatalf("assigned category = %#v, want selectable Immigration Lawyer", result.Category)
	}
}

func cloneUUID(id *uuid.UUID) *uuid.UUID {
	if id == nil {
		return nil
	}
	cloned := *id
	return &cloned
}
