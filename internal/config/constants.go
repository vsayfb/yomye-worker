package config

const (
	EnvironmentDevelopment = "development"
	EnvironmentStaging     = "staging"
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
	EnvWorkerConcurrency = "WORKER_CONCURRENCY"

	EnvGroqAPIKey  = "GROQ_API_KEY"
	EnvGroqModel   = "GROQ_MODEL"
	EnvGroqBaseURL = "GROQ_BASE_URL"

	EnvGeminiAPIKey = "GEMINI_API_KEY"
	EnvGeminiModel  = "GEMINI_MODEL"

	EnvQwenAPIKey  = "QWEN_API_KEY"
	EnvQwenModel   = "QWEN_MODEL"
	EnvQwenBaseURL = "QWEN_BASE_URL"

	EnvNvidiaAPIKey  = "NVIDIA_API_KEY"
	EnvNvidiaModel   = "NVIDIA_MODEL"
	EnvNvidiaBaseURL = "NVIDIA_BASE_URL"

	EnvMistralAPIKey  = "MISTRAL_API_KEY"
	EnvMistralModel   = "MISTRAL_MODEL"
	EnvMistralBaseURL = "MISTRAL_BASE_URL"

	EnvHuggingFaceAIModel  = "HUGGINGFACE_AI_MODEL"
	EnvLocalOllamaEndpoint = "LOCAL_OLLAMA_ENDPOINT"
	EnvPromptFilePath      = "PROMPT_FILE"

	EnvAWSRegion            = "AWS_REGION"
	EnvAWSAccessKeyID       = "AWS_ACCESS_KEY_ID"
	EnvAWSSecretAccessKey   = "AWS_SECRET_ACCESS_KEY"
	EnvAWSSQSEndpont        = "AWS_SQS_ENDPOINT"
	EnvCategorizationSQSUrl = "CATEGORIZATION_SQS_URL"
	EnvNotificationSQSUrl   = "NOTIFICATION_SQS_URL"

	EnvGoogleCloudProject   = "GOOGLE_CLOUD_PROJECT"
	EnvGCPParameterLocation = "GCP_PARAMETER_LOCATION"
)

const (
	DefaultServiceName = "categorization-worker"

	DefaultOtelCollectorAddr = "localhost:4317"
	DefaultMetricsServerPort = ":9100"
	DefaultWorkerConcurrency = 10

	DefaultHuggingFaceAIModel  = "all-minilm:l12-v2"
	DefaultLocalOllamaEndpoint = "http://localhost:11434"
	DefaultPromptFilePath      = "./internal/prompter/prompts/profession.txt"

	DefaultDBSSLMode = "disable"
)
