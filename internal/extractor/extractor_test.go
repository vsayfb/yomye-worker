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

	path := []ExtractedCategoryNode{
		{Name: "Bilişim ve Teknoloji", Description: "Dijital teknoloji hizmetleri"},
		{Name: "Yazılımcı", Description: "Yazılım geliştirme hizmetleri"},
	}
	got, err := decodeDetails("gig", path, `{
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
	if len(got.CategoryPath) != 2 ||
		got.CategoryPath[1].Name != "Yazılımcı" ||
		got.Gig.Profession != "Yazılımcı" {
		t.Fatalf("decodeDetails() = %#v", got)
	}
}

func TestDecodeSaleDetailsBuildsDiscriminatedListing(t *testing.T) {
	t.Parallel()

	path := []ExtractedCategoryNode{
		{Name: "Spor ve Outdoor", Description: "Spor ve açık hava ürünleri"},
		{Name: "Bisiklet", Description: "Bisiklet ve ekipmanları"},
	}
	got, err := decodeDetails("for-sale", path, `{
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
	if len(got.CategoryPath) != 2 ||
		got.CategoryPath[1].Name != "Bisiklet" ||
		got.Sale.PricingType != "contact" {
		t.Fatalf("decodeDetails() = %#v", got)
	}
}

func TestDecodeCategoryPath(t *testing.T) {
	t.Parallel()

	got, err := decodeCategoryPath(`{
		"category_path": [
			{"name":"Eğitim","description":"Eğitim hizmetleri"},
			{"name":"Matematik Öğretmeni","description":"Matematik eğitimi"}
		]
	}`)
	if err != nil {
		t.Fatalf("decodeCategoryPath() error = %v", err)
	}
	if len(got) != 2 || got[1].Name != "Matematik Öğretmeni" {
		t.Fatalf("decodeCategoryPath() = %#v", got)
	}
}
