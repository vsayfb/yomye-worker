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
		Type:    EventListingApproved,
		EventID: StableEventID(EventListingApproved, listingID),
		Version: CurrentEventVersion,
		Payload: ListingApprovedEvent{
			RecipientID:      recipientID,
			ListingID:        listingID,
			BaseCategorySlug: "gig",
			Title:            "Logo Designer",
			OccurredAt:       occurredAt,
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	want := `{"type":"listing_approved","event_id":"e7fdf1ea-00c7-50dd-8551-770a82dfc59a","version":1,"payload":{"recipient_id":"11111111-1111-1111-1111-111111111111","listing_id":"22222222-2222-2222-2222-222222222222","base_category_slug":"gig","title":"Logo Designer","occurred_at":"2026-07-25T12:30:00Z"}}`
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
	occurredAt := time.Date(2026, time.July, 25, 12, 31, 0, 0, time.UTC)

	body, err := json.Marshal(GigCategoryMatchedEventEnvelope{
		Type:    EventGigCategoryMatched,
		EventID: StableEventID(EventGigCategoryMatched, gigID, categoryID),
		Version: CurrentEventVersion,
		Payload: GigCategoryMatchedEvent{
			GigID:      gigID,
			CategoryID: categoryID,
			CountryID:  &countryID,
			PlaceID:    &placeID,
			OccurredAt: occurredAt,
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	want := `{"type":"gig_category_matched","event_id":"f4dfe120-1c30-59b4-9f0a-9a499765b49a","version":1,"payload":{"gig_id":"11111111-1111-1111-1111-111111111111","category_id":"22222222-2222-2222-2222-222222222222","country_id":"33333333-3333-3333-3333-333333333333","place_id":"44444444-4444-4444-4444-444444444444","occurred_at":"2026-07-25T12:31:00Z"}}`
	if string(body) != want {
		t.Fatalf("JSON contract mismatch\n got: %s\nwant: %s", body, want)
	}
}

func TestStableEventIDIsDeterministicAndEventSpecific(t *testing.T) {
	t.Parallel()

	listingID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	categoryID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")

	first := StableEventID(EventGigCategoryMatched, listingID, categoryID)
	second := StableEventID(EventGigCategoryMatched, listingID, categoryID)
	if first != second {
		t.Fatalf("stable event ID changed across replay: %s != %s", first, second)
	}

	approved := StableEventID(EventListingApproved, listingID)
	if first == approved {
		t.Fatalf("different logical events must not share an event ID: %s", first)
	}
}

func TestContentRejectedEnvelopeJSONContract(t *testing.T) {
	t.Parallel()

	recipientID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	subjectID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	eventID := StableEventID(EventContentRejected, subjectID)
	occurredAt := time.Date(2026, time.August, 10, 12, 30, 0, 0, time.UTC)

	body, err := json.Marshal(ContentRejectedEnvelope{
		Type:    EventContentRejected,
		EventID: eventID,
		Version: CurrentEventVersion,
		Payload: ContentRejectedEvent{
			RecipientID: recipientID,
			SubjectType: RejectedSubjectListing,
			SubjectID:   subjectID,
			Reason:      "alcohol",
			OccurredAt:  occurredAt,
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	want := `{"type":"content_rejected","event_id":"` + eventID.String() + `","version":1,"payload":{"recipient_id":"11111111-1111-1111-1111-111111111111","subject_type":"listing","subject_id":"22222222-2222-2222-2222-222222222222","reason":"alcohol","occurred_at":"2026-08-10T12:30:00Z"}}`
	if string(body) != want {
		t.Fatalf("JSON contract mismatch\n got: %s\nwant: %s", body, want)
	}
}
