package env

import "time"

var (
	SandboxGuestImage = RegisterStringVar("SANDBOX_GUEST_IMAGE", "", "Guest package image pinned by sha256 digest. Required for sandbox preparation and passed unchanged to Substrate.", ComponentController)
	SandboxCPU        = RegisterStringVar("SANDBOX_CPU", "1", "CPU limit for standalone sandbox runtimes.", ComponentController)
	SandboxMemory     = RegisterStringVar("SANDBOX_MEMORY", "1Gi", "Memory limit for standalone sandbox runtimes.", ComponentController)
	SandboxDefaultTTL = RegisterDurationVar("SANDBOX_DEFAULT_TTL", time.Hour, "Default standalone sandbox lifetime.", ComponentController)
	SandboxMaxTTL     = RegisterDurationVar("SANDBOX_MAX_TTL", 24*time.Hour, "Maximum standalone sandbox lifetime, at most 24h.", ComponentController)
)
