package config

import (
	"context"
	"strings"
	"testing"
)

func TestLoadProductionFailsExplicitlyUntilImplemented(t *testing.T) {
	t.Setenv(EnvApp, EnvironmentProduction)

	cfg, err := Load(context.Background())
	if err == nil {
		t.Fatal("expected production configuration to fail explicitly")
	}
	if cfg != nil {
		t.Fatalf("expected nil config, got %#v", cfg)
	}
	if !strings.Contains(err.Error(), "production configuration is not implemented") {
		t.Fatalf("unexpected error: %v", err)
	}
}
