package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/vsayfb/gig-platform-categorization-service/internal/category"
	"github.com/vsayfb/gig-platform-categorization-service/internal/notification"
	"github.com/vsayfb/gig-platform-categorization-service/internal/worker"
)

type GigCreatedMessage struct {
	GigID       uuid.UUID `json:"gig_id"`
	Description string    `json:"description"`
	CityID      int16     `json:"city_id"`
	PosterID    uuid.UUID `json:"poster_id"`
}

func (a *App) Process(ctx context.Context, record worker.Message) error {

	slog.InfoContext(ctx, "worker record", "record", record)

	var msg GigCreatedMessage

	if err := json.Unmarshal([]byte(record.Body), &msg); err != nil {
		return err
	}

	slog.InfoContext(ctx, "unmarshalled message", "GigCreatedMessage", msg)

	// extract GigDetails text
	cat, err := a.categoryService.ProcessGig(ctx, msg.PosterID, msg.GigID, msg.Description)

	if err != nil {
		if errors.Is(err, category.ErrProfessionNotDetected) {
			slog.InfoContext(ctx, "profession not detected", "gig_id", msg.GigID)
			return nil
		}

		// err = a.notificationPublisher.PublishRejected(ctx, notification.RejectedEnvelope{
		// 	Type: notification.EventGigRejected,
		// 	Payload: notification.NewGigRejectedNotificationEvent{
		// 		RecipientID: msg.PosterID.String(),
		// 		GigID:       msg.GigID.String(),
		// 		OccurredAt:  time.Now(),
		// 	},
		// })

		// if err != nil {
		// 	slog.ErrorContext(ctx, "sqs message could not sent for (gig rejected):", "error", err)

		// }

		return err
	}

	slog.InfoContext(ctx, "categorized text", "category", cat)

	err = a.notificationPublisher.PublishApproved(ctx, notification.ApprovedEnvelope{
		Type: notification.EventGigApproved,
		Payload: notification.NewGigAprrovedNotificationEvent{
			GigTitle:    cat.Name,
			RecipientID: msg.PosterID.String(),
			GigID:       msg.GigID.String(),
			OccurredAt:  time.Now(),
		},
	})

	if err != nil {
		slog.ErrorContext(ctx, "sqs message could not sent for (gig_approved):", "error", err)
	}

	err = a.notificationPublisher.PublishGigCategoryMatched(ctx, notification.GigCategoryMatchedEventEnvelope{
		Type: notification.EventGigCategoryMatched,
		Payload: notification.GigCategoryMatchedEvent{
			GigID:      msg.GigID,
			CategoryID: cat.ID,
			CityID:     msg.CityID,
		},
	})

	if err != nil {
		slog.ErrorContext(ctx, "sqs message could not sent for (gig_category_matched):", "error", err)
	}

	return nil
}

func (a *App) CategorizationSQSQueue() string {
	return a.cfg.AWS.CategorizationEventsQueue
}
