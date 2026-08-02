package processor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
	"github.com/vsayfb/gig-platform-categorization-service/internal/extractor"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/dbtx"
)

type Category struct {
	ID                   uuid.UUID
	ParentID             *uuid.UUID
	CanonicalName        string
	CanonicalSlug        string
	CanonicalDescription string
	IsSelectable         bool
	IsActive             bool
	Depth                int16
	CreatedAt            time.Time
}

type CategoryLocalization struct {
	CategoryID  uuid.UUID
	Locale      string
	Name        string
	Slug        string
	Description string
	GeneratedBy string
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
	ListActiveCategories(ctx context.Context, baseCategoryID uuid.UUID) ([]Category, error)
	ClassifyListing(ctx context.Context, listingID, baseCategoryID uuid.UUID, descriptionClean string) error
	FindCategory(ctx context.Context, baseCategoryID uuid.UUID, parentID *uuid.UUID, canonicalName, canonicalSlug string, embedding []float32) (*Category, error)
	CreateCategory(ctx context.Context, baseCategoryID uuid.UUID, parentID *uuid.UUID, canonicalName, canonicalSlug, canonicalDescription, generatedBy string, isSelectable bool, depth int16, embedding []float32) (*Category, error)
	PromoteCategorySelectable(ctx context.Context, categoryID, baseCategoryID uuid.UUID) (*Category, error)
	EnsureCategoryLocalization(ctx context.Context, localization CategoryLocalization) error
	AttachCategory(ctx context.Context, listingID, categoryID, baseCategoryID uuid.UUID) error
	AttachUserCategory(ctx context.Context, userID, categoryID uuid.UUID) error
	UpsertGigDetails(ctx context.Context, listingID uuid.UUID, details *extractor.ExtractedGigDetails, expiresAt *time.Time) error
	UpsertSaleDetails(ctx context.Context, listingID uuid.UUID, details *extractor.ExtractedSaleDetails, expiresAt *time.Time) error
	OpenListing(ctx context.Context, listingID uuid.UUID) error
	GetCurrentProfessionRequest(context.Context, uuid.UUID) (uuid.UUID, bool, error)
	LockCurrentProfessionRequest(context.Context, uuid.UUID) (uuid.UUID, bool, error)
	UpsertUserProfessionCategory(context.Context, uuid.UUID, uuid.UUID) error
}

type repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *repository {
	return &repository{db: db}
}

func (r *repository) GetCurrentProfessionRequest(
	ctx context.Context,
	userID uuid.UUID,
) (uuid.UUID, bool, error) {
	var requestID *uuid.UUID

	err := dbtx.Extract(ctx, r.db).QueryRow(ctx, `
		SELECT profession_request_id
		FROM users
		WHERE id = $1
	`, userID).Scan(&requestID)

	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf(
			"get current profession request: %w",
			err,
		)
	}

	if requestID == nil {
		return uuid.Nil, true, nil
	}

	return *requestID, true, nil
}

func (r *repository) LockCurrentProfessionRequest(
	ctx context.Context,
	userID uuid.UUID,
) (uuid.UUID, bool, error) {
	var requestID *uuid.UUID

	err := dbtx.Extract(ctx, r.db).QueryRow(ctx, `
		SELECT profession_request_id
		FROM users
		WHERE id = $1
		FOR UPDATE
	`, userID).Scan(&requestID)

	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf(
			"lock current profession request: %w",
			err,
		)
	}

	if requestID == nil {
		return uuid.Nil, true, nil
	}

	return *requestID, true, nil
}

func (r *repository) UpsertUserProfessionCategory(
	ctx context.Context,
	userID uuid.UUID,
	categoryID uuid.UUID,
) error {
	tag, err := dbtx.Extract(ctx, r.db).Exec(ctx, `
		INSERT INTO user_categories (
			user_id,
			category_id
		)
		SELECT $1, c.id
		FROM categories c
		JOIN base_categories bc ON bc.id = c.base_category_id
		WHERE c.id = $2
		  AND c.is_active = TRUE
		  AND c.is_selectable = TRUE
		  AND bc.slug = 'gig'
		  AND bc.is_active = TRUE
		ON CONFLICT (user_id)
		DO UPDATE SET
			category_id = EXCLUDED.category_id
	`, userID, categoryID)
	if err != nil {
		return fmt.Errorf("upsert user profession category: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf(
			"upsert user profession category: category is not an active selectable gig category (affected rows: %d)",
			tag.RowsAffected(),
		)
	}

	return nil
}

func (r *repository) AttachUserCategory(
	ctx context.Context,
	userID uuid.UUID,
	categoryID uuid.UUID,
) error {
	_, err := dbtx.Extract(ctx, r.db).Exec(ctx, `
		INSERT INTO user_categories (
			user_id,
			category_id
		)
		VALUES ($1, $2)
		ON CONFLICT (user_id, category_id) DO NOTHING
	`, userID, categoryID)
	if err != nil {
		return fmt.Errorf("attach category to user: %w", err)
	}

	return nil
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
			SELECT c.id, c.parent_id, c.canonical_name, c.canonical_slug, c.canonical_description,
			       c.is_selectable, c.is_active, c.depth, c.created_at,
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
			&state.Category.ParentID,
			&state.Category.CanonicalName,
			&state.Category.CanonicalSlug,
			&state.Category.CanonicalDescription,
			&state.Category.IsSelectable,
			&state.Category.IsActive,
			&state.Category.Depth,
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

func (r *repository) ListActiveCategories(
	ctx context.Context,
	baseCategoryID uuid.UUID,
) ([]Category, error) {
	rows, err := dbtx.Extract(ctx, r.db).Query(ctx, `
		WITH RECURSIVE tree AS (
			SELECT c.id, c.parent_id, c.canonical_name, c.canonical_slug,
			       c.canonical_description, c.is_selectable, c.is_active, c.depth,
			       c.created_at, ARRAY[c.canonical_slug]::text[] AS sort_path
			FROM categories c
			WHERE c.base_category_id = $1
			  AND c.parent_id IS NULL
			  AND c.is_active = TRUE

			UNION ALL

			SELECT c.id, c.parent_id, c.canonical_name, c.canonical_slug,
			       c.canonical_description,
			       c.is_selectable, c.is_active, c.depth,
			       c.created_at, tree.sort_path || c.canonical_slug
			FROM categories c
			JOIN tree ON c.parent_id = tree.id
			WHERE c.base_category_id = $1
			  AND c.is_active = TRUE
		)
		SELECT id, parent_id, canonical_name, canonical_slug, canonical_description,
		       is_selectable, is_active,
		       depth, created_at
		FROM tree
		ORDER BY sort_path
	`, baseCategoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []Category
	for rows.Next() {
		var category Category
		if err := rows.Scan(
			&category.ID,
			&category.ParentID,
			&category.CanonicalName,
			&category.CanonicalSlug,
			&category.CanonicalDescription,
			&category.IsSelectable,
			&category.IsActive,
			&category.Depth,
			&category.CreatedAt,
		); err != nil {
			return nil, err
		}
		categories = append(categories, category)
	}
	return categories, rows.Err()
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

func (r *repository) FindCategory(
	ctx context.Context,
	baseCategoryID uuid.UUID,
	parentID *uuid.UUID,
	canonicalName, canonicalSlug string,
	embedding []float32,
) (*Category, error) {
	scope := baseCategoryID.String() + ":root"
	if parentID != nil {
		scope = baseCategoryID.String() + ":" + parentID.String()
	}
	if _, err := dbtx.Extract(ctx, r.db).Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		scope,
	); err != nil {
		return nil, fmt.Errorf("lock category scope: %w", err)
	}

	var category Category
	vec := pgvector.NewVector(embedding)
	err := dbtx.Extract(ctx, r.db).QueryRow(ctx, `
		SELECT c.id, c.parent_id, c.canonical_name, c.canonical_slug,
		       c.canonical_description,
		       c.is_selectable, c.is_active, c.depth, c.created_at
		FROM categories c
		WHERE c.base_category_id = $1
		  AND c.parent_id IS NOT DISTINCT FROM $2
		  AND c.is_active = TRUE
		  AND (
		      c.canonical_slug = $3
		      OR lower(c.canonical_name) = lower($4)
		      OR (c.embedding IS NOT NULL AND (c.embedding <=> $5) < 0.15)
		  )
		ORDER BY (c.canonical_slug = $3) DESC,
		         (lower(c.canonical_name) = lower($4)) DESC,
		         c.embedding <=> $5
		LIMIT 1
	`, baseCategoryID, parentID, canonicalSlug, canonicalName, vec).Scan(
		&category.ID,
		&category.ParentID,
		&category.CanonicalName,
		&category.CanonicalSlug,
		&category.CanonicalDescription,
		&category.IsSelectable,
		&category.IsActive,
		&category.Depth,
		&category.CreatedAt,
	)
	if err == nil {
		return &category, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return nil, err
}

func (r *repository) CreateCategory(
	ctx context.Context,
	baseCategoryID uuid.UUID,
	parentID *uuid.UUID,
	canonicalName, canonicalSlug, canonicalDescription, generatedBy string,
	isSelectable bool,
	depth int16,
	embedding []float32,
) (*Category, error) {
	cat := &Category{}
	vec := pgvector.NewVector(embedding)

	err := dbtx.Extract(ctx, r.db).QueryRow(ctx, `
		INSERT INTO categories (
			id, base_category_id, parent_id, canonical_name, canonical_slug,
			canonical_description,
			generated_by, is_selectable, depth, embedding
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT DO NOTHING
		RETURNING id, parent_id, canonical_name, canonical_slug,
		          canonical_description, is_selectable, is_active, depth, created_at
	`, uuid.New(), baseCategoryID, parentID, canonicalName, canonicalSlug,
		canonicalDescription, generatedBy,
		isSelectable, depth, vec).Scan(
		&cat.ID,
		&cat.ParentID,
		&cat.CanonicalName,
		&cat.CanonicalSlug,
		&cat.CanonicalDescription,
		&cat.IsSelectable,
		&cat.IsActive,
		&cat.Depth,
		&cat.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		err = dbtx.Extract(ctx, r.db).QueryRow(ctx, `
			UPDATE categories
			SET canonical_description = $5,
			    embedding = $6,
			    is_selectable = is_selectable OR $7,
			    is_active = TRUE,
			    updated_at = NOW()
			WHERE id = (
				SELECT id
				FROM categories
				WHERE base_category_id = $1
				  AND parent_id IS NOT DISTINCT FROM $2
				  AND (canonical_slug = $3 OR canonical_name = $4)
				ORDER BY (canonical_slug = $3) DESC
				LIMIT 1
			)
			RETURNING id, parent_id, canonical_name, canonical_slug,
			          canonical_description, is_selectable,
			          is_active, depth, created_at
		`, baseCategoryID, parentID, canonicalSlug, canonicalName,
			canonicalDescription, vec, isSelectable).Scan(
			&cat.ID,
			&cat.ParentID,
			&cat.CanonicalName,
			&cat.CanonicalSlug,
			&cat.CanonicalDescription,
			&cat.IsSelectable,
			&cat.IsActive,
			&cat.Depth,
			&cat.CreatedAt,
		)
	}
	if err != nil {
		return nil, err
	}
	return cat, nil
}

func (r *repository) PromoteCategorySelectable(
	ctx context.Context,
	categoryID, baseCategoryID uuid.UUID,
) (*Category, error) {
	category := &Category{}
	err := dbtx.Extract(ctx, r.db).QueryRow(ctx, `
		UPDATE categories
		SET is_selectable = TRUE,
		    updated_at = CASE WHEN is_selectable THEN updated_at ELSE NOW() END
		WHERE id = $1
		  AND base_category_id = $2
		  AND is_active = TRUE
		RETURNING id, parent_id, canonical_name, canonical_slug,
		          canonical_description, is_selectable,
		          is_active, depth, created_at
	`, categoryID, baseCategoryID).Scan(
		&category.ID,
		&category.ParentID,
		&category.CanonicalName,
		&category.CanonicalSlug,
		&category.CanonicalDescription,
		&category.IsSelectable,
		&category.IsActive,
		&category.Depth,
		&category.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return category, nil
}

func (r *repository) EnsureCategoryLocalization(
	ctx context.Context,
	localization CategoryLocalization,
) error {
	_, err := dbtx.Extract(ctx, r.db).Exec(ctx, `
		INSERT INTO category_localizations (
			category_id, locale, name, slug, description, generated_by
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (category_id, locale) DO NOTHING
	`, localization.CategoryID, localization.Locale, localization.Name,
		localization.Slug, localization.Description, localization.GeneratedBy)
	if err != nil {
		return fmt.Errorf("ensure category localization: %w", err)
	}
	return nil
}

func (r *repository) AttachCategory(
	ctx context.Context,
	listingID, categoryID, baseCategoryID uuid.UUID,
) error {
	tag, err := dbtx.Extract(ctx, r.db).Exec(ctx, `
		INSERT INTO listing_categories (listing_id, category_id, base_category_id)
		SELECT $1, c.id, c.base_category_id
		FROM categories c
		JOIN base_categories bc ON bc.id = c.base_category_id
		WHERE c.id = $2
		  AND c.base_category_id = $3
		  AND c.is_active = TRUE
		  AND c.is_selectable = TRUE
		  AND bc.slug = 'gig'
		  AND bc.is_active = TRUE
		ON CONFLICT (listing_id, category_id) DO UPDATE SET
			base_category_id = EXCLUDED.base_category_id
	`, listingID, categoryID, baseCategoryID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("category is not an active selectable gig category")
	}
	return nil
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
		      JOIN categories c ON c.id = lc.category_id
		      JOIN base_categories bc ON bc.id = c.base_category_id
		      WHERE lc.listing_id = listings.id
		        AND lc.base_category_id = listings.base_category_id
		        AND lc.base_category_id = c.base_category_id
		        AND c.is_active = TRUE
		        AND c.is_selectable = TRUE
		        AND bc.slug = 'gig'
		        AND bc.is_active = TRUE
		  )
		  AND base_category_id = base_category_id_for_slug('gig')
		  AND EXISTS (SELECT 1 FROM gig_details gd WHERE gd.gig_id = listings.id)
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
