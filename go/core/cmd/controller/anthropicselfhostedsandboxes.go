package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/core/pkg/app"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

func loadAnthropicSelfHostedSandboxesConfig(getenv func(string) string) (*app.AnthropicSelfHostedSandboxesConfig, error) {
	enabled := strings.TrimSpace(getenv(env.AnthropicSelfHostedSandboxesEnabled.Name()))
	if enabled == "" {
		return nil, nil
	}
	on, err := strconv.ParseBool(strings.ToLower(enabled))
	if err != nil {
		return nil, fmt.Errorf("invalid %s: expected boolean", env.AnthropicSelfHostedSandboxesEnabled.Name())
	}
	if !on {
		return nil, nil
	}
	cfg := &app.AnthropicSelfHostedSandboxesConfig{
		EnvironmentID:     getenv(env.AnthropicSelfHostedSandboxesEnvironmentID.Name()),
		EnvironmentKey:    getenv(env.AnthropicSelfHostedSandboxesEnvironmentKey.Name()),
		BaseURL:           getenv(env.AnthropicSelfHostedSandboxesBaseURL.Name()),
		TemplateNamespace: getenv(env.AnthropicSelfHostedSandboxesTemplateNamespace.Name()),
		TemplateName:      getenv(env.AnthropicSelfHostedSandboxesTemplateName.Name()),
		SandboxTTL:        12 * time.Hour,
	}
	if value := strings.TrimSpace(getenv(env.AnthropicSelfHostedSandboxesTTL.Name())); value != "" {
		ttl, err := time.ParseDuration(value)
		if err != nil {
			return nil, fmt.Errorf("invalid %s: expected duration", env.AnthropicSelfHostedSandboxesTTL.Name())
		}
		cfg.SandboxTTL = ttl
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}
