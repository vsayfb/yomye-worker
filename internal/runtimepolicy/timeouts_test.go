package runtimepolicy

import (
	"testing"
	"time"
)

func TestRuntimeBudgetsRemainInternallyConsistent(t *testing.T) {
	if MessageProcessingTimeout != 2*time.Minute {
		t.Fatalf("message processing timeout = %s, want 2m", MessageProcessingTimeout)
	}
	if MessageVisibilityTimeout != 3*time.Minute {
		t.Fatalf("message visibility timeout = %s, want 3m", MessageVisibilityTimeout)
	}
	if MessageVisibilityTimeout <= MessageProcessingTimeout {
		t.Fatalf("visibility timeout %s must exceed processing timeout %s", MessageVisibilityTimeout, MessageProcessingTimeout)
	}
	if AIProviderAttemptTimeout != 25*time.Second {
		t.Fatalf("provider timeout = %s, want 25s", AIProviderAttemptTimeout)
	}
	if AIFallbackTotalTimeout != 75*time.Second {
		t.Fatalf("fallback timeout = %s, want 75s", AIFallbackTotalTimeout)
	}
	if AIProviderAttemptTimeout >= AIFallbackTotalTimeout {
		t.Fatalf("provider timeout %s must be lower than fallback timeout %s", AIProviderAttemptTimeout, AIFallbackTotalTimeout)
	}
	if AIFallbackTotalTimeout >= MessageProcessingTimeout {
		t.Fatalf("fallback timeout %s must leave room inside message timeout %s", AIFallbackTotalTimeout, MessageProcessingTimeout)
	}
	if ModerationProviderAttemptTimeout != 10*time.Second {
		t.Fatalf("moderation provider timeout = %s, want 10s", ModerationProviderAttemptTimeout)
	}
	if ModerationFallbackTotalTimeout != 30*time.Second {
		t.Fatalf("moderation fallback timeout = %s, want 30s", ModerationFallbackTotalTimeout)
	}
	if ModerationProviderAttemptTimeout >= ModerationFallbackTotalTimeout {
		t.Fatalf("moderation provider timeout %s must be lower than total %s", ModerationProviderAttemptTimeout, ModerationFallbackTotalTimeout)
	}
	if ModerationFallbackTotalTimeout+AIFallbackTotalTimeout >= MessageProcessingTimeout {
		t.Fatalf("AI budgets %s + %s must leave room inside message timeout %s", ModerationFallbackTotalTimeout, AIFallbackTotalTimeout, MessageProcessingTimeout)
	}
	if ApplicationShutdownTimeout != 10*time.Second {
		t.Fatalf("shutdown timeout = %s, want 10s", ApplicationShutdownTimeout)
	}
}
