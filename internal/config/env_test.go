package config

import (
	"os"
	"strings"
	"testing"
)

// requiredEnvVars mirrors exactly what loadEnv() treats as required, so this
// test stays accurate if that list changes.
var requiredEnvVars = []string{
	EnvDBHost,
	EnvDBPort,
	EnvDBUser,
	EnvDBPassword,
	EnvDBName,
	EnvAWSRegion,
	EnvAWSAccessKeyID,
	EnvAWSSecretAccessKey,
	EnvCategorizationSQSUrl,
	EnvNotificationSQSUrl,
	EnvAWSSQSEndpont,
}

// clearEnv unsets the given keys for the duration of the test and restores
// whatever they were (set or unset) afterwards. testing.T.Setenv only
// covers "set to a value", not "make sure it's absent", hence this helper.
func clearEnv(t *testing.T, keys ...string) {
	t.Helper()

	for _, key := range keys {
		original, wasSet := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}

		t.Cleanup(func() {
			if wasSet {
				os.Setenv(key, original)
			} else {
				os.Unsetenv(key)
			}
		})
	}
}

func TestLoadEnv_MissingRequiredVarsAreAllReported(t *testing.T) {
	clearEnv(t, requiredEnvVars...)

	_, err := loadEnv()
	if err == nil {
		t.Fatal("expected an error when required env vars are missing")
	}

	for _, key := range requiredEnvVars {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("expected error to mention missing var %s, got: %v", key, err)
		}
	}
}

func TestLoadEnv_AllRequiredSetSucceeds(t *testing.T) {
	values := map[string]string{
		EnvDBHost:               "localhost",
		EnvDBPort:               "5432",
		EnvDBUser:               "postgres",
		EnvDBPassword:           "secret",
		EnvDBName:               "categorization",
		EnvAWSRegion:            "us-east-1",
		EnvAWSAccessKeyID:       "test-access-key",
		EnvAWSSecretAccessKey:   "test-secret-key",
		EnvCategorizationSQSUrl: "http://localhost:4566/queue/categorization",
		EnvNotificationSQSUrl:   "http://localhost:4566/queue/notification",
		EnvAWSSQSEndpont:        "http://localhost:4566",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}
	clearEnv(t, EnvWorkerConcurrency)

	cfg, err := loadEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.DB.Host != "localhost" {
		t.Errorf("expected DB.Host %q, got %q", "localhost", cfg.DB.Host)
	}
	if cfg.AWS.Region != "us-east-1" {
		t.Errorf("expected AWS.Region %q, got %q", "us-east-1", cfg.AWS.Region)
	}
	if cfg.Server.WorkerConcurrency != DefaultWorkerConcurrency {
		t.Errorf("expected default WorkerConcurrency %d, got %d", DefaultWorkerConcurrency, cfg.Server.WorkerConcurrency)
	}
}

func TestGetIntEnv(t *testing.T) {
	const key = "TEST_GET_INT_ENV_VALUE"
	const defaultValue = 7

	tests := []struct {
		name  string
		value string
		unset bool
		want  int
	}{
		{name: "unset falls back to default", unset: true, want: defaultValue},
		{name: "empty falls back to default", value: "", want: defaultValue},
		{name: "valid positive integer is used", value: "42", want: 42},
		{name: "non-numeric falls back to default", value: "not-a-number", want: defaultValue},
		{name: "zero falls back to default", value: "0", want: defaultValue},
		{name: "negative falls back to default", value: "-3", want: defaultValue},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.unset {
				clearEnv(t, key)
			} else {
				t.Setenv(key, tt.value)
			}

			got := getIntEnv(key, defaultValue)
			if got != tt.want {
				t.Errorf("getIntEnv(%q, %d) = %d, want %d", key, defaultValue, got, tt.want)
			}
		})
	}
}
