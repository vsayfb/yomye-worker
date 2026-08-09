package config

import "testing"

func TestStagingParameterNamesIncludeMistralAndNvidia(t *testing.T) {
	names := make(map[string]struct{})
	for _, name := range stagingParameterNames() {
		names[name] = struct{}{}
	}

	for _, key := range []string{
		ParameterMistralAPIKey,
		ParameterMistralBaseURL,
		ParameterMistralModel,
		ParameterNvidiaAPIKey,
		ParameterNvidiaEndpoint,
		ParameterNvidiaModel,
	} {
		name := ssmParameter(key)
		if _, ok := names[name]; !ok {
			t.Fatalf("staging parameter list is missing %q", name)
		}
	}
}

func TestAIConfigFromParametersAssignsAllFallbackProviders(t *testing.T) {
	params := map[string]string{
		ParameterGroqAPIKey:         "groq-key",
		ParameterGroqEndpoint:       "https://groq.example",
		ParameterGroqModel:          "groq-model",
		ParameterOpenRouterAPIKey:   "openrouter-key",
		ParameterOpenRouterEndpoint: "https://openrouter.example",
		ParameterOpenRouterModel:    "openrouter-model",
		ParameterGeminiAPIKey:       "gemini-key",
		ParameterGeminiModel:        "gemini-model",
		ParameterMistralAPIKey:      "mistral-key",
		ParameterMistralBaseURL:     "https://mistral.example",
		ParameterMistralModel:       "mistral-model",
		ParameterNvidiaAPIKey:       "nvidia-key",
		ParameterNvidiaEndpoint:     "https://nvidia.example",
		ParameterNvidiaModel:        "nvidia-model",
	}

	ai := aiConfigFromParameters(params)

	assertAIConfig(t, "groq", ai.Groq, "groq-key", "https://groq.example", "groq-model")
	assertAIConfig(t, "openrouter", ai.OpenRouter, "openrouter-key", "https://openrouter.example", "openrouter-model")
	assertAIConfig(t, "gemini", ai.Gemini, "gemini-key", "", "gemini-model")
	assertAIConfig(t, "mistral", ai.Mistral, "mistral-key", "https://mistral.example", "mistral-model")
	assertAIConfig(t, "nvidia", ai.Nvidia, "nvidia-key", "https://nvidia.example", "nvidia-model")
}

func assertAIConfig(t *testing.T, name string, got AIConfig, wantKey, wantURL, wantModel string) {
	t.Helper()

	if got.APIKey != wantKey || got.BaseURL != wantURL || got.Model != wantModel {
		t.Fatalf(
			"%s config = %#v, want key=%q url=%q model=%q",
			name,
			got,
			wantKey,
			wantURL,
			wantModel,
		)
	}
}
