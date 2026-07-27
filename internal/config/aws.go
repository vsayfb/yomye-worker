package config

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

const SSMParameterPath = "/yevmiye/app/"

const (
	ParameterDBHost       = "db-host"
	ParameterDBPort       = "db-port"
	ParameterDBName       = "db-name"
	ParameterRDSSecretArn = "rds-secret-arn"

	ParameterSQSCategoryEventsQueueURL     = "sqs-category-events-queue-url"
	ParameterSQSNotificationEventsQueueURL = "sqs-notification-events-queue-url"

	ParameterGroqAPIKey   = "groq-api-key"
	ParameterGroqEndpoint = "groq-ai-endpoint"
	ParameterGroqModel    = "groq-ai-model"

	ParameterQwenAPIKey   = "qwen-api-key"
	ParameterQwenEndpoint = "qwen-ai-endpoint"
	ParameterQwenModel    = "qwen-ai-model"

	ParameterGeminiAPIKey = "gemini-api-key"
	ParameterGeminiModel  = "gemini-ai-model"

	ParameterNvidiaAPIKey   = "nvidia-api-key"
	ParameterNvidiaEndpoint = "nvidia-ai-endpoint"
	ParameterNvidiaModel    = "nvidia-ai-model"
)

type rdsSecret struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func loadAWS(ctx context.Context) (*Config, error) {

	awsCfg, err := awscfg.LoadDefaultConfig(ctx)

	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	awsCreds, err := awsCfg.Credentials.Retrieve(ctx)

	if err != nil {
		return nil, fmt.Errorf("load aws creds: %w", err)
	}

	ssmClient := ssm.NewFromConfig(awsCfg)
	secretsClient := secretsmanager.NewFromConfig(awsCfg)

	params, err := loadParameters(ctx, ssmClient)

	for k, v := range params {
		slog.Info("parameter", "key", k, "value", v)
	}

	if err != nil {
		return nil, err
	}

	var db rdsSecret

	if err := loadSecret(ctx, secretsClient, params[ParameterRDSSecretArn], &db); err != nil {
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
			User:     db.Username,
			Password: db.Password,
			SSLMode:  "require",
		},

		Server: ServerConfig{
			MetricsServerPort: getEnv(EnvMetricsServerPort, DefaultMetricsServerPort),
			OTelCollectorAddr: getEnv(EnvOtelCollectorAddr, DefaultOtelCollectorAddr),
		},

		AWS: AWSConfig{
			Region:                    awsCfg.Region,
			AccessKeyID:               awsCreds.AccessKeyID,
			SecretAccessKey:           awsCreds.SecretAccessKey,
			CategorizationEventsQueue: params[ParameterSQSCategoryEventsQueueURL],
			NotificationEventsQueue:   params[ParameterSQSNotificationEventsQueueURL],
		},

		AI: AI{

			Groq: AIConfig{
				APIKey:  params[ParameterGroqAPIKey],
				BaseURL: params[ParameterGroqEndpoint],
				Model:   params[ParameterGroqModel],
			},
			Qwen: AIConfig{
				APIKey:  params[ParameterQwenAPIKey],
				BaseURL: params[ParameterQwenEndpoint],
				Model:   params[ParameterQwenModel],
			},
			Gemini: AIConfig{
				APIKey: params[ParameterGeminiAPIKey],
				Model:  params[ParameterGeminiModel],
			},
			Nvidia: AIConfig{
				APIKey:  params[ParameterNvidiaAPIKey],
				BaseURL: params[ParameterNvidiaEndpoint],
				Model:   params[ParameterNvidiaModel],
			},

			HuggingFaceAIModel:  getEnv(EnvHuggingFaceAIModel, DefaultHuggingFaceAIModel),
			LocalOllamaEndpoint: getEnv(EnvLocalOllamaEndpoint, DefaultLocalOllamaEndpoint),
			PromptFile:          getEnv(EnvPromptFilePath, DefaultPromptFilePath),
		},
	}, nil
}

func loadParameters(ctx context.Context, client *ssm.Client) (map[string]string, error) {
	names := []string{
		ssmParameter(ParameterDBHost),
		ssmParameter(ParameterDBPort),
		ssmParameter(ParameterDBName),
		ssmParameter(ParameterRDSSecretArn),
		ssmParameter(ParameterSQSCategoryEventsQueueURL),
		ssmParameter(ParameterSQSNotificationEventsQueueURL),
		ssmParameter(ParameterGroqAPIKey),
		ssmParameter(ParameterGroqEndpoint),
		ssmParameter(ParameterGroqModel),
		ssmParameter(ParameterQwenAPIKey),
		ssmParameter(ParameterQwenEndpoint),
		ssmParameter(ParameterQwenModel),
		ssmParameter(ParameterGeminiAPIKey),
		ssmParameter(ParameterGeminiModel),
		ssmParameter(ParameterNvidiaAPIKey),
		ssmParameter(ParameterNvidiaEndpoint),
		ssmParameter(ParameterNvidiaModel),
	}

	params := make(map[string]string, len(names))

	const batchSize = 10

	for start := 0; start < len(names); start += batchSize {
		end := min(start+batchSize, len(names))

		out, err := client.GetParameters(ctx, &ssm.GetParametersInput{
			Names:          names[start:end],
			WithDecryption: aws.Bool(true),
		})
		if err != nil {
			return nil, fmt.Errorf("read parameter batch: %w", err)
		}

		if len(out.InvalidParameters) > 0 {
			return nil, fmt.Errorf(
				"missing or invalid parameters: %v",
				out.InvalidParameters,
			)
		}

		for _, p := range out.Parameters {
			fullName := aws.ToString(p.Name)

			key, ok := strings.CutPrefix(fullName, SSMParameterPath)
			if !ok {
				return nil, fmt.Errorf(
					"unexpected parameter name %q",
					fullName,
				)
			}

			params[key] = aws.ToString(p.Value)
		}
	}

	return params, nil
}

func loadSecret(
	ctx context.Context,
	client *secretsmanager.Client,
	name string,
	dst any,
) error {

	out, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(name),
	})

	if err != nil {
		return err
	}

	return json.Unmarshal([]byte(aws.ToString(out.SecretString)), dst)
}

func ssmParameter(name string) string {
	return SSMParameterPath + name
}
