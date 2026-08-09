package prompter

import (
	"strings"
	"testing"
)

func TestCategoryPathPromptUsesCanonicalEnglishAndLocalizedMetadata(t *testing.T) {
	prompt := CategoryPathPrompt("- Lawyer: Legal professional [selectable=true]")

	for _, required := range []string{
		"canonical_name",
		"canonical_slug",
		"canonical_description",
		"localized_name",
		"localized_slug",
		"localized_description",
		"source_locale",
		"Canonical category metadata is always English",
		"Lawyer: Legal professional",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("prompt is missing %q", required)
		}
	}
	if strings.Contains(prompt, "%!") {
		t.Fatalf("prompt contains a formatting error: %s", prompt)
	}
}

func TestCategoryPathPromptKeepsServiceCategoriesProviderNeutral(t *testing.T) {
	prompt := CategoryPathPrompt("")

	for _, required := range []string{
		"individuals, tradespeople, teams, shops, or small businesses",
		`Use "Car Wash", not "Car Wash Specialist"`,
		`"Oto Yıkama" resolves to canonical "Car Wash"`,
		`not "Car Wash and`,
		"only when detailing is explicit",
		"Do not add person-role suffixes",
		"Do not broaden the input",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("provider-neutral prompt is missing %q", required)
		}
	}
}

func TestGigPromptDoesNotInventProviderRolesOrAdjacentServices(t *testing.T) {
	prompt := GigPrompt()

	for _, required := range []string{
		"provider-neutral name",
		`"Oto Yıkama" stays "Oto Yıkama"`,
		`do not invent "Detaylı Temizlik"`,
		"Do not add adjacent services",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("gig prompt is missing %q", required)
		}
	}
}
