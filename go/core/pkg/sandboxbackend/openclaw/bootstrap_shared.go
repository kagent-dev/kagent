package openclaw

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

// GatewayBootstrapConfig describes the gateway section of openclaw.json for a harness runtime.
type GatewayBootstrapConfig struct {
	Port      int
	Bind      string // loopback | lan
	AuthMode  string // none | token
	Token     string // required when AuthMode is token
	ControlUI *ControlUIBootstrapConfig
}

// ControlUIBootstrapConfig maps to gateway.controlUi in openclaw.json.
type ControlUIBootstrapConfig struct {
	BasePath                     string
	AllowedOrigins               []string
	DangerouslyDisableDeviceAuth bool
}

// OpenshellGatewayBootstrap is the default gateway profile for OpenShell sandboxes.
func OpenshellGatewayBootstrap(port int) GatewayBootstrapConfig {
	return GatewayBootstrapConfig{Port: port, Bind: "loopback", AuthMode: "none"}
}

// SubstrateGatewayBootstrap is the gateway profile for Agent Substrate actors (port 80, token auth, proxied Control UI).
func SubstrateGatewayBootstrap(token string, port int, controlUIBasePath string) GatewayBootstrapConfig {
	return GatewayBootstrapConfig{
		Port:     port,
		Bind:     "lan",
		AuthMode: "token",
		Token:    strings.TrimSpace(token),
		ControlUI: &ControlUIBootstrapConfig{
			BasePath:                     normalizeControlUIBasePath(controlUIBasePath),
			AllowedOrigins:               []string{"*"},
			DangerouslyDisableDeviceAuth: true,
		},
	}
}

func normalizeControlUIBasePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.TrimRight(path, "/")
}

// BuildGatewayOnlyBootstrapJSON returns a minimal openclaw.json with gateway settings only (no models/channels).
func BuildGatewayOnlyBootstrapJSON(gw GatewayBootstrapConfig) ([]byte, error) {
	doc := bootstrapDocument{Gateway: buildGatewaySection(gw)}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("marshal openclaw json: %w", err)
	}
	return raw, nil
}

func buildCoreBootstrapDocument(mc *v1alpha2.ModelConfig, gw GatewayBootstrapConfig, apiKey credentialValue, providerRecord, modelID, apiAdapter, defaultBaseURLWhenUnset string) bootstrapDocument {
	doc := bootstrapDocument{
		Gateway: buildGatewaySection(gw),
		Agents: agentsSection{
			Defaults: agentDefaults{
				Model: defaultModelPick{
					Primary: fmt.Sprintf("%s/%s", providerRecord, modelID),
				},
			},
		},
	}

	// Substrate: do not emit models.providers without baseUrl (OpenClaw rejects undefined baseUrl).
	// Rely on agents.defaults + API key env unless the user set an explicit URL on ModelConfig.
	if defaultBaseURLWhenUnset == SubstrateBootstrapDefaultBaseURL {
		if explicit := modelConfigExplicitBaseURL(mc); explicit != "" {
			doc.Models = &modelsSection{
				Mode: "merge",
				Providers: map[string]providerSettings{
					providerRecord: {
						BaseURL: explicit,
						APIKey:  apiKey,
						Auth:    providerAuth(mc),
						API:     apiAdapter,
						Models: []modelSlot{
							{ID: modelID, Name: modelID},
						},
					},
				},
			}
		}
		return doc
	}

	baseURL := bootstrapProviderBaseURL(mc, defaultBaseURLWhenUnset)
	doc.Models = &modelsSection{
		Mode: "merge",
		Providers: map[string]providerSettings{
			providerRecord: {
				BaseURL: baseURL,
				APIKey:  apiKey,
				Auth:    providerAuth(mc),
				API:     apiAdapter,
				Models: []modelSlot{
					{ID: modelID, Name: modelID},
				},
			},
		},
	}
	return doc
}

func buildGatewaySection(gw GatewayBootstrapConfig) gatewaySection {
	port := gw.Port
	if port <= 0 {
		port = 18800
	}
	bind := strings.TrimSpace(gw.Bind)
	if bind == "" {
		bind = "loopback"
	}
	authMode := strings.TrimSpace(gw.AuthMode)
	if authMode == "" {
		authMode = "none"
	}
	section := gatewaySection{
		Mode: "local",
		Bind: bind,
		Auth: gatewayAuth{Mode: authMode},
		Port: port,
	}
	if authMode == "token" {
		section.Auth.Token = gw.Token
	}
	if gw.ControlUI != nil {
		section.ControlUi = &controlUiSection{
			BasePath:                     normalizeControlUIBasePath(gw.ControlUI.BasePath),
			AllowedOrigins:               gw.ControlUI.AllowedOrigins,
			DangerouslyDisableDeviceAuth: gw.ControlUI.DangerouslyDisableDeviceAuth,
		}
	}
	return section
}

func requiredModelID(mc *v1alpha2.ModelConfig) (string, error) {
	modelID := strings.TrimSpace(mc.Spec.Model)
	if modelID == "" {
		return "", fmt.Errorf("ModelConfig.spec.model is required for OpenClaw bootstrap JSON")
	}
	return modelID, nil
}
