package openclaw

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

// GatewayBootstrapConfig describes the gateway section of openclaw.json for a harness runtime.
type GatewayBootstrapConfig struct {
	Port     int
	Bind     string // loopback | lan
	AuthMode string // none
}

// SubstrateGatewayBootstrap is the gateway profile for Agent Substrate actors
// (no auth, loopback-only). The gateway has no Control UI and is a private
// in-sandbox detail the `openclaw acp` child connects to over loopback;
// kagent reaches the actor solely through the acp-shim's /acp WebSocket, which
// is itself only exposed via the controller's same-origin proxy.
//
// Bind MUST be "loopback": OpenClaw refuses to bind the gateway to "lan" when
// auth.mode is "none" ("Refusing to bind gateway to lan without auth"), so a
// lan bind would make the gateway exit without listening on :18789, the acp
// child would never spawn, and chats would hang. The gateway is only ever
// reached over 127.0.0.1 by the in-sandbox child, so loopback is both correct
// and the only bind permitted without a token.
func SubstrateGatewayBootstrap(port int) GatewayBootstrapConfig {
	return GatewayBootstrapConfig{
		Port:     port,
		Bind:     "loopback",
		AuthMode: "none",
	}
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
	return section
}

func requiredModelID(mc *v1alpha2.ModelConfig) (string, error) {
	modelID := strings.TrimSpace(mc.Spec.Model)
	if modelID == "" {
		return "", fmt.Errorf("ModelConfig.spec.model is required for OpenClaw bootstrap JSON")
	}
	return modelID, nil
}
