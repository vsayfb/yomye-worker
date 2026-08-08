package processor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
	"github.com/vsayfb/gig-platform-categorization-service/internal/extractor"
	"github.com/vsayfb/gig-platform-categorization-service/internal/notification"
)

type fakeNotificationPublisher struct {
	listingApprovedCalls int
	categoryMatchedCalls int
	listingApproved      notification.ListingApprovedEnvelope
	categoryMatched      notification.GigCategoryMatchedEventEnvelope
	categoryMatchedErr   error
}

func (f *fakeNotificationPublisher) PublishListingApproved(
	_ context.Context,
	event notification.ListingApprovedEnvelope,
) error {
	f.listingApprovedCalls++
	f.listingApproved = event
	return nil
}

func (f *fakeNotificationPublisher) PublishGigCategoryMatched(
	_ context.Context,
	event notification.GigCategoryMatchedEventEnvelope,
) error {
	f.categoryMatchedCalls++
	f.categoryMatched = event
	return f.categoryMatchedErr
}

func TestGigProcessor_AlreadyOpenReplayRepublishesNotifications(t *testing.T) {
	listingID := uuid.New()
	authorID := uuid.New()
	baseCategoryID := uuid.New()
	categoryID := uuid.New()

	repo := &fakeRepository{
		resolveBaseCategoryFn: func(context.Context, string) (uuid.UUID, error) {
			return baseCategoryID, nil
		},
		listActiveCategoriesFn: func(context.Context, uuid.UUID) ([]Category, error) {
			return nil, nil
		},
		lockListingFn: func(context.Context, uuid.UUID) (*ListingState, error) {
			return &ListingState{
				Status:           listingStatusOpen,
				BaseCategorySlug: BaseCategoryGig,
				Category:         &Category{ID: categoryID},
				Title:            "stored title",
			}, nil
		},
	}

	fallback := extractor.NewFallbackExtractor(&fakeExtractor{
		extract: func(context.Context, string, string, string) (*extractor.ExtractedListing, error) {
			return &extractor.ExtractedListing{
				BaseCategorySlug: BaseCategoryGig,
				SourceLocale:     "en",
				CategoryPath: []extractor.ExtractedCategoryNode{
					testCategoryNode("Plumber", "Plumber", true),
				},
				Gig: &extractor.ExtractedGigDetails{
					Title: "extracted title",
				},
				GeneratedBy: "fake:test",
			}, nil
		},
	})

	publisher := &fakeNotificationPublisher{}
	proc := NewGigProcessor(
		repo,
		&fakeEmbeddingClient{},
		fallback,
		publisher,
		&config.Config{},
	)

	body, err := json.Marshal(ListingCreatedMessage{
		ListingID:   listingID,
		Description: "Need a plumber",
		AuthorID:    authorID,
	})
	if err != nil {
		t.Fatalf("marshal listing message: %v", err)
	}

	if err := proc.Process(context.Background(), Message{
		ID:   "message-1",
		Type: "ListingCreated",
		Body: string(body),
	}); err != nil {
		t.Fatalf("process already-open listing: %v", err)
	}

	if publisher.listingApprovedCalls != 1 {
		t.Fatalf("listing-approved calls = %d, want 1", publisher.listingApprovedCalls)
	}
	if publisher.categoryMatchedCalls != 1 {
		t.Fatalf("category-matched calls = %d, want 1", publisher.categoryMatchedCalls)
	}
	if publisher.listingApproved.Payload.Title != "stored title" {
		t.Fatalf("listing-approved title = %q, want stored title", publisher.listingApproved.Payload.Title)
	}
	if publisher.categoryMatched.Payload.CategoryID != categoryID {
		t.Fatalf("category ID = %s, want %s", publisher.categoryMatched.Payload.CategoryID, categoryID)
	}
}

func TestGigProcessor_AlreadyOpenPublishFailureCanRecoverOnReplay(t *testing.T) {
	listingID := uuid.New()
	authorID := uuid.New()
	baseCategoryID := uuid.New()
	categoryID := uuid.New()

	repo := &fakeRepository{
		resolveBaseCategoryFn: func(context.Context, string) (uuid.UUID, error) {
			return baseCategoryID, nil
		},
		listActiveCategoriesFn: func(context.Context, uuid.UUID) ([]Category, error) {
			return nil, nil
		},
		lockListingFn: func(context.Context, uuid.UUID) (*ListingState, error) {
			return &ListingState{
				Status:           listingStatusOpen,
				BaseCategorySlug: BaseCategoryGig,
				Category:         &Category{ID: categoryID},
				Title:            "stored title",
			}, nil
		},
	}

	fallback := extractor.NewFallbackExtractor(&fakeExtractor{
		extract: func(context.Context, string, string, string) (*extractor.ExtractedListing, error) {
			return &extractor.ExtractedListing{
				BaseCategorySlug: BaseCategoryGig,
				SourceLocale:     "en",
				CategoryPath: []extractor.ExtractedCategoryNode{
					testCategoryNode("Plumber", "Plumber", true),
				},
				Gig:         &extractor.ExtractedGigDetails{Title: "extracted title"},
				GeneratedBy: "fake:test",
			}, nil
		},
	})

	publisher := &fakeNotificationPublisher{categoryMatchedErr: errors.New("sqs unavailable")}
	proc := NewGigProcessor(repo, &fakeEmbeddingClient{}, fallback, publisher, &config.Config{})

	body, err := json.Marshal(ListingCreatedMessage{
		ListingID:   listingID,
		Description: "Need a plumber",
		AuthorID:    authorID,
	})
	if err != nil {
		t.Fatalf("marshal listing message: %v", err)
	}
	record := Message{ID: "message-1", Type: "ListingCreated", Body: string(body)}

	if err := proc.Process(context.Background(), record); err == nil {
		t.Fatal("expected notification publication failure")
	}

	publisher.categoryMatchedErr = nil
	if err := proc.Process(context.Background(), record); err != nil {
		t.Fatalf("replay after notification recovery: %v", err)
	}

	if publisher.listingApprovedCalls != 2 {
		t.Fatalf("listing-approved calls = %d, want 2", publisher.listingApprovedCalls)
	}
	if publisher.categoryMatchedCalls != 2 {
		t.Fatalf("category-matched calls = %d, want 2", publisher.categoryMatchedCalls)
	}
}
