package notification

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestListingApprovedEnvelopeJSONContract(t *testing.T) {
	t.Parallel()

	recipientID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	listingID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	occurredAt := time.Date(2026, time.July, 25, 12, 30, 0, 0, time.UTC)

	body, err := json.Marshal(ListingApprovedEnvelope{
		Type: EventListingApproved,
		Payload: ListingApprovedEvent{
			RecipientID:      recipientID,
			ListingID:        listingID,
			BaseCategorySlug: "for-sale",
			Title:            "Az Kullanılmış Bisiklet",
			OccurredAt:       occurredAt,
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	want := `{"type":"listing_approved","payload":{"recipient_id":"11111111-1111-1111-1111-111111111111","listing_id":"22222222-2222-2222-2222-222222222222","base_category_slug":"for-sale","title":"Az Kullanılmış Bisiklet","occurred_at":"2026-07-25T12:30:00Z"}}`
	if string(body) != want {
		t.Fatalf("JSON contract mismatch\n got: %s\nwant: %s", body, want)
	}
}

func TestGigCategoryMatchedEnvelopeUsesInternationalLocation(t *testing.T) {
	t.Parallel()

	gigID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	categoryID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	countryID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	placeID := uuid.MustParse("44444444-4444-4444-4444-444444444444")

	body, err := json.Marshal(GigCategoryMatchedEventEnvelope{
		Type: EventGigCategoryMatched,
		Payload: GigCategoryMatchedEvent{
			GigID:      gigID,
			CategoryID: categoryID,
			CountryID:  &countryID,
			PlaceID:    &placeID,
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	want := `{"type":"gig_category_matched","payload":{"gig_id":"11111111-1111-1111-1111-111111111111","category_id":"22222222-2222-2222-2222-222222222222","country_id":"33333333-3333-3333-3333-333333333333","place_id":"44444444-4444-4444-4444-444444444444","occurred_at":"0001-01-01T00:00:00Z"}}`
	if string(body) != want {
		t.Fatalf("JSON contract mismatch\n got: %s\nwant: %s", body, want)
	}
}
