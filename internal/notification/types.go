package notification

import (
	"time"

	"github.com/google/uuid"
)

type NotificationEventType string

const (
	EventGigApproved        NotificationEventType = "gig_approved"
	EventGigRejected        NotificationEventType = "gig_rejected"
	EventGigCategoryMatched NotificationEventType = "gig_category_matched"
)

type ApprovedEnvelope struct {
	Type    NotificationEventType           `json:"type"`
	Payload NewGigAprrovedNotificationEvent `json:"payload"`
}

type NewGigAprrovedNotificationEvent struct {
	RecipientID string    `json:"recipient_id"`
	GigID       string    `json:"gig_id"`
	GigTitle    string    `json:"gig_title"`
	OccurredAt  time.Time `json:"occurred_at"`
}

type RejectedEnvelope struct {
	Type    NotificationEventType           `json:"type"`
	Payload NewGigRejectedNotificationEvent `json:"payload"`
}

type NewGigRejectedNotificationEvent struct {
	RecipientID string    `json:"recipient_id"`
	GigID       string    `json:"gig_id"`
	OccurredAt  time.Time `json:"occurred_at"`
}

type GigCategoryMatchedEvent struct {
	GigID      uuid.UUID `json:"gig_id"`
	CategoryID uuid.UUID `json:"category_id"`
	CityID     int16     `json:"city_id"`
	OccurredAt time.Time `json:"occurred_at"`
}

type GigCategoryMatchedEventEnvelope struct {
	Type    NotificationEventType   `json:"type"`
	Payload GigCategoryMatchedEvent `json:"payload"`
}
