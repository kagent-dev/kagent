package openclaw

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// BuildBootstrapJSON builds ~/.openclaw/openclaw.json contents plus environment variables that must be present when
// OpenClaw resolves openshell:resolve:env:<VAR> (API key + channel tokens).
//
// defaultBaseURLWhenUnset is used when ModelConfig has no explicit provider base URL.
// OpenShell callers should pass DefaultInferenceBaseURL; Substrate should pass SubstrateBootstrapDefaultBaseURL.
func BuildBootstrapJSON(ctx context.Context, kube client.Client, namespace string, sbx *v1alpha2.AgentHarness, mc *v1alpha2.ModelConfig, gw GatewayBootstrapConfig, defaultBaseURLWhenUnset string) ([]byte, map[string]string, error) {
	if mc == nil {
		return nil, nil, fmt.Errorf("ModelConfig is required")
	}
	apiKey, err := ResolveModelConfigAPIKey(ctx, kube, mc)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve model API key: %w", err)
	}
	apiAdapter, err := providerAPI(mc)
	if err != nil {
		return nil, nil, err
	}

	apiKeyEnv := DefaultAPIKeyEnvVar(mc.Spec.Provider)
	env := map[string]string{
		apiKeyEnv: apiKey,
	}

	modelID := strings.TrimSpace(mc.Spec.Model)
	if modelID == "" {
		return nil, nil, fmt.Errorf("ModelConfig.spec.model is required for OpenClaw bootstrap JSON")
	}

	providerRecord := GatewayProviderRecordName(mc.Spec.Provider)
	doc := buildCoreBootstrapDocument(mc, gw, apiKeyEnv, providerRecord, modelID, apiAdapter, defaultBaseURLWhenUnset)

	chState, err := accumulateHarnessChannels(ctx, kube, namespace, sbx.Spec.Backend, sbx.Spec.Channels, env)
	if err != nil {
		return nil, nil, err
	}
	doc.Channels = chState.channelsJSON()

	applySecretsAllowlist(&doc, env)

	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal openclaw json: %w", err)
	}
	return raw, env, nil
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

func buildCoreBootstrapDocument(mc *v1alpha2.ModelConfig, gw GatewayBootstrapConfig, apiKeyEnv, providerRecord, modelID, apiAdapter, defaultBaseURLWhenUnset string) bootstrapDocument {
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
						APIKey:  openshellResolveEnv(apiKeyEnv),
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
				APIKey:  openshellResolveEnv(apiKeyEnv),
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

func applySecretsAllowlist(doc *bootstrapDocument, env map[string]string) {
	secretAllow := make([]string, 0, len(env))
	for k := range env {
		secretAllow = append(secretAllow, k)
	}
	slices.Sort(secretAllow)
	doc.Secrets = secretsSection{
		Providers: map[string]secretProvider{
			bootstrapSecretProviderID: {
				Source:    "env",
				Allowlist: secretAllow,
			},
		},
	}
}
