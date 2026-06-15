package openclaw

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildBootstrapJSON builds ~/.openclaw/openclaw.json contents plus environment variables that must be present when
// OpenClaw resolves openshell:resolve:env:<VAR> (API key + channel tokens).
//
// defaultBaseURLWhenUnset is used when ModelConfig has no explicit provider base URL.
// OpenShell callers should pass DefaultInferenceBaseURL.
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

	modelID, err := requiredModelID(mc)
	if err != nil {
		return nil, nil, err
	}

	providerRecord := GatewayProviderRecordName(mc.Spec.Provider)
	doc := buildCoreBootstrapDocument(mc, gw, credentialValue{literal: openshellResolveEnv(apiKeyEnv)}, providerRecord, modelID, apiAdapter, defaultBaseURLWhenUnset)

	chState, err := accumulateHarnessChannels(ctx, kube, namespace, sbx.Spec.Channels, env)
	if err != nil {
		return nil, nil, err
	}
	doc.Channels = chState.channelsJSON()

	applyOpenshellSecretsAllowlist(&doc, env)

	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal openclaw json: %w", err)
	}
	return raw, env, nil
}

func applyOpenshellSecretsAllowlist(doc *bootstrapDocument, env map[string]string, extraEnvNames ...string) {
	seen := make(map[string]struct{}, len(env)+len(extraEnvNames))
	secretAllow := make([]string, 0, len(env)+len(extraEnvNames))
	for k := range env {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			secretAllow = append(secretAllow, k)
		}
	}
	for _, k := range extraEnvNames {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			secretAllow = append(secretAllow, k)
		}
	}
	slices.Sort(secretAllow)
	doc.Secrets = secretsSection{
		Providers: map[string]secretProvider{
			openshellSecretProviderID: {
				Source:    "env",
				Allowlist: secretAllow,
			},
		},
	}
}
