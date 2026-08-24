package config

import (
	"context"
	"strings"
	"testing"
)

func TestLoadProductionRequiresGoogleCloudProject(t *testing.T) {
	t.Setenv(EnvApp, EnvironmentProduction)
	clearEnv(t, EnvGoogleCloudProject)

	cfg, err := Load(context.Background())
	if err == nil {
		t.Fatal("expected production configuration to require a project ID")
	}
	if cfg != nil {
		t.Fatalf("expected nil config, got %#v", cfg)
	}
	if !strings.Contains(err.Error(), EnvGoogleCloudProject) {
		t.Fatalf("error = %v, want missing %s", err, EnvGoogleCloudProject)
	}
}
