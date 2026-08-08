package awsclient

import (
	"context"
	"testing"

	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
)

func TestNew_StagingUsesDefaultCredentialChain(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "rotating-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "rotating-secret-key")
	t.Setenv("AWS_SESSION_TOKEN", "rotating-session-token")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	cfg := &config.Config{
		App: config.APP{Env: config.EnvironmentStaging},
		AWS: config.AWSConfig{
			Region:          "us-east-1",
			AccessKeyID:     "stale-access-key",
			SecretAccessKey: "stale-secret-key",
		},
	}

	awsCfg, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("create aws config: %v", err)
	}

	creds, err := awsCfg.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("retrieve aws credentials: %v", err)
	}

	if creds.AccessKeyID != "rotating-access-key" {
		t.Fatalf("access key = %q, want default-chain credential", creds.AccessKeyID)
	}
	if creds.SecretAccessKey != "rotating-secret-key" {
		t.Fatalf("secret key = %q, want default-chain credential", creds.SecretAccessKey)
	}
	if creds.SessionToken != "rotating-session-token" {
		t.Fatalf("session token = %q, want temporary credential token", creds.SessionToken)
	}
}

func TestNew_DevelopmentUsesConfiguredStaticCredentials(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "environment-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "environment-secret-key")
	t.Setenv("AWS_SESSION_TOKEN", "environment-session-token")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	cfg := &config.Config{
		App: config.APP{Env: config.EnvironmentDevelopment},
		AWS: config.AWSConfig{
			Region:          "us-east-1",
			AccessKeyID:     "local-access-key",
			SecretAccessKey: "local-secret-key",
		},
	}

	awsCfg, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("create aws config: %v", err)
	}

	creds, err := awsCfg.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("retrieve aws credentials: %v", err)
	}

	if creds.AccessKeyID != "local-access-key" {
		t.Fatalf("access key = %q, want configured local credential", creds.AccessKeyID)
	}
	if creds.SecretAccessKey != "local-secret-key" {
		t.Fatalf("secret key = %q, want configured local credential", creds.SecretAccessKey)
	}
	if creds.SessionToken != "" {
		t.Fatalf("session token = %q, want empty local static token", creds.SessionToken)
	}
}
