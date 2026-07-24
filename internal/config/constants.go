package config

const (
	EnvironmentDevelopment = "development"
	EnvironmentProduction  = "production"
)

const (
	EnvApp         = "APP_ENV"
	EnvServiceName = "SERVICE_NAME"

	EnvDBHost     = "DB_HOST"
	EnvDBPort     = "DB_PORT"
	EnvDBUser     = "DB_USER"
	EnvDBPassword = "DB_PASSWORD"
	EnvDBName     = "DB_NAME"
	EnvDBSSLMode  = "DB_SSLMODE"

	EnvOtelCollectorAddr = "OTEL_COLLECTOR_ADDR"
	EnvMetricsServerPort = "METRICS_SERVER_PORT"

	EnvAIKey               = "AI_API_KEY"
	EnvAIEndpoint          = "AI_API_ENDPOINT"
	EnvAIModel             = "AI_MODEL"
	EnvHuggingFaceAIModel  = "HUGGINGFACE_AI_MODEL"
	EnvLocalOllamaEndpoint = "LOCAL_OLLAMA_ENDPOINT"
	EnvPromptFilePath      = "PROMPT_FILE"

	EnvAWSRegion            = "AWS_REGION"
	EnvAWSAccessKeyID       = "AWS_ACCESS_KEY_ID"
	EnvAWSSecretAccessKey   = "AWS_SECRET_ACCESS_KEY"
	EnvAWSSQSEndpont        = "AWS_SQS_ENDPOINT"
	EnvCategorizationSQSUrl = "CATEGORIZATION_SQS_URL"
	EnvNotificationSQSUrl   = "NOTIFICATION_SQS_URL"
)

const (
	DefaultServiceName = "categorization-worker"

	DefaultOtelCollectorAddr = "localhost:4317"
	DefaultMetricsServerPort = ":9100"

	DefaultHuggingFaceAIModel  = "all-minilm:l12-v2"
	DefaultLocalOllamaEndpoint = "localhost:11434"
	DefaultPromptFilePath      = "./internal/prompter/prompts/profession.txt"

	DefaultDBSSLMode = "disable"
)
