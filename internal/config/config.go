package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	App    APP
	DB     DBConfig
	AWS    AWSConfig
	GCP    GCPConfig
	Server ServerConfig
	AI     AI
}

type APP struct {
	ServiceName string
	Env         string
}

type DBConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
	SSLMode  string
}

type AWSConfig struct {
	Region string
	// AccessKeyID and SecretAccessKey are development/LocalStack-only.
	// Staging uses the AWS SDK default credential chain so IAM/STS credentials
	// retain their session token and refresh behavior.
	AccessKeyID               string
	SecretAccessKey           string
	SQSEndpoint               string
	CategorizationEventsQueue string
	NotificationEventsQueue   string
}

// GCPConfig describes production resources. The parameter IDs retain their
// legacy SQS names, but these values are fully-qualified Pub/Sub topic names.
// Transport construction belongs to a dedicated Pub/Sub adapter and must not
// pass these values to the AWS SDK.
type GCPConfig struct {
	ProjectID                  string
	ParameterLocation          string
	CategorizationEventsTopic  string
	CategorizationSubscription string
	NotificationEventsTopic    string
}

type ServerConfig struct {
	MetricsServerPort string
	OTelCollectorAddr string
	WorkerConcurrency int
}

type AIConfig struct {
	APIKey  string
	BaseURL string
	Model   string
}

type AI struct {
	Groq       AIConfig
	OpenRouter AIConfig
	Gemini     AIConfig
	Nvidia     AIConfig
	Mistral    AIConfig

	HuggingFaceAIModel  string
	PromptFile          string
	LocalOllamaEndpoint string
}

func Load(ctx context.Context) (*Config, error) {

	env := os.Getenv(EnvApp)

	if env == EnvironmentStaging {
		return loadAWS(ctx)
	}

	if env == EnvironmentProduction {
		return loadGCP(ctx)
	}

	return loadEnv()
}

// requiredEnv accumulates missing-required-variable errors instead of
// panicking, so a misconfigured deployment fails with a clean error log
// (listing everything that's missing at once) instead of an unhandled panic.
type requiredEnv struct {
	errs []error
}

func (r *requiredEnv) get(key string) string {
	value := os.Getenv(key)

	if value == "" {
		r.errs = append(r.errs, fmt.Errorf("missing required environment variable: %s", key))
	}

	return value
}

func (r *requiredEnv) err() error {
	if len(r.errs) == 0 {
		return nil
	}

	return fmt.Errorf("invalid configuration: %w", errors.Join(r.errs...))
}

func getEnv(key string, defaultValue string) string {
	value := os.Getenv(key)

	if value == "" {
		return defaultValue
	}

	return value
}

func getIntEnv(key string, defaultValue int) int {
	value := os.Getenv(key)

	if value == "" {
		return defaultValue
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return defaultValue
	}

	return parsed
}

func (c *DBConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.Name, c.SSLMode,
	)
}
