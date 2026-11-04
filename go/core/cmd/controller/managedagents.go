package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/core/pkg/app"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

func loadManagedAgentsConfig(getenv func(string) string) (*app.ManagedAgentsConfig, error) {
	enabled := strings.TrimSpace(getenv(env.ManagedAgentsEnabled.Name()))
	if enabled == "" {
		return nil, nil
	}
	on, err := strconv.ParseBool(strings.ToLower(enabled))
	if err != nil {
		return nil, fmt.Errorf("invalid %s: expected boolean", env.ManagedAgentsEnabled.Name())
	}
	if !on {
		return nil, nil
	}
	cfg := &app.ManagedAgentsConfig{
		EnvironmentID:     getenv(env.ManagedAgentsEnvironmentID.Name()),
		EnvironmentKey:    getenv(env.ManagedAgentsEnvironmentKey.Name()),
		BaseURL:           getenv(env.ManagedAgentsBaseURL.Name()),
		TemplateNamespace: getenv(env.ManagedAgentsTemplateNamespace.Name()),
		TemplateName:      getenv(env.ManagedAgentsTemplateName.Name()),
		SandboxTTL:        12 * time.Hour,
	}
	if value := strings.TrimSpace(getenv(env.ManagedAgentsSandboxTTL.Name())); value != "" {
		ttl, err := time.ParseDuration(value)
		if err != nil {
			return nil, fmt.Errorf("invalid %s: expected duration", env.ManagedAgentsSandboxTTL.Name())
		}
		cfg.SandboxTTL = ttl
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}
