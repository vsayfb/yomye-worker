package notification

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Publisher interface {
	PublishListingApproved(context.Context, ListingApprovedEnvelope) error
	PublishGigCategoryMatched(context.Context, GigCategoryMatchedEventEnvelope) error
	PublishContentRejected(context.Context, ContentRejectedEnvelope) error
}

type NotificationEventType string

const (
	CurrentEventVersion = 1

	EventListingApproved    NotificationEventType = "listing_approved"
	EventGigCategoryMatched NotificationEventType = "gig_category_matched"
	EventContentRejected    NotificationEventType = "content_rejected"
)

type RejectedSubjectType string

const (
	RejectedSubjectListing    RejectedSubjectType = "listing"
	RejectedSubjectProfession RejectedSubjectType = "profession"
)

type ListingApprovedEnvelope struct {
	Type    NotificationEventType `json:"type"`
	EventID uuid.UUID             `json:"event_id"`
	Version int                   `json:"version"`
	Payload ListingApprovedEvent  `json:"payload"`
}

type ListingApprovedEvent struct {
	RecipientID      uuid.UUID `json:"recipient_id"`
	ListingID        uuid.UUID `json:"listing_id"`
	BaseCategorySlug string    `json:"base_category_slug"`
	Title            string    `json:"title"`
	OccurredAt       time.Time `json:"occurred_at"`
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
	EventID uuid.UUID               `json:"event_id"`
	Version int                     `json:"version"`
	Payload GigCategoryMatchedEvent `json:"payload"`
}

type ContentRejectedEnvelope struct {
	Type    NotificationEventType `json:"type"`
	EventID uuid.UUID             `json:"event_id"`
	Version int                   `json:"version"`
	Payload ContentRejectedEvent  `json:"payload"`
}

type ContentRejectedEvent struct {
	RecipientID uuid.UUID           `json:"recipient_id"`
	SubjectType RejectedSubjectType `json:"subject_type"`
	SubjectID   uuid.UUID           `json:"subject_id"`
	Reason      string              `json:"reason"`
	OccurredAt  time.Time           `json:"occurred_at"`
}

// StableEventID returns the same ID for repeated publication of the same
// logical event. This lets downstream consumers deduplicate the worker's
// intentionally at-least-once notification delivery without requiring an
// outbox or persisted publication record in this service.
func StableEventID(eventType NotificationEventType, aggregateIDs ...uuid.UUID) uuid.UUID {
	parts := make([]string, 0, len(aggregateIDs)+2)
	parts = append(parts, "yevmiye-categorization", string(eventType))
	for _, id := range aggregateIDs {
		parts = append(parts, id.String())
	}

	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join(parts, ":")))
}
