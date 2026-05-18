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

// BuildBootstrapJSON builds ~/.openclaw/openclaw.json contents plus environment variables that must be present when
// OpenClaw resolves openshell:resolve:env:<VAR> (API key + channel tokens).
func BuildBootstrapJSON(ctx context.Context, kube client.Client, namespace string, sbx *v1alpha2.AgentHarness, mc *v1alpha2.ModelConfig, gwPort int) ([]byte, map[string]string, error) {
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
	doc := buildCoreBootstrapDocument(mc, gwPort, apiKeyEnv, providerRecord, modelID, apiAdapter)

	chState, err := accumulateHarnessChannels(ctx, kube, namespace, sbx.Spec.Channels, env)
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

func buildCoreBootstrapDocument(mc *v1alpha2.ModelConfig, gwPort int, apiKeyEnv, providerRecord, modelID, apiAdapter string) bootstrapDocument {
	baseURL := bootstrapProviderBaseURL(mc)
	return bootstrapDocument{
		Gateway: gatewaySection{
			Mode: "local",
			Bind: "loopback",
			Auth: gatewayAuth{Mode: "none"},
			Port: gwPort,
		},
		Models: modelsSection{
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
		},
		Agents: agentsSection{
			Defaults: agentDefaults{
				Model: defaultModelPick{
					Primary: fmt.Sprintf("%s/%s", providerRecord, modelID),
				},
			},
		},
	}
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
