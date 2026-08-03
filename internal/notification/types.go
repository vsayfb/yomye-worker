package notification

import (
	"time"

	"github.com/google/uuid"
)

type NotificationEventType string

const (
	EventListingApproved    NotificationEventType = "listing_approved"
	EventGigRejected        NotificationEventType = "gig_rejected"
	EventGigCategoryMatched NotificationEventType = "gig_category_matched"
)

type ListingApprovedEnvelope struct {
	Type    NotificationEventType `json:"type"`
	Payload ListingApprovedEvent  `json:"payload"`
}

type ListingApprovedEvent struct {
	RecipientID      uuid.UUID `json:"recipient_id"`
	ListingID        uuid.UUID `json:"listing_id"`
	BaseCategorySlug string    `json:"base_category_slug"`
	Title            string    `json:"title"`
	OccurredAt       time.Time `json:"occurred_at"`
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
	GigID      uuid.UUID  `json:"gig_id"`
	CategoryID uuid.UUID  `json:"category_id"`
	CountryID  *uuid.UUID `json:"country_id,omitempty"`
	PlaceID    *uuid.UUID `json:"place_id,omitempty"`
	OccurredAt time.Time  `json:"occurred_at"`
}

type GigCategoryMatchedEventEnvelope struct {
	Type    NotificationEventType   `json:"type"`
	Payload GigCategoryMatchedEvent `json:"payload"`
}
