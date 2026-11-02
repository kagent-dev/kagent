package config

import configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"

// ProcessConfig returns root settings or the standalone zero defaults.
func ProcessConfig(a *configv1.ADKConfig) *configv1.ADKRuntimeConfig {
	if a == nil || a.Runtime == nil {
		return &configv1.ADKRuntimeConfig{}
	}
	return a.Runtime
}
