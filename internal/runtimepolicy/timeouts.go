package runtimepolicy

import "time"

// Cross-package runtime budgets are centralized here so queue visibility,
// message processing, AI fallback, and shutdown policies stay internally
// consistent as the service evolves.
const (
	MessageProcessingTimeout = 2 * time.Minute
	MessageVisibilityTimeout = 3 * time.Minute

	AIProviderAttemptTimeout         = 25 * time.Second
	AIFallbackTotalTimeout           = 75 * time.Second
	ModerationProviderAttemptTimeout = 10 * time.Second
	ModerationFallbackTotalTimeout   = 30 * time.Second

	ApplicationShutdownTimeout = 10 * time.Second
)
