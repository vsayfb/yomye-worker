package processor

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
	"github.com/vsayfb/gig-platform-categorization-service/internal/extractor"
	"github.com/vsayfb/gig-platform-categorization-service/internal/notification"
)

func TestGigProcessor_ModerationFailureDoesNotRejectOrPublish(t *testing.T) {
	var databaseTouched bool
	repo := &fakeRepository{
		runInTxFn: func(context.Context, func(context.Context) error) error {
			databaseTouched = true
			return nil
		},
	}
	fallback := extractor.NewFallbackExtractor(&fakeExtractor{
		moderate: func(context.Context, string) (*extractor.ModerationResult, error) {
			return nil, extractor.ErrRateLimited
		},
		extract: func(context.Context, string, string, string) (*extractor.ExtractedListing, error) {
			t.Fatal("taxonomy extraction must not run after moderation failure")
			return nil, nil
		},
	})
	publisher := &fakeNotificationPublisher{}
	processor := NewGigProcessor(repo, &fakeEmbeddingClient{}, fallback, publisher, &config.Config{})

	err := processor.processListing(context.Background(), ListingCreatedMessage{
		ListingID:   uuid.New(),
		AuthorID:    uuid.New(),
		CountryID:   uuid.New(),
		Description: "content",
	})
	if err == nil || !errors.Is(err, extractor.ErrRateLimited) {
		t.Fatalf("processListing() error = %v, want rate-limit failure", err)
	}
	if IsPermanent(err) {
		t.Fatal("moderation dependency failure must remain retryable")
	}
	if databaseTouched {
		t.Fatal("moderation dependency failure touched database state")
	}
	if publisher.contentRejectedCalls != 0 {
		t.Fatal("moderation dependency failure emitted rejection event")
	}
}

func TestGigProcessor_ExplicitModerationRejectSetsFailedAndPublishes(t *testing.T) {
	listingID := uuid.New()
	authorID := uuid.New()
	var rejectedID uuid.UUID
	repo := &fakeRepository{
		lockListingFn: func(context.Context, uuid.UUID) (*ListingState, error) {
			return &ListingState{Status: listingStatusProcessing}, nil
		},
		rejectListingFn: func(_ context.Context, got uuid.UUID) error {
			rejectedID = got
			return nil
		},
	}
	fallback := extractor.NewFallbackExtractor(&fakeExtractor{
		generator: "moderator:model",
		moderate: func(context.Context, string) (*extractor.ModerationResult, error) {
			return &extractor.ModerationResult{
				Decision:    extractor.ModerationReject,
				Reason:      "alcohol",
				GeneratedBy: "moderator:model",
			}, nil
		},
		extract: func(context.Context, string, string, string) (*extractor.ExtractedListing, error) {
			t.Fatal("taxonomy extraction must not run for rejected content")
			return nil, nil
		},
	})
	publisher := &fakeNotificationPublisher{}
	processor := NewGigProcessor(repo, &fakeEmbeddingClient{}, fallback, publisher, &config.Config{})

	err := processor.processListing(context.Background(), ListingCreatedMessage{
		ListingID:   listingID,
		AuthorID:    authorID,
		CountryID:   uuid.New(),
		Description: "whisky delivery",
	})
	if err != nil {
		t.Fatalf("processListing() error = %v", err)
	}
	if rejectedID != listingID {
		t.Fatalf("failed listing ID = %s, want %s", rejectedID, listingID)
	}
	if publisher.contentRejectedCalls != 1 {
		t.Fatalf("content rejection events = %d, want 1", publisher.contentRejectedCalls)
	}
	event := publisher.contentRejected
	if event.Payload.SubjectType != notification.RejectedSubjectListing ||
		event.Payload.SubjectID != listingID ||
		event.Payload.RecipientID != authorID ||
		event.Payload.Reason != "alcohol" {
		t.Fatalf("rejection event = %#v", event)
	}
	if event.EventID != notification.StableEventID(notification.EventContentRejected, listingID) {
		t.Fatalf("event ID = %s, want deterministic ID", event.EventID)
	}
}

func TestProfessionProcessor_ExplicitModerationRejectKeepsCategoryAndPublishes(t *testing.T) {
	requestID := uuid.New()
	authorID := uuid.New()
	repo := &fakeRepository{
		getCurrentProfessionRequestFn: func(context.Context, uuid.UUID) (uuid.UUID, bool, error) {
			return requestID, true, nil
		},
		resolveBaseCategoryFn: func(context.Context, string) (uuid.UUID, error) {
			t.Fatal("category work must not run for rejected profession content")
			return uuid.Nil, nil
		},
	}
	fallback := extractor.NewFallbackExtractor(&fakeExtractor{
		generator: "moderator:model",
		moderate: func(context.Context, string) (*extractor.ModerationResult, error) {
			return &extractor.ModerationResult{
				Decision:    extractor.ModerationReject,
				Reason:      "controlled_substances",
				GeneratedBy: "moderator:model",
			}, nil
		},
	})
	publisher := &fakeNotificationPublisher{}
	processor := NewProfessionProcessor(
		repo, &fakeEmbeddingClient{}, fallback, publisher, &config.Config{},
	)

	err := processor.Process(context.Background(), Message{
		ID:   uuid.NewString(),
		Type: "ProfessionPublished",
		Body: `{"request_id":"` + requestID.String() + `","author_id":"` + authorID.String() + `","description":"restricted offer"}`,
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if repo.upsertUserProfessionCategoryCalls != 0 {
		t.Fatal("rejected profession changed the user's category")
	}
	if publisher.contentRejectedCalls != 1 {
		t.Fatalf("content rejection events = %d, want 1", publisher.contentRejectedCalls)
	}
	if event := publisher.contentRejected; event.Payload.SubjectType != notification.RejectedSubjectProfession ||
		event.Payload.SubjectID != requestID || event.Payload.Reason != "controlled_substances" {
		t.Fatalf("rejection event = %#v", event)
	}
}
