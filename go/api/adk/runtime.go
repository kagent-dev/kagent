package adk

import (
	"buf.build/go/protovalidate"
)

// ProcessConfig returns root settings or the standalone zero defaults.
func ProcessConfig(a *AgentConfig) *RuntimeConfig {
	if a == nil || a.Runtime == nil {
		return &RuntimeConfig{}
	}
	return a.Runtime
}

// ValidateRuntime validates the shared contract, including nested root-only settings.
func ValidateRuntime(a *AgentConfig) error { return protovalidate.Validate(a) }
