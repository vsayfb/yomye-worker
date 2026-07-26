package extractor

import "testing"

func TestDecodeBaseCategory(t *testing.T) {
	t.Parallel()

	got, err := decodeBaseCategory(`{"base_category_slug":"gig"}`)
	if err != nil {
		t.Fatalf("decodeBaseCategory() error = %v", err)
	}
	if got != "gig" {
		t.Fatalf("decodeBaseCategory() = %q, want gig", got)
	}
}

func TestDecodeGigDetailsBuildsDiscriminatedListing(t *testing.T) {
	t.Parallel()

	got, err := decodeDetails("gig", `{
		"category": "Yazılımcı",
		"title": "Yazılımcı Aranıyor",
		"profession": "Yazılımcı",
		"duration_type": "",
		"pay_amount": null,
		"pay_currency": "TRY",
		"expires_at": null
	}`)
	if err != nil {
		t.Fatalf("decodeDetails() error = %v", err)
	}
	if got.Gig == nil || got.Sale != nil {
		t.Fatalf("decodeDetails() returned invalid gig shape: %#v", got)
	}
	if got.Category != "Yazılımcı" || got.Gig.Profession != "Yazılımcı" {
		t.Fatalf("decodeDetails() = %#v", got)
	}
}

func TestDecodeSaleDetailsBuildsDiscriminatedListing(t *testing.T) {
	t.Parallel()

	got, err := decodeDetails("for-sale", `{
		"category": "Bisiklet",
		"title": "Az Kullanılmış Bisiklet",
		"price_amount": null,
		"price_currency": null,
		"pricing_type": "contact",
		"condition": "good",
		"brand": null,
		"model": null,
		"expires_at": null
	}`)
	if err != nil {
		t.Fatalf("decodeDetails() error = %v", err)
	}
	if got.Sale == nil || got.Gig != nil {
		t.Fatalf("decodeDetails() returned invalid sale shape: %#v", got)
	}
	if got.Category != "Bisiklet" || got.Sale.PricingType != "contact" {
		t.Fatalf("decodeDetails() = %#v", got)
	}
}
