package env

import (
	"time"

	"github.com/kagent-dev/kagent/go/core/internal/version"
)

var (
	SandboxGuestImage = RegisterStringVar("SANDBOX_GUEST_IMAGE", "ghcr.io/kagent-dev/kagent/sandbox-guest:"+version.Version, "Guest package image. Defaults to the controller release and is resolved to a digest during sandbox preparation.", ComponentController)
	SandboxCPU        = RegisterStringVar("SANDBOX_CPU", "1", "CPU limit for standalone sandbox runtimes.", ComponentController)
	SandboxMemory     = RegisterStringVar("SANDBOX_MEMORY", "1Gi", "Memory limit for standalone sandbox runtimes.", ComponentController)
	SandboxDefaultTTL = RegisterDurationVar("SANDBOX_DEFAULT_TTL", time.Hour, "Default standalone sandbox lifetime.", ComponentController)
	SandboxMaxTTL     = RegisterDurationVar("SANDBOX_MAX_TTL", 24*time.Hour, "Maximum standalone sandbox lifetime, at most 24h.", ComponentController)
)
