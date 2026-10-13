package adk

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

// RuntimeConfig configures the root process. Subagents inherit these settings.
// Session and Actor identity are projected separately by Substrate on launch.
type RuntimeConfig struct {
	ControlPlaneURL string `json:"control_plane_url,omitempty"`
	GatewayURL      string `json:"gateway_url,omitempty"`
	// Name and Namespace identify the runnable Agent Kubernetes resource.
	Name             string                   `json:"name,omitempty"`
	Namespace        string                   `json:"namespace,omitempty"`
	LogLevel         string                   `json:"log_level,omitempty"`
	Host             string                   `json:"host,omitempty"`
	Port             string                   `json:"port,omitempty"`
	MaxContentLength *int64                   `json:"max_content_length,omitempty"`
	TokenPropagation TokenPropagationConfig   `json:"token_propagation,omitzero"`
	BashVenvPath     string                   `json:"bash_venv_path,omitempty"`
	FileSearchTools  bool                     `json:"file_search_tools,omitempty"`
	Telemetry        tracing.RuntimeTelemetry `json:"telemetry,omitzero"`
}

type TokenPropagationConfig struct {
	Enabled         bool     `json:"enabled,omitempty"`
	STSWellKnownURI string   `json:"sts_well_known_uri,omitempty"`
	Resources       []string `json:"resources,omitempty"`
	Audiences       []string `json:"audiences,omitempty"`
}

func (c RuntimeConfig) Validate() error {
	for name, value := range map[string]string{"control_plane_url": c.ControlPlaneURL, "gateway_url": c.GatewayURL, "token_propagation.sts_well_known_uri": c.TokenPropagation.STSWellKnownURI} {
		if value == "" {
			continue
		}
		u, err := url.Parse(value)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("runtime.%s must be an HTTP(S) URL", name)
		}
	}
	switch c.LogLevel {
	case "", "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("runtime.log_level must be debug, info, warn, or error")
	}
	if c.Port != "" {
		n, err := strconv.Atoi(c.Port)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("runtime.port must be between 1 and 65535")
		}
	}
	if c.MaxContentLength != nil && *c.MaxContentLength < 0 {
		return fmt.Errorf("runtime.max_content_length must be nonnegative (zero means unlimited)")
	}
	for _, value := range append(append([]string{}, c.TokenPropagation.Resources...), c.TokenPropagation.Audiences...) {
		if value == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("runtime.token_propagation resources and audiences must be nonempty and unpadded")
		}
	}
	return c.Telemetry.Validate()
}

// ProcessConfig returns root settings or the standalone zero defaults.
func (a *AgentConfig) ProcessConfig() RuntimeConfig {
	if a == nil || a.Runtime == nil {
		return RuntimeConfig{}
	}
	return *a.Runtime
}

func (a *AgentConfig) ValidateRuntime() error {
	if a.Runtime != nil {
		if err := a.Runtime.Validate(); err != nil {
			return err
		}
	}
	for _, child := range a.SubAgents {
		if child == nil {
			return fmt.Errorf("subagent must not be null")
		}
		if child.Runtime != nil {
			return fmt.Errorf("runtime settings are only allowed on the root agent")
		}
		if err := child.ValidateRuntime(); err != nil {
			return err
		}
	}
	return nil
}
