package extractor

import "github.com/vsayfb/gig-platform-categorization-service/internal/config"

// ConfiguredProviders builds the sequential fallback chain in product order,
// omitting providers whose required configuration is incomplete.
func ConfiguredProviders(ai *config.AI) []Extractor {
	if ai == nil {
		return nil
	}

	providers := make([]Extractor, 0, 5)
	appendOpenAI := func(name string, cfg *config.AIConfig) {
		if cfg.APIKey == "" || cfg.BaseURL == "" || cfg.Model == "" {
			return
		}
		providers = append(providers, NewOpenAIExtractor(cfg, name))
	}

	appendOpenAI("groq", &ai.Groq)
	appendOpenAI("open-router", &ai.OpenRouter)

	if ai.Gemini.APIKey != "" && ai.Gemini.Model != "" {
		providers = append(providers, NewGeminiExtractor(&ai.Gemini))
	}

	appendOpenAI("mistral", &ai.Mistral)
	appendOpenAI("nvidia", &ai.Nvidia)

	return providers
}
