package config

func loadEnv() (*Config, error) {
	req := &requiredEnv{}

	cfg := &Config{
		App: APP{
			ServiceName: getEnv(EnvServiceName, DefaultServiceName),
			Env:         EnvironmentDevelopment,
		},

		DB: DBConfig{
			Host:     req.get(EnvDBHost),
			Port:     req.get(EnvDBPort),
			User:     req.get(EnvDBUser),
			Password: req.get(EnvDBPassword),
			Name:     req.get(EnvDBName),
			SSLMode:  DefaultDBSSLMode,
		},

		AWS: AWSConfig{
			Region:                    req.get(EnvAWSRegion),
			AccessKeyID:               req.get(EnvAWSAccessKeyID),
			SecretAccessKey:           req.get(EnvAWSSecretAccessKey),
			CategorizationEventsQueue: req.get(EnvCategorizationSQSUrl),
			NotificationEventsQueue:   req.get(EnvNotificationSQSUrl),
			SQSEndpoint:               req.get(EnvAWSSQSEndpont),
		},

		Server: ServerConfig{
			MetricsServerPort: getEnv(EnvMetricsServerPort, DefaultMetricsServerPort),
			OTelCollectorAddr: getEnv(EnvOtelCollectorAddr, DefaultOtelCollectorAddr),
			WorkerConcurrency: getIntEnv(EnvWorkerConcurrency, DefaultWorkerConcurrency),
		},

		AI: AI{

			Groq: AIConfig{
				APIKey:  getEnv(EnvGroqAPIKey, ""),
				BaseURL: getEnv(EnvGroqBaseURL, ""),
				Model:   getEnv(EnvGroqModel, ""),
			},
			OpenRouter: AIConfig{
				APIKey:  getEnv(EnvQwenAPIKey, ""),
				BaseURL: getEnv(EnvQwenBaseURL, ""),
				Model:   getEnv(EnvQwenModel, ""),
			},
			Gemini: AIConfig{
				APIKey: getEnv(EnvGeminiAPIKey, ""),
				Model:  getEnv(EnvGeminiModel, ""),
			},
			Nvidia: AIConfig{
				APIKey:  getEnv(EnvNvidiaAPIKey, ""),
				BaseURL: getEnv(EnvNvidiaBaseURL, ""),
				Model:   getEnv(EnvNvidiaModel, ""),
			},
			Mistral: AIConfig{
				APIKey:  getEnv(EnvMistralAPIKey, ""),
				BaseURL: getEnv(EnvMistralBaseURL, ""),
				Model:   getEnv(EnvMistralModel, ""),
			},

			HuggingFaceAIModel:  getEnv(EnvHuggingFaceAIModel, DefaultHuggingFaceAIModel),
			LocalOllamaEndpoint: getEnv(EnvLocalOllamaEndpoint, DefaultLocalOllamaEndpoint),
			PromptFile:          getEnv(EnvPromptFilePath, DefaultPromptFilePath),
		},
	}

	if err := req.err(); err != nil {
		return nil, err
	}

	return cfg, nil
}
