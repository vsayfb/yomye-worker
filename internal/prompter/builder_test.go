package prompter

import (
	"strings"
	"testing"
)

func TestCategoryPathPromptUsesCanonicalEnglishAndLocalizedMetadata(t *testing.T) {
	prompt := CategoryPathPrompt("gig", "- Lawyer: Legal professional [selectable=true]")

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
