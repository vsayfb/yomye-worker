package category

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
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

type GigDetails struct {
	GigID   uuid.UUID
	Details extractor.ExtractedGigDetails
}

type GigStatus string

type Repository interface {
	RunInTx(ctx context.Context, fn func(context.Context) error) error

	CreateGigDetails(ctx context.Context, d *GigDetails) error

	FindSimilar(ctx context.Context, embedding []float32, threshold float64) (*Category, error)

	CreateGigCategory(ctx context.Context, gigID, categoryID uuid.UUID) error

	UpdateGigStatus(ctx context.Context, gigID uuid.UUID, status GigStatus) error

	Create(ctx context.Context, name, slug string, embedding []float32) (*Category, error)
}

type repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *repository {
	return &repository{db: db}
}

func (r *repository) FindSimilar(ctx context.Context, embedding []float32, threshold float64) (*Category, error) {
	vec := pgvector.NewVector(embedding)

	var cat Category
	var storedVec pgvector.Vector

	row := r.db.QueryRow(ctx, `
		SELECT id, name, slug, created_at, embedding
		FROM categories
		ORDER BY embedding <=> $1
		LIMIT 1
	`, vec)

	if err := row.Scan(&cat.ID, &cat.Name, &cat.Slug, &cat.CreatedAt, &storedVec); err != nil {
		return nil, nil // no categories yet
	}

	if cosineSimilarity(embedding, storedVec.Slice()) < threshold {
		return nil, nil
	}

	return &cat, nil
}

func (r *repository) Create(ctx context.Context, name, slug string, embedding []float32) (*Category, error) {
	cat := &Category{
		ID:        uuid.New(),
		Name:      name,
		Slug:      slug,
		CreatedAt: time.Now(),
	}

	vec := pgvector.NewVector(embedding)

	_, err := r.db.Exec(ctx, `
		INSERT INTO categories (id, name, slug, embedding, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, cat.ID, cat.Name, cat.Slug, vec, cat.CreatedAt)

	if err != nil {
		return nil, err
	}

	return cat, nil
}

func (r *repository) CreateGigDetails(
	ctx context.Context,
	d *GigDetails,
) error {

	if d.Details.ExpiresAt == nil {
		t := time.Now().UTC().AddDate(0, 1, 0)
		d.Details.ExpiresAt = &t
	}

	if d.Details.PayCurrency == "" {
		d.Details.PayCurrency = "TRY"
	}

	_, err := r.db.Exec(ctx, `
		INSERT INTO gig_details (
			gig_id,
			title,
			duration_type,
			pay_amount,
			pay_currency,
			expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`,
		d.GigID,
		d.Details.Title,
		d.Details.DurationType,
		d.Details.PayAmount,
		d.Details.PayCurrency,
		d.Details.ExpiresAt,
	)

	return err
}

func (r *repository) UpdateGigStatus(
	ctx context.Context,
	gigID uuid.UUID,
	gigStatus GigStatus,
) error {

	cmd, err := dbtx.Extract(ctx, r.db).Exec(ctx, `
		UPDATE gigs
		SET status = $2
		WHERE id = $1
	`, gigID, gigStatus)

	if err != nil {
		return err
	}

	if cmd.RowsAffected() == 0 {
		return fmt.Errorf("gig not found")
	}

	return nil
}

func (r *repository) CreateGigCategory(
	ctx context.Context,
	gigID, categoryID uuid.UUID,
) error {

	_, err := dbtx.Extract(ctx, r.db).Exec(ctx, `
		INSERT INTO gig_categories (gig_id, category_id)
		VALUES ($1, $2)
	`, gigID, categoryID)

	return err
}

func (r *repository) RunInTx(
	ctx context.Context,
	fn func(context.Context) error,
) error {
	return dbtx.RunInTx(ctx, r.db, fn)
}

func cosineSimilarity(a, b []float32) float64 {
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
