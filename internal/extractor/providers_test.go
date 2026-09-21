package extractor

import (
	"reflect"
	"testing"

	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
)

func TestConfiguredProvidersUsesProductFallbackOrder(t *testing.T) {
	ai := config.AI{
		Groq:       completeProvider("groq-model"),
		OpenRouter: completeProvider("open-router-model"),
		Gemini:     config.AIConfig{APIKey: "key", Model: "gemini-model"},
		Mistral:    completeProvider("mistral-model"),
		Nvidia:     completeProvider("nvidia-model"),
	}

	providers := ConfiguredProviders(&ai)
	got := make([]string, 0, len(providers))
	for _, provider := range providers {
		got = append(got, provider.Generator())
	}
	want := []string{
		"groq:groq-model",
		"open-router:open-router-model",
		"gemini:gemini-model",
		"mistral:mistral-model",
		"nvidia:nvidia-model",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ConfiguredProviders() = %v, want %v", got, want)
	}
}

func TestConfiguredProvidersSkipsIncompleteProvider(t *testing.T) {
	ai := config.AI{
		Groq:       completeProvider("groq-model"),
		OpenRouter: config.AIConfig{APIKey: "key", Model: "missing-base-url"},
		Gemini:     config.AIConfig{APIKey: "key", Model: "gemini-model"},
	}

	providers := ConfiguredProviders(&ai)
	got := make([]string, 0, len(providers))
	for _, provider := range providers {
		got = append(got, provider.Generator())
	}
	want := []string{"groq:groq-model", "gemini:gemini-model"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ConfiguredProviders() = %v, want %v", got, want)
	}
}

func completeProvider(model string) config.AIConfig {
	return config.AIConfig{APIKey: "key", BaseURL: "https://example.test/v1", Model: model}
}
