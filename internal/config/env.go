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
			API_KEY:             required(EnvAIKey),
			API_ENDPOINT:        required(EnvAIEndpoint),
			Model:               required(EnvAIModel),
			HuggingFaceAIModel:  getEnv(EnvHuggingFaceAIModel, DefaultHuggingFaceAIModel),
			LocalOllamaEndpoint: getEnv(EnvLocalOllamaEndpoint, DefaultLocalOllamaEndpoint),
			PromptFile:          getEnv(EnvPromptFilePath, DefaultPromptFilePath),
		},
	}, nil
}
