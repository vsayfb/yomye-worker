package extractor

import "testing"

func TestDecodeGigDetailsBuildsListing(t *testing.T) {
	t.Parallel()

	path := []ExtractedCategoryNode{
		{CanonicalName: "Technology", CanonicalDescription: "Technology services"},
		{CanonicalName: "Software Developer", CanonicalDescription: "Software development services"},
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
	if got.Gig == nil {
		t.Fatalf("decodeDetails() returned invalid gig shape: %#v", got)
	}
	if len(got.CategoryPath) != 2 ||
		got.CategoryPath[1].CanonicalName != "Software Developer" ||
		got.Gig.Profession != "Yazılımcı" {
		t.Fatalf("decodeDetails() = %#v", got)
	}
}

func TestDecodeDetailsRejectsUnsupportedBaseCategory(t *testing.T) {
	t.Parallel()

	_, err := decodeDetails("unknown", nil, `{}`)
	if err == nil {
		t.Fatal("expected unsupported base category to fail")
	}
}

func TestDecodeCategoryPath(t *testing.T) {
	t.Parallel()

	locale, got, err := decodeCategoryPath(`{
		"source_locale":"tr",
		"category_path": [
			{"canonical_name":"Education","canonical_slug":"education","canonical_description":"Education services","localized_name":"Eğitim","localized_slug":"egitim","localized_description":"Eğitim hizmetleri","locale":"tr","is_selectable":false},
			{"canonical_name":"Mathematics Teacher","canonical_slug":"mathematics-teacher","canonical_description":"Mathematics education","localized_name":"Matematik Öğretmeni","localized_slug":"matematik-ogretmeni","localized_description":"Matematik eğitimi","locale":"tr","is_selectable":true}
		]
	}`)
	if err != nil {
		t.Fatalf("decodeCategoryPath() error = %v", err)
	}
	if locale != "tr" || len(got) != 2 || got[1].CanonicalName != "Mathematics Teacher" {
		t.Fatalf("decodeCategoryPath() = %#v", got)
	}
	if got[0].IsSelectable || !got[1].IsSelectable {
		t.Fatalf("decodeCategoryPath() selectability = %#v", got)
	}
}

func TestDecodeCategoryPathRejectsIncompleteLocalization(t *testing.T) {
	t.Parallel()

	_, _, err := decodeCategoryPath(`{
		"source_locale":"de",
		"category_path":[{
			"canonical_name":"Lawyer",
			"canonical_slug":"lawyer",
			"canonical_description":"Legal professional",
			"localized_name":"Rechtsanwalt",
			"localized_slug":"rechtsanwalt",
			"locale":"de",
			"is_selectable":true
		}]
	}`)
	if err == nil {
		t.Fatal("expected missing localized_description to be rejected")
	}
}

func TestDecodeJSONObjectAcceptsWrappedJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "markdown fence",
			raw:  "```json\n{\"value\":\"gig\"}\n```",
		},
		{
			name: "leading explanation",
			raw:  "Answer:\n{\"value\":\"gig\"}",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got struct {
				Value string `json:"value"`
			}
			if err := decodeJSONObject(tt.raw, &got); err != nil {
				t.Fatalf("decodeJSONObject() error = %v", err)
			}
			if got.Value != "gig" {
				t.Fatalf("decoded value = %q, want gig", got.Value)
			}
		})
	}
}
