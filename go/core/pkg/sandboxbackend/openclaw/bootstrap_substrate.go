package openclaw

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildSubstrateBootstrapJSON builds openclaw.json and ActorTemplate container env for Agent Substrate.
// Model and channel credentials use OpenClaw env SecretRefs in openclaw.json ({source:"env",provider:"default",id:"..."})
// and ActorTemplate container env (literal value or valueFrom secretKeyRef/configMapKeyRef, resolved by ate-api at resume).
func BuildSubstrateBootstrapJSON(ctx context.Context, kube client.Client, namespace string, sbx *v1alpha2.AgentHarness, mc *v1alpha2.ModelConfig, gw GatewayBootstrapConfig) ([]byte, []corev1.EnvVar, error) {
	if mc == nil {
		return nil, nil, fmt.Errorf("ModelConfig is required")
	}
	apiKeyEnvVar, err := ModelConfigAPIKeyEnvVar(mc)
	if err != nil {
		return nil, nil, err
	}
	apiAdapter, err := providerAPI(mc)
	if err != nil {
		return nil, nil, err
	}

	modelID, err := requiredModelID(mc)
	if err != nil {
		return nil, nil, err
	}

	apiKeyEnv := apiKeyEnvVar.Name
	providerRecord := GatewayProviderRecordName(mc.Spec.Provider)
	apiKeyRef := openclawEnvSecretRef(apiKeyEnv)
	doc := buildCoreBootstrapDocument(mc, gw, credentialValue{envSecret: &apiKeyRef}, providerRecord, modelID, apiAdapter, SubstrateBootstrapDefaultBaseURL)

	chState, channelEnv, err := accumulateSubstrateHarnessChannels(ctx, kube, namespace, sbx.Spec.Channels)
	if err != nil {
		return nil, nil, err
	}
	doc.Channels = chState.channelsJSON()

	applySubstrateSecretsAllowlist(&doc, apiKeyEnv, channelEnv)

	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal openclaw json: %w", err)
	}
	return raw, substrateContainerEnv(apiKeyEnvVar, channelEnv), nil
}

func substrateContainerEnv(apiKey corev1.EnvVar, extra []corev1.EnvVar) []corev1.EnvVar {
	out := make([]corev1.EnvVar, 0, len(extra)+2)
	out = append(out, apiKey)
	out = append(out, extra...)
	out = append(out, corev1.EnvVar{Name: "HOME", Value: "/root"})
	return out
}

func applySubstrateSecretsAllowlist(doc *bootstrapDocument, apiKeyEnv string, channelEnv []corev1.EnvVar) {
	seen := make(map[string]struct{}, len(channelEnv)+1)
	secretAllow := make([]string, 0, len(channelEnv)+1)
	add := func(name string) {
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		secretAllow = append(secretAllow, name)
	}
	add(apiKeyEnv)
	for _, env := range channelEnv {
		add(env.Name)
	}
	slices.Sort(secretAllow)
	doc.Secrets = secretsSection{
		Providers: map[string]secretProvider{
			substrateSecretProviderID: {
				Source:    "env",
				Allowlist: secretAllow,
			},
		},
		Defaults: &secretsDefaults{Env: substrateSecretProviderID},
	}
}
