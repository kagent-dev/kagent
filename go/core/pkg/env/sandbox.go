package env

import "time"

var (
	SandboxGuestImage             = RegisterStringVar("KAGENT_SANDBOX_GUEST_IMAGE", "", "Guest package image pinned by sha256 digest. Required for sandbox preparation and passed unchanged to Substrate.", ComponentController)
	SandboxCPU                    = RegisterStringVar("KAGENT_SANDBOX_CPU", "1", "CPU limit for standalone sandbox runtimes.", ComponentController)
	SandboxMemory                 = RegisterStringVar("KAGENT_SANDBOX_MEMORY", "1Gi", "Memory limit for standalone sandbox runtimes.", ComponentController)
	SandboxDefaultTTL             = RegisterDurationVar("KAGENT_SANDBOX_DEFAULT_TTL", time.Hour, "Default standalone sandbox lifetime.", ComponentController)
	SandboxMaxTTL                 = RegisterDurationVar("KAGENT_SANDBOX_MAX_TTL", 24*time.Hour, "Maximum standalone sandbox lifetime, at most 24h.", ComponentController)
	SandboxExpirationPollInterval = RegisterDurationVar("KAGENT_SANDBOX_EXPIRATION_POLL_INTERVAL", time.Second, "Interval between expired sandbox cleanup batches. Must be positive; longer intervals delay deletion after TTL expiry.", ComponentController)
)
