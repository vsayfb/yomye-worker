package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/vsayfb/gig-platform-categorization-service/internal/notification"
	"github.com/vsayfb/gig-platform-categorization-service/internal/worker"
)

type ListingCreatedMessage struct {
	ListingID   uuid.UUID `json:"listing_id"`
	Description string    `json:"description"`
	CityID      int16     `json:"city_id"`
	AuthorID    uuid.UUID `json:"author_id"`
}

func (a *App) Process(ctx context.Context, record worker.Message) error {

	slog.InfoContext(ctx, "worker record", "record", record)

	var msg ListingCreatedMessage

	if err := json.Unmarshal([]byte(record.Body), &msg); err != nil {
		return err
	}
	if msg.ListingID == uuid.Nil || msg.AuthorID == uuid.Nil {
		return errors.New("listing-created event contains an invalid listing_id or author_id")
	}

	slog.InfoContext(ctx, "unmarshalled listing-created message", "listing_id", msg.ListingID)

	result, err := a.workerService.ProcessListing(ctx, msg.ListingID, msg.Description)

	if err != nil {
		if errors.Is(err, worker.ErrCategoryNotDetected) {
			slog.InfoContext(ctx, "category not detected", "listing_id", msg.ListingID)
			return nil
		}

		return err
	}

	slog.InfoContext(ctx,
		"listing categorized",
		"listing_id", msg.ListingID,
		"base_category", result.BaseCategorySlug,
		"category", result.Category,
		"already_open", result.AlreadyOpen,
	)

	if err := a.notificationPublisher.PublishListingApproved(
		ctx,
		notification.ListingApprovedEnvelope{
			Type: notification.EventListingApproved,
			Payload: notification.ListingApprovedEvent{
				RecipientID:      msg.AuthorID,
				ListingID:        msg.ListingID,
				BaseCategorySlug: result.BaseCategorySlug,
				Title:            result.Title,
				OccurredAt:       time.Now().UTC(),
			},
		},
	); err != nil {
		return fmt.Errorf("publish listing-approved event: %w", err)
	}

	// Profession subscription fan-out remains gig-specific.
	if result.BaseCategorySlug != worker.BaseCategoryGig {
		return nil
	}

	if err := a.notificationPublisher.PublishGigCategoryMatched(ctx, notification.GigCategoryMatchedEventEnvelope{
		Type: notification.EventGigCategoryMatched,
		Payload: notification.GigCategoryMatchedEvent{
			GigID:      msg.ListingID,
			CategoryID: result.Category.ID,
			CityID:     msg.CityID,
		},
	}); err != nil {
		return fmt.Errorf("publish gig-category-matched event: %w", err)
	}

	return nil
}

func (a *App) CategorizationSQSQueue() string {
	return a.cfg.AWS.CategorizationEventsQueue
}
