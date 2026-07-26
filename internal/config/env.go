package config

func loadEnv() (*Config, error) {
	return &Config{
		App: APP{
			ServiceName: getEnv(EnvServiceName, DefaultServiceName),
			Env:         EnvironmentDevelopment,
		},

		DB: DBConfig{
			Host:     required(EnvDBHost),
			Port:     required(EnvDBPort),
			User:     required(EnvDBUser),
			Password: required(EnvDBPassword),
			Name:     required(EnvDBName),
			SSLMode:  DefaultDBSSLMode,
		},

		AWS: AWSConfig{
			Region:                    required(EnvAWSRegion),
			AccessKeyID:               required(EnvAWSAccessKeyID),
			SecretAccessKey:           required(EnvAWSSecretAccessKey),
			CategorizationEventsQueue: required(EnvCategorizationSQSUrl),
			NotificationEventsQueue:   required(EnvNotificationSQSUrl),
			SQSEndpoint:               required(EnvAWSSQSEndpont),
		},

		Server: ServerConfig{
			MetricsServerPort: getEnv(EnvMetricsServerPort, DefaultMetricsServerPort),
			OTelCollectorAddr: getEnv(EnvOtelCollectorAddr, DefaultOtelCollectorAddr),
		},

		AI: AI{

			Groq: AIConfig{
				APIKey:  required(EnvGroqAPIKey),
				BaseURL: required(EnvGroqBaseURL),
				Model:   required(EnvGroqModel),
			},
			Qwen: AIConfig{
				APIKey:  required(EnvQwenAPIKey),
				BaseURL: required(EnvQwenBaseURL),
				Model:   required(EnvQwenModel),
			},
			Gemini: AIConfig{
				APIKey: required(EnvGeminiAPIKey),
				Model:  required(EnvGeminiModel),
			},
			Nvidia: AIConfig{
				APIKey:  required(EnvNvidiaAPIKey),
				BaseURL: required(EnvNvidiaBaseURL),
				Model:   required(EnvNvidiaModel),
			},

			HuggingFaceAIModel:  getEnv(EnvHuggingFaceAIModel, DefaultHuggingFaceAIModel),
			LocalOllamaEndpoint: getEnv(EnvLocalOllamaEndpoint, DefaultLocalOllamaEndpoint),
			PromptFile:          getEnv(EnvPromptFilePath, DefaultPromptFilePath),
		},
	}, nil
}
