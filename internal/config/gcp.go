package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	parametermanager "cloud.google.com/go/parametermanager/apiv1"
	"cloud.google.com/go/parametermanager/apiv1/parametermanagerpb"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/googleapis/gax-go/v2"
)

const defaultGCPParameterLocation = "global"

type parameterManagerClient interface {
	RenderParameterVersion(
		context.Context,
		*parametermanagerpb.RenderParameterVersionRequest,
		...gax.CallOption,
	) (*parametermanagerpb.RenderParameterVersionResponse, error)
	Close() error
}

type secretManagerClient interface {
	AccessSecretVersion(
		context.Context,
		*secretmanagerpb.AccessSecretVersionRequest,
		...gax.CallOption,
	) (*secretmanagerpb.AccessSecretVersionResponse, error)
	Close() error
}

var newParameterManagerClient = func(ctx context.Context) (parameterManagerClient, error) {
	return parametermanager.NewClient(ctx)
}

var newSecretManagerClient = func(ctx context.Context) (secretManagerClient, error) {
	return secretmanager.NewClient(ctx)
}

type gcpDatabaseSecret struct {
	DBName   string `json:"dbname"`
	Engine   string `json:"engine"`
	Host     string `json:"host"`
	Password string `json:"password"`
	Port     int    `json:"port"`
	Username string `json:"username"`
}

func loadGCP(ctx context.Context) (cfg *Config, resultErr error) {
	projectID := strings.TrimSpace(os.Getenv(EnvGoogleCloudProject))
	if projectID == "" {
		return nil, fmt.Errorf("missing required environment variable: %s", EnvGoogleCloudProject)
	}

	location := strings.TrimSpace(getEnv(EnvGCPParameterLocation, defaultGCPParameterLocation))
	if location == "" {
		location = defaultGCPParameterLocation
	}

	parameterClient, err := newParameterManagerClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create GCP Parameter Manager client: %w", err)
	}
	defer func() {
		if err := parameterClient.Close(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close GCP Parameter Manager client: %w", err))
			cfg = nil
		}
	}()

	secretClient, err := newSecretManagerClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create GCP Secret Manager client: %w", err)
	}
	defer func() {
		if err := secretClient.Close(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close GCP Secret Manager client: %w", err))
			cfg = nil
		}
	}()

	return loadGCPConfig(ctx, parameterClient, secretClient, projectID, location)
}

func loadGCPConfig(
	ctx context.Context,
	parameterClient parameterManagerClient,
	secretClient secretManagerClient,
	projectID string,
	location string,
) (*Config, error) {
	params, err := loadGCPParameters(
		ctx,
		parameterClient,
		projectID,
		location,
		gcpParameterNames(),
	)
	if err != nil {
		return nil, err
	}

	if err := validateGCPDBPort(params[ParameterDBPort]); err != nil {
		return nil, err
	}

	dbSecret, err := loadGCPDatabaseSecret(
		ctx,
		secretClient,
		params[ParameterRDSSecretArn],
	)
	if err != nil {
		return nil, err
	}

	return &Config{
		App: APP{
			ServiceName: getEnv(EnvServiceName, DefaultServiceName),
			Env:         EnvironmentProduction,
		},
		DB: DBConfig{
			Host:     params[ParameterDBHost],
			Port:     params[ParameterDBPort],
			Name:     params[ParameterDBName],
			User:     dbSecret.Username,
			Password: dbSecret.Password,
			SSLMode:  "require",
		},
		GCP: GCPConfig{
			ProjectID:                 projectID,
			ParameterLocation:         location,
			CategorizationEventsTopic: params[ParameterSQSCategoryEventsQueueURL],
			NotificationEventsTopic:   params[ParameterSQSNotificationEventsQueueURL],
		},
		Server: ServerConfig{
			MetricsServerPort: getEnv(EnvMetricsServerPort, DefaultMetricsServerPort),
			OTelCollectorAddr: getEnv(EnvOtelCollectorAddr, DefaultOtelCollectorAddr),
			WorkerConcurrency: getIntEnv(EnvWorkerConcurrency, DefaultWorkerConcurrency),
		},
		AI: aiConfigFromParameters(params),
	}, nil
}

func loadGCPParameters(
	ctx context.Context,
	client parameterManagerClient,
	projectID string,
	location string,
	names []string,
) (map[string]string, error) {
	params := make(map[string]string, len(names))

	for _, name := range names {
		resourceName := fmt.Sprintf(
			"projects/%s/locations/%s/parameters/%s/versions/latest",
			projectID,
			location,
			name,
		)

		response, err := client.RenderParameterVersion(
			ctx,
			&parametermanagerpb.RenderParameterVersionRequest{Name: resourceName},
		)
		if err != nil {
			return nil, fmt.Errorf("render GCP parameter %q: %w", name, err)
		}

		if response == nil || len(response.GetRenderedPayload()) == 0 {
			return nil, fmt.Errorf("GCP parameter %q has an empty rendered payload", name)
		}

		params[name] = string(response.GetRenderedPayload())
	}

	return params, nil
}

func gcpParameterNames() []string {
	return []string{
		ParameterDBHost,
		ParameterDBPort,
		ParameterDBName,
		ParameterRDSSecretArn,
		ParameterSQSCategoryEventsQueueURL,
		ParameterSQSNotificationEventsQueueURL,
		ParameterGroqAPIKey,
		ParameterGroqEndpoint,
		ParameterGroqModel,
		ParameterOpenRouterAPIKey,
		ParameterOpenRouterEndpoint,
		ParameterOpenRouterModel,
		ParameterGeminiAPIKey,
		ParameterGeminiModel,
		ParameterMistralAPIKey,
		ParameterMistralModel,
		ParameterMistralBaseURL,
		ParameterNvidiaAPIKey,
		ParameterNvidiaEndpoint,
		ParameterNvidiaModel,
	}
}

func validateGCPDBPort(value string) error {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid GCP parameter %q: must be a valid TCP port", ParameterDBPort)
	}
	return nil
}

func loadGCPDatabaseSecret(
	ctx context.Context,
	client secretManagerClient,
	secretResource string,
) (*gcpDatabaseSecret, error) {
	versionName, err := gcpSecretVersionName(secretResource)
	if err != nil {
		return nil, err
	}

	response, err := client.AccessSecretVersion(
		ctx,
		&secretmanagerpb.AccessSecretVersionRequest{Name: versionName},
	)
	if err != nil {
		return nil, fmt.Errorf("access PostgreSQL credential secret: %w", err)
	}
	if response == nil || response.GetPayload() == nil || len(response.GetPayload().GetData()) == 0 {
		return nil, errors.New("PostgreSQL credential secret has an empty payload")
	}

	var secret gcpDatabaseSecret
	if err := json.Unmarshal(response.GetPayload().GetData(), &secret); err != nil {
		return nil, fmt.Errorf("decode PostgreSQL credential secret JSON: %w", err)
	}
	if strings.TrimSpace(secret.Username) == "" || strings.TrimSpace(secret.Password) == "" {
		return nil, errors.New("PostgreSQL credential secret is missing username or password")
	}
	if secret.Engine != "" && !strings.EqualFold(strings.TrimSpace(secret.Engine), "postgres") {
		return nil, errors.New("PostgreSQL credential secret has an unsupported engine")
	}

	return &secret, nil
}

func gcpSecretVersionName(secretResource string) (string, error) {
	secretResource = strings.TrimSpace(secretResource)
	parts := strings.Split(secretResource, "/")
	if len(parts) != 4 || parts[0] != "projects" || parts[1] == "" || parts[2] != "secrets" || parts[3] == "" {
		return "", fmt.Errorf(
			"GCP parameter %q must contain a Secret Manager resource name",
			ParameterRDSSecretArn,
		)
	}

	return secretResource + "/versions/latest", nil
}
