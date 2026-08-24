package config

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"cloud.google.com/go/parametermanager/apiv1/parametermanagerpb"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/googleapis/gax-go/v2"
)

const (
	testGCPProject  = "yevmiye-production"
	testGCPLocation = "global"
	testSecretName  = "projects/yevmiye-production/secrets/worker-postgres"
	testSecretJSON  = `{"dbname":"yevmiye","engine":"postgres","host":"db.internal","password":"db-password","port":5432,"username":"worker"}`
)

type fakeParameterManagerClient struct {
	values   map[string][]byte
	errors   map[string]error
	requests []string
	closed   bool
	closeErr error
}

func (f *fakeParameterManagerClient) RenderParameterVersion(
	_ context.Context,
	req *parametermanagerpb.RenderParameterVersionRequest,
	_ ...gax.CallOption,
) (*parametermanagerpb.RenderParameterVersionResponse, error) {
	f.requests = append(f.requests, req.GetName())
	if err := f.errors[req.GetName()]; err != nil {
		return nil, err
	}
	payload, ok := f.values[req.GetName()]
	if !ok {
		return nil, errors.New("parameter not found")
	}
	return &parametermanagerpb.RenderParameterVersionResponse{RenderedPayload: payload}, nil
}

func (f *fakeParameterManagerClient) Close() error {
	f.closed = true
	return f.closeErr
}

type fakeSecretManagerClient struct {
	payload   []byte
	err       error
	requested string
	closed    bool
	closeErr  error
}

func (f *fakeSecretManagerClient) AccessSecretVersion(
	_ context.Context,
	req *secretmanagerpb.AccessSecretVersionRequest,
	_ ...gax.CallOption,
) (*secretmanagerpb.AccessSecretVersionResponse, error) {
	f.requested = req.GetName()
	if f.err != nil {
		return nil, f.err
	}
	return &secretmanagerpb.AccessSecretVersionResponse{
		Payload: &secretmanagerpb.SecretPayload{Data: f.payload},
	}, nil
}

func (f *fakeSecretManagerClient) Close() error {
	f.closed = true
	return f.closeErr
}

func TestLoadGCPSuccessAndClosesClients(t *testing.T) {
	parameterClient := newFakeParameterClient(validGCPParameters())
	secretClient := &fakeSecretManagerClient{payload: []byte(testSecretJSON)}
	installGCPClientFactories(t, parameterClient, secretClient)

	t.Setenv(EnvGoogleCloudProject, testGCPProject)
	clearEnv(t, EnvGCPParameterLocation)

	cfg, err := loadGCP(context.Background())
	if err != nil {
		t.Fatalf("loadGCP() error = %v", err)
	}
	if !parameterClient.closed || !secretClient.closed {
		t.Fatalf("clients closed = parameter:%v secret:%v, want both true", parameterClient.closed, secretClient.closed)
	}
	if cfg.App.Env != EnvironmentProduction {
		t.Fatalf("environment = %q, want %q", cfg.App.Env, EnvironmentProduction)
	}
	if cfg.GCP.ProjectID != testGCPProject || cfg.GCP.ParameterLocation != testGCPLocation {
		t.Fatalf("GCP config = %#v", cfg.GCP)
	}
	if cfg.GCP.CategorizationEventsTopic != "projects/yevmiye-production/topics/categorization" {
		t.Fatalf("categorization topic = %q", cfg.GCP.CategorizationEventsTopic)
	}
	if cfg.GCP.NotificationEventsTopic != "projects/yevmiye-production/topics/notification" {
		t.Fatalf("notification topic = %q", cfg.GCP.NotificationEventsTopic)
	}
	if cfg.DB.Host != "db.internal" || cfg.DB.Port != "5432" || cfg.DB.Name != "yevmiye" || cfg.DB.User != "worker" || cfg.DB.Password != "db-password" {
		t.Fatal("database config was not assembled correctly")
	}
	if cfg.DB.SSLMode != "require" {
		t.Fatalf("DB SSL mode = %q, want require", cfg.DB.SSLMode)
	}
	if secretClient.requested != testSecretName+"/versions/latest" {
		t.Fatalf("secret resource = %q", secretClient.requested)
	}
	if len(parameterClient.requests) != len(gcpParameterNames()) {
		t.Fatalf("render calls = %d, want %d", len(parameterClient.requests), len(gcpParameterNames()))
	}
}

func TestLoadGCPMissingProjectDoesNotCreateClients(t *testing.T) {
	clearEnv(t, EnvGoogleCloudProject)

	called := false
	previous := newParameterManagerClient
	newParameterManagerClient = func(context.Context) (parameterManagerClient, error) {
		called = true
		return nil, errors.New("must not be called")
	}
	t.Cleanup(func() { newParameterManagerClient = previous })

	_, err := loadGCP(context.Background())
	if err == nil || !strings.Contains(err.Error(), EnvGoogleCloudProject) {
		t.Fatalf("loadGCP() error = %v", err)
	}
	if called {
		t.Fatal("Parameter Manager client was created without a project ID")
	}
}

func TestLoadGCPParametersMissingParameter(t *testing.T) {
	values := validGCPParameters()
	missingName := gcpParameterResource(testGCPProject, testGCPLocation, ParameterGeminiModel)
	delete(values, missingName)
	client := newFakeParameterClient(values)

	_, err := loadGCPParameters(
		context.Background(), client, testGCPProject, testGCPLocation, gcpParameterNames(),
	)
	if err == nil || !strings.Contains(err.Error(), ParameterGeminiModel) {
		t.Fatalf("loadGCPParameters() error = %v, want parameter name", err)
	}
}

func TestGCPParameterNamesMatchAWSParameterSuffixes(t *testing.T) {
	want := make(map[string]struct{}, len(stagingParameterNames()))
	for _, resourceName := range stagingParameterNames() {
		name, ok := strings.CutPrefix(resourceName, SSMParameterPath)
		if !ok {
			t.Fatalf("AWS parameter %q does not use staging prefix", resourceName)
		}
		want[name] = struct{}{}
	}

	got := make(map[string]struct{}, len(gcpParameterNames()))
	for _, name := range gcpParameterNames() {
		got[name] = struct{}{}
	}

	if len(got) != len(want) {
		t.Fatalf("GCP parameter count = %d, want %d", len(got), len(want))
	}
	for name := range want {
		if _, ok := got[name]; !ok {
			t.Errorf("GCP parameters are missing AWS suffix %q", name)
		}
	}
}

func TestLoadGCPRejectsInvalidDBPortWithoutLeakingValue(t *testing.T) {
	params := validGCPParameters()
	const invalidPort = "not-a-port-secret-value"
	params[gcpParameterResource(testGCPProject, testGCPLocation, ParameterDBPort)] = []byte(invalidPort)

	_, err := loadGCPConfig(
		context.Background(),
		newFakeParameterClient(params),
		&fakeSecretManagerClient{payload: []byte(testSecretJSON)},
		testGCPProject,
		testGCPLocation,
	)
	if err == nil || !strings.Contains(err.Error(), ParameterDBPort) {
		t.Fatalf("loadGCPConfig() error = %v", err)
	}
	if strings.Contains(err.Error(), invalidPort) {
		t.Fatalf("error leaked parameter payload: %v", err)
	}
}

func TestLoadGCPRejectsMalformedDatabaseSecretWithoutLeakingPayload(t *testing.T) {
	const malformedSecret = `{"username":"do-not-leak"`

	_, err := loadGCPConfig(
		context.Background(),
		newFakeParameterClient(validGCPParameters()),
		&fakeSecretManagerClient{payload: []byte(malformedSecret)},
		testGCPProject,
		testGCPLocation,
	)
	if err == nil || !strings.Contains(err.Error(), "decode PostgreSQL credential secret JSON") {
		t.Fatalf("loadGCPConfig() error = %v", err)
	}
	if strings.Contains(err.Error(), "do-not-leak") || strings.Contains(err.Error(), malformedSecret) {
		t.Fatalf("error leaked secret payload: %v", err)
	}
}

func TestLoadGCPReportsSecretManagerAccessFailure(t *testing.T) {
	secretClient := &fakeSecretManagerClient{err: errors.New("permission denied")}

	_, err := loadGCPConfig(
		context.Background(),
		newFakeParameterClient(validGCPParameters()),
		secretClient,
		testGCPProject,
		testGCPLocation,
	)
	if err == nil || !strings.Contains(err.Error(), "access PostgreSQL credential secret") || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("loadGCPConfig() error = %v", err)
	}
}

func TestLoadGCPRejectsInvalidSecretResourceWithoutEchoingIt(t *testing.T) {
	params := validGCPParameters()
	const invalidResource = "private-secret-value-that-is-not-a-resource"
	params[gcpParameterResource(testGCPProject, testGCPLocation, ParameterRDSSecretArn)] = []byte(invalidResource)

	_, err := loadGCPConfig(
		context.Background(),
		newFakeParameterClient(params),
		&fakeSecretManagerClient{payload: []byte(testSecretJSON)},
		testGCPProject,
		testGCPLocation,
	)
	if err == nil || !strings.Contains(err.Error(), ParameterRDSSecretArn) {
		t.Fatalf("loadGCPConfig() error = %v", err)
	}
	if strings.Contains(err.Error(), invalidResource) {
		t.Fatalf("error leaked parameter payload: %v", err)
	}
}

func TestLoadGCPReturnsClientCloseErrors(t *testing.T) {
	parameterClient := newFakeParameterClient(validGCPParameters())
	parameterClient.closeErr = errors.New("parameter close failed")
	secretClient := &fakeSecretManagerClient{payload: []byte(testSecretJSON)}
	installGCPClientFactories(t, parameterClient, secretClient)
	t.Setenv(EnvGoogleCloudProject, testGCPProject)

	cfg, err := loadGCP(context.Background())
	if err == nil || !strings.Contains(err.Error(), "close GCP Parameter Manager client") {
		t.Fatalf("loadGCP() error = %v", err)
	}
	if cfg != nil {
		t.Fatalf("loadGCP() config = %#v, want nil when close fails", cfg)
	}
	if !parameterClient.closed || !secretClient.closed {
		t.Fatal("both GCP clients must be closed")
	}
}

func installGCPClientFactories(
	t *testing.T,
	parameterClient parameterManagerClient,
	secretClient secretManagerClient,
) {
	t.Helper()

	previousParameterFactory := newParameterManagerClient
	previousSecretFactory := newSecretManagerClient
	newParameterManagerClient = func(context.Context) (parameterManagerClient, error) {
		return parameterClient, nil
	}
	newSecretManagerClient = func(context.Context) (secretManagerClient, error) {
		return secretClient, nil
	}
	t.Cleanup(func() {
		newParameterManagerClient = previousParameterFactory
		newSecretManagerClient = previousSecretFactory
	})
}

func newFakeParameterClient(values map[string][]byte) *fakeParameterManagerClient {
	return &fakeParameterManagerClient{
		values: values,
		errors: make(map[string]error),
	}
}

func validGCPParameters() map[string][]byte {
	values := map[string]string{
		ParameterDBHost:                        "db.internal",
		ParameterDBPort:                        "5432",
		ParameterDBName:                        "yevmiye",
		ParameterRDSSecretArn:                  testSecretName,
		ParameterSQSCategoryEventsQueueURL:     "projects/yevmiye-production/topics/categorization",
		ParameterSQSNotificationEventsQueueURL: "projects/yevmiye-production/topics/notification",
		ParameterGroqAPIKey:                    "groq-key",
		ParameterGroqEndpoint:                  "https://groq.example",
		ParameterGroqModel:                     "groq-model",
		ParameterOpenRouterAPIKey:              "openrouter-key",
		ParameterOpenRouterEndpoint:            "https://openrouter.example",
		ParameterOpenRouterModel:               "openrouter-model",
		ParameterGeminiAPIKey:                  "gemini-key",
		ParameterGeminiModel:                   "gemini-model",
		ParameterMistralAPIKey:                 "mistral-key",
		ParameterMistralBaseURL:                "https://mistral.example",
		ParameterMistralModel:                  "mistral-model",
		ParameterNvidiaAPIKey:                  "nvidia-key",
		ParameterNvidiaEndpoint:                "https://nvidia.example",
		ParameterNvidiaModel:                   "nvidia-model",
	}

	result := make(map[string][]byte, len(values))
	for name, value := range values {
		result[gcpParameterResource(testGCPProject, testGCPLocation, name)] = []byte(value)
	}
	return result
}

func gcpParameterResource(projectID, location, name string) string {
	return fmt.Sprintf(
		"projects/%s/locations/%s/parameters/%s/versions/latest",
		projectID,
		location,
		name,
	)
}
