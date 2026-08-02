package processor

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// TestProfessionProcessor_E2E_CategorizesAndLinksUser runs the real
// pipeline for a profession submission: live AI extraction, real
// embeddings, and real persistence, then checks the resulting category is
// actually linked to the user.
func TestProfessionProcessor_E2E_CategorizesAndLinksUser(t *testing.T) {
	env := newE2EEnv(t)
	ctx := context.Background()

	env.seedBaseCategories(t, ctx)

	requestID := uuid.New()
	userID := env.seedUser(t, ctx, requestID)

	msg := NewProfession{
		RequestID:   requestID,
		AuthorID:    userID,
		Description: "Freelance graphic designer specializing in logo design and brand identity for small businesses.",
	}
	body, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	record := Message{ID: uuid.New().String(), Type: "ProfessionPublished", Body: string(body)}

	if err := env.profession.Process(ctx, record); err != nil {
		t.Fatalf("process profession: %v", err)
	}

	var categoryID uuid.UUID
	if err := env.db.QueryRow(ctx,
		`SELECT category_id FROM user_categories WHERE user_id = $1`, userID,
	).Scan(&categoryID); err != nil {
		t.Fatalf("expected a linked profession category, got: %v", err)
	}
	if categoryID == uuid.Nil {
		t.Fatalf("linked category id is nil")
	}

	var selectable bool
	if err := env.db.QueryRow(ctx, `
		SELECT c.is_selectable
		FROM categories c
		JOIN base_categories bc ON bc.id = c.base_category_id
		WHERE c.id = $1
		  AND c.is_active = TRUE
		  AND bc.slug = 'gig'
		  AND bc.is_active = TRUE
	`, categoryID).Scan(&selectable); err != nil {
		t.Fatalf("read linked profession category: %v", err)
	}
	if !selectable {
		t.Fatal("user was associated with a non-selectable profession category")
	}

	// Idempotent replay: the same request_id is still current, so
	// reprocessing (e.g. SQS redelivery) must succeed and leave the user
	// pointed at a valid category rather than erroring or duplicating rows.
	replay := Message{ID: uuid.New().String(), Type: "ProfessionPublished", Body: string(body)}
	if err := env.profession.Process(ctx, replay); err != nil {
		t.Fatalf("reprocess profession: %v", err)
	}

	var categoryCountAfterReplay int
	if err := env.db.QueryRow(ctx,
		`SELECT count(*) FROM user_categories WHERE user_id = $1`, userID,
	).Scan(&categoryCountAfterReplay); err != nil {
		t.Fatalf("count user_categories after replay: %v", err)
	}
	if categoryCountAfterReplay != 1 {
		t.Fatalf("expected exactly 1 user_categories row after replay, got %d", categoryCountAfterReplay)
	}
}

// TestProfessionProcessor_E2E_IgnoresStaleRequest deterministically exercises
// the early staleness check (GetCurrentProfessionRequest): a message whose
// request_id doesn't match what's currently on the user row must be a no-op.
// This does NOT exercise the authoritative locked check inside the
// transaction - that path only fires on a genuine race between two
// in-flight requests and isn't something an e2e test can reliably land on;
// that stays covered by a unit test instead.
func TestProfessionProcessor_E2E_IgnoresStaleRequest(t *testing.T) {
	env := newE2EEnv(t)
	ctx := context.Background()

	env.seedBaseCategories(t, ctx)

	currentRequestID := uuid.New()
	userID := env.seedUser(t, ctx, currentRequestID)

	staleMsg := NewProfession{
		RequestID:   uuid.New(), // deliberately does not match currentRequestID
		AuthorID:    userID,
		Description: "This request has already been superseded by a newer one.",
	}
	body, err := json.Marshal(staleMsg)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	record := Message{ID: uuid.New().String(), Type: "ProfessionPublished", Body: string(body)}

	if err := env.profession.Process(ctx, record); err != nil {
		t.Fatalf("process stale profession: %v", err)
	}

	var count int
	if err := env.db.QueryRow(ctx,
		`SELECT count(*) FROM user_categories WHERE user_id = $1`, userID,
	).Scan(&count); err != nil {
		t.Fatalf("count user_categories: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected the stale request to be ignored, but a category was linked")
	}
}
