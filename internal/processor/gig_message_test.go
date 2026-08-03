package processor

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestDecodeListingCreatedMessagePreservesInternationalLocation(t *testing.T) {
	countryID := uuid.New()
	placeID := uuid.New()
	body := `{"listing_id":"` + uuid.NewString() + `","author_id":"` + uuid.NewString() +
		`","description":"Ich suche einen Elektriker","country_id":"` + countryID.String() +
		`","place_id":"` + placeID.String() + `"}`

	msg, err := decodeListingCreatedMessage(body)
	if err != nil {
		t.Fatalf("decodeListingCreatedMessage() error = %v", err)
	}
	if msg.CountryID == nil || *msg.CountryID != countryID {
		t.Fatalf("country_id = %v, want %s", msg.CountryID, countryID)
	}
	if msg.PlaceID == nil || *msg.PlaceID != placeID {
		t.Fatalf("place_id = %v, want %s", msg.PlaceID, placeID)
	}
}

func TestDecodeListingCreatedMessageAllowsMissingLocation(t *testing.T) {
	body := `{"listing_id":"` + uuid.NewString() + `","author_id":"` + uuid.NewString() +
		`","description":"I need a plumber"}`

	msg, err := decodeListingCreatedMessage(body)
	if err != nil {
		t.Fatalf("decodeListingCreatedMessage() error = %v", err)
	}
	if msg.CountryID != nil || msg.PlaceID != nil {
		t.Fatalf("location = (%v, %v), want nil values", msg.CountryID, msg.PlaceID)
	}
}

func TestDecodeListingCreatedMessageRejectsPlaceWithoutCountry(t *testing.T) {
	body := `{"listing_id":"` + uuid.NewString() + `","author_id":"` + uuid.NewString() +
		`","description":"I need a plumber","place_id":"` + uuid.NewString() + `"}`

	_, err := decodeListingCreatedMessage(body)
	if err == nil || !strings.Contains(err.Error(), "place_id requires country_id") {
		t.Fatalf("decodeListingCreatedMessage() error = %v, want place/country validation error", err)
	}
}
