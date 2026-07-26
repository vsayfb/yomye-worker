package worker

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
	"github.com/vsayfb/gig-platform-categorization-service/internal/extractor"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/dbtx"
)

type Category struct {
	ID        uuid.UUID
	Name      string
	Slug      string
	CreatedAt time.Time
}

type ListingState struct {
	Status           string
	BaseCategorySlug string
	Category         *Category
	Title            string
}

type Repository interface {
	RunInTx(ctx context.Context, fn func(context.Context) error) error
	LockListing(ctx context.Context, listingID uuid.UUID) (*ListingState, error)
	ResolveBaseCategory(ctx context.Context, slug string) (uuid.UUID, error)
	ClassifyListing(ctx context.Context, listingID, baseCategoryID uuid.UUID, descriptionClean string) error
	FindSimilar(ctx context.Context, baseCategoryID uuid.UUID, embedding []float32, threshold float64) (*Category, error)
	CreateCategory(ctx context.Context, baseCategoryID uuid.UUID, name, slug string, embedding []float32) (*Category, error)
	AttachCategory(ctx context.Context, listingID, categoryID, baseCategoryID uuid.UUID) error
	UpsertGigDetails(ctx context.Context, listingID uuid.UUID, details *extractor.ExtractedGigDetails, expiresAt *time.Time) error
	UpsertSaleDetails(ctx context.Context, listingID uuid.UUID, details *extractor.ExtractedSaleDetails, expiresAt *time.Time) error
	OpenListing(ctx context.Context, listingID uuid.UUID) error
}

type repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *repository {
	return &repository{db: db}
}

func (r *repository) LockListing(ctx context.Context, listingID uuid.UUID) (*ListingState, error) {
	var state ListingState
	var baseSlug *string

	err := dbtx.Extract(ctx, r.db).QueryRow(ctx, `
		SELECT l.status::text, bc.slug
		FROM listings l
		LEFT JOIN base_categories bc ON bc.id = l.base_category_id
		WHERE l.id = $1
		FOR UPDATE OF l
	`, listingID).Scan(&state.Status, &baseSlug)
	if err != nil {
		return nil, err
	}
	if baseSlug != nil {
		state.BaseCategorySlug = *baseSlug
	}

	if state.Status == "OPEN" {
		state.Category = &Category{}
		err = dbtx.Extract(ctx, r.db).QueryRow(ctx, `
			SELECT c.id, c.name, c.slug, c.created_at,
			       COALESCE(gd.title, sd.title)
			FROM listing_categories lc
			JOIN categories c ON c.id = lc.category_id
			LEFT JOIN gig_details gd ON gd.gig_id = lc.listing_id
			LEFT JOIN sale_details sd ON sd.listing_id = lc.listing_id
			WHERE lc.listing_id = $1
			  AND lc.base_category_id = c.base_category_id
			ORDER BY c.created_at
			LIMIT 1
		`, listingID).Scan(
			&state.Category.ID,
			&state.Category.Name,
			&state.Category.Slug,
			&state.Category.CreatedAt,
			&state.Title,
		)
		if err != nil {
			return nil, fmt.Errorf("load open listing classification: %w", err)
		}
	}

	return &state, nil
}

func (r *repository) ResolveBaseCategory(ctx context.Context, slug string) (uuid.UUID, error) {
	var id uuid.UUID
	err := dbtx.Extract(ctx, r.db).QueryRow(ctx, `
		SELECT id
		FROM base_categories
		WHERE slug = $1 AND is_active = TRUE
	`, slug).Scan(&id)
	return id, err
}

func (r *repository) ClassifyListing(
	ctx context.Context,
	listingID, baseCategoryID uuid.UUID,
	descriptionClean string,
) error {
	tag, err := dbtx.Extract(ctx, r.db).Exec(ctx, `
		UPDATE listings
		SET base_category_id = $2,
		    description_clean = $3
		WHERE id = $1 AND status = 'PROCESSING'
	`, listingID, baseCategoryID, descriptionClean)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("listing is not PROCESSING")
	}
	return nil
}

func (r *repository) FindSimilar(
	ctx context.Context,
	baseCategoryID uuid.UUID,
	embedding []float32,
	threshold float64,
) (*Category, error) {
	vec := pgvector.NewVector(embedding)
	var cat Category
	var storedVec pgvector.Vector

	err := dbtx.Extract(ctx, r.db).QueryRow(ctx, `
		SELECT c.id, c.name, c.slug, c.created_at, c.embedding
		FROM categories c
		WHERE c.base_category_id = $1
		ORDER BY c.embedding <=> $2
		LIMIT 1
	`, baseCategoryID, vec).Scan(&cat.ID, &cat.Name, &cat.Slug, &cat.CreatedAt, &storedVec)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if cosineSimilarity(embedding, storedVec.Slice()) < threshold {
		return nil, nil
	}
	return &cat, nil
}

func (r *repository) CreateCategory(
	ctx context.Context,
	baseCategoryID uuid.UUID,
	name, slug string,
	embedding []float32,
) (*Category, error) {
	cat := &Category{}
	vec := pgvector.NewVector(embedding)

	err := dbtx.Extract(ctx, r.db).QueryRow(ctx, `
		INSERT INTO categories (id, base_category_id, name, slug, embedding)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (base_category_id, slug)
		DO UPDATE SET name = EXCLUDED.name
		RETURNING id, name, slug, created_at
	`, uuid.New(), baseCategoryID, name, slug, vec).Scan(
		&cat.ID, &cat.Name, &cat.Slug, &cat.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return cat, nil
}

func (r *repository) AttachCategory(
	ctx context.Context,
	listingID, categoryID, baseCategoryID uuid.UUID,
) error {
	_, err := dbtx.Extract(ctx, r.db).Exec(ctx, `
		INSERT INTO listing_categories (listing_id, category_id, base_category_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (listing_id, category_id) DO NOTHING
	`, listingID, categoryID, baseCategoryID)
	return err
}

func (r *repository) UpsertGigDetails(
	ctx context.Context,
	listingID uuid.UUID,
	d *extractor.ExtractedGigDetails,
	expiresAt *time.Time,
) error {
	_, err := dbtx.Extract(ctx, r.db).Exec(ctx, `
		INSERT INTO gig_details (
			gig_id, title, duration_type, pay_amount, pay_currency, expires_at
		)
		VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6)
		ON CONFLICT (gig_id) DO UPDATE SET
			title = EXCLUDED.title,
			duration_type = EXCLUDED.duration_type,
			pay_amount = EXCLUDED.pay_amount,
			pay_currency = EXCLUDED.pay_currency,
			expires_at = EXCLUDED.expires_at,
			updated_at = NOW()
	`, listingID, d.Title, d.DurationType, d.PayAmount, d.PayCurrency, expiresAt)
	return err
}

func (r *repository) UpsertSaleDetails(
	ctx context.Context,
	listingID uuid.UUID,
	d *extractor.ExtractedSaleDetails,
	expiresAt *time.Time,
) error {
	_, err := dbtx.Extract(ctx, r.db).Exec(ctx, `
		INSERT INTO sale_details (
			listing_id, title, price_amount, price_currency, pricing_type,
			condition, brand, model, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (listing_id) DO UPDATE SET
			title = EXCLUDED.title,
			price_amount = EXCLUDED.price_amount,
			price_currency = EXCLUDED.price_currency,
			pricing_type = EXCLUDED.pricing_type,
			condition = EXCLUDED.condition,
			brand = EXCLUDED.brand,
			model = EXCLUDED.model,
			expires_at = EXCLUDED.expires_at,
			updated_at = NOW()
	`, listingID, d.Title, d.PriceAmount, d.PriceCurrency, d.PricingType,
		d.Condition, d.Brand, d.Model, expiresAt)
	return err
}

func (r *repository) OpenListing(ctx context.Context, listingID uuid.UUID) error {
	tag, err := dbtx.Extract(ctx, r.db).Exec(ctx, `
		UPDATE listings
		SET status = 'OPEN'
		WHERE id = $1
		  AND status = 'PROCESSING'
		  AND base_category_id IS NOT NULL
		  AND EXISTS (
		      SELECT 1 FROM listing_categories lc
		      WHERE lc.listing_id = listings.id
		        AND lc.base_category_id = listings.base_category_id
		  )
		  AND (
		      (
		          base_category_id = base_category_id_for_slug('gig')
		          AND EXISTS (SELECT 1 FROM gig_details gd WHERE gd.gig_id = listings.id)
		      )
		      OR
		      (
		          base_category_id = base_category_id_for_slug('for-sale')
		          AND EXISTS (SELECT 1 FROM sale_details sd WHERE sd.listing_id = listings.id)
		      )
		  )
	`, listingID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("listing prerequisites are incomplete")
	}
	return nil
}

func (r *repository) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	return dbtx.RunInTx(ctx, r.db, fn)
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
