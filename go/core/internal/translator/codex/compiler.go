// Package codex compiles resolved v1alpha3 inputs for the native Codex Harness adapter.
package codex

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	codexconfig "github.com/kagent-dev/kagent/go/harness/codex/config"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	codexHomeEnv        = "CODEX_HOME"
	openAIAPIKeyEnv     = "OPENAI_API_KEY"
	awsRegionEnv        = "AWS_REGION"
	awsBedrockTokenEnv  = "AWS_BEARER_TOKEN_BEDROCK"
	awsAccessKeyEnv     = "AWS_ACCESS_KEY_ID"
	awsSecretKeyEnv     = "AWS_SECRET_ACCESS_KEY"
	awsSessionTokenEnv  = "AWS_SESSION_TOKEN"
	mcpCredentialPrefix = "KAGENT_CODEX_MCP_CREDENTIAL_"
)

var ownedEnvironment = map[string]struct{}{
	codexHomeEnv: {}, openAIAPIKeyEnv: {}, awsRegionEnv: {}, awsBedrockTokenEnv: {},
	awsAccessKeyEnv: {}, awsSecretKeyEnv: {}, awsSessionTokenEnv: {},
}

type Compiler struct {
	ctx         krt.HandlerContext
	collections v2translator.Collections
}

func NewCompiler(ctx krt.HandlerContext, collections v2translator.Collections) *Compiler {
	return &Compiler{ctx: ctx, collections: collections}
}

func (c *Compiler) Compile(ctx context.Context, input *v2translator.HarnessInput) (*v2translator.CompileResult, error) {
	if input == nil || input.Harness == nil || input.Root == nil || input.Root.Template == nil || input.Root.ResolvedModelConfig == nil || input.Root.ResolvedModelConfig.Config == nil {
		return nil, fmt.Errorf("codex compiler requires a resolved Harness, AgentTemplate, and ModelConfig")
	}
	model := input.Root.ResolvedModelConfig.Config
	if strings.TrimSpace(model.Spec.Model) == "" {
		return nil, v2translator.NewValidationError("Codex ModelConfig model is required")
	}
	if len(model.Spec.DefaultHeaders) != 0 || !model.Spec.TLS.IsEmpty() || model.Spec.APIKeyPassthrough {
		return nil, v2translator.NewValidationError("Codex does not support ModelConfig defaultHeaders, TLS, or apiKeyPassthrough")
	}

	provider, providerEnvironment, egress, err := c.compileProvider(ctx, model)
	if err != nil {
		return nil, err
	}
	skillResources, skillEgress, err := v2translator.CompileSkillResources(input.Root.Template)
	if err != nil {
		return nil, err
	}
	mcp, err := c.compileMCP(ctx, input.Root.Template.Namespace, input.Root.MCPTools)
	if err != nil {
		return nil, err
	}
	environment := append(providerEnvironment, mcp.environment...)
	for _, variable := range input.Harness.Spec.Env {
		if _, reserved := ownedEnvironment[variable.Name]; reserved || strings.HasPrefix(variable.Name, mcpCredentialPrefix) {
			return nil, v2translator.NewValidationError("Harness env %q conflicts with Codex's compiled configuration", variable.Name)
		}
		envVar := corev1.EnvVar{Name: variable.Name}
		if variable.Value != nil {
			envVar.Value = *variable.Value
		} else {
			envVar.ValueFrom = &corev1.EnvVarSource{SecretKeyRef: variable.CredentialRef.DeepCopy()}
		}
		environment = append(environment, envVar)
	}
	agents, err := compileAgents(input.Root)
	if err != nil {
		return nil, err
	}
	cfg := codexconfig.Production(model.Spec.Model, input.Root.Instruction)
	cfg.Provider, cfg.Agents, cfg.MCPServers = provider, agents, mcp.servers
	if len(skillResources.Skills) != 0 || len(skillResources.Plugins) != 0 {
		cfg.SkillResources = &skillResources
	}
	if err := cfg.Validate(); err != nil {
		return nil, v2translator.NewValidationError("invalid compiled Codex configuration: %v", err)
	}
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal Codex config: %w", err)
	}
	cardJSON, err := json.Marshal(agentTemplateCard(input.Root.Template))
	if err != nil {
		return nil, fmt.Errorf("marshal Codex agent card: %w", err)
	}
	provenance, err := c.buildProvenance(ctx, input, environment, configJSON, cardJSON)
	if err != nil {
		return nil, fmt.Errorf("build Codex revision provenance: %w", err)
	}
	environment, err = c.resolveEnvironment(ctx, input.Harness.Namespace, environment)
	if err != nil {
		return nil, fmt.Errorf("resolve Codex runtime environment: %w", err)
	}
	egress = append(egress, skillEgress...)
	egress = append(egress, mcp.egress...)
	slices.Sort(egress)
	egress = slices.Compact(egress)
	template, harness := input.Root.Template, input.Harness
	return &v2translator.CompileResult{
		Revision: v2translator.Revision{
			Namespace: template.Namespace, AgentTemplateName: template.Name, HarnessName: harness.Name,
			Image: harness.Spec.Workload.Image, Environment: environment, ConfigJSON: configJSON, AgentCardJSON: cardJSON,
			WorkerPoolName: harness.Spec.Substrate.WorkerPoolRef.Name, SnapshotLocation: harness.Spec.Substrate.SnapshotPolicy.Location,
			Provenance: provenance, EgressDestinations: egress,
		},
		Warnings: mcp.warnings,
	}, nil
}

func (c *Compiler) compileProvider(ctx context.Context, model *v1alpha3.ModelConfig) (codexconfig.Provider, []corev1.EnvVar, []string, error) {
	switch model.Spec.Provider {
	case v1alpha3.ModelProviderOpenAI:
		if model.Spec.OpenAI == nil || model.Spec.OpenAI.APIFormat == nil || *model.Spec.OpenAI.APIFormat != v1alpha3.OpenAIAPIFormatResponses {
			return codexconfig.Provider{}, nil, nil, v2translator.NewValidationError("Codex OpenAI requires openAI.apiFormat responses")
		}
		options := *model.Spec.OpenAI
		baseURL := strings.TrimSpace(options.BaseURL)
		options.BaseURL, options.APIFormat = "", nil
		if !reflect.DeepEqual(options, v1alpha3.OpenAIConfig{}) {
			return codexconfig.Provider{}, nil, nil, v2translator.NewValidationError("Codex does not support OpenAI provider options beyond baseUrl and apiFormat responses")
		}
		if err := c.requireSecretKey(ctx, model.Namespace, model.Spec.APIKeySecret, model.Spec.APIKeySecretKey); err != nil {
			return codexconfig.Provider{}, nil, nil, err
		}
		provider := codexconfig.Provider{Name: "openai", BaseURL: baseURL}
		egress := []string{"api.openai.com"}
		if baseURL != "" {
			host, err := absoluteHTTPHostname(baseURL)
			if err != nil {
				return codexconfig.Provider{}, nil, nil, v2translator.NewValidationError("Codex OpenAI baseUrl %v", err)
			}
			egress = []string{host}
		}
		return provider, []corev1.EnvVar{secretEnvironment(openAIAPIKeyEnv, model.Spec.APIKeySecret, model.Spec.APIKeySecretKey)}, egress, nil
	case v1alpha3.ModelProviderBedrock:
		if model.Spec.Bedrock == nil || strings.TrimSpace(model.Spec.Bedrock.Region) == "" {
			return codexconfig.Provider{}, nil, nil, v2translator.NewValidationError("Codex Bedrock requires bedrock.region")
		}
		options := *model.Spec.Bedrock
		region := strings.TrimSpace(options.Region)
		if !strings.HasPrefix(model.Spec.Model, "gpt-") {
			return codexconfig.Provider{}, nil, nil, v2translator.NewValidationError("Codex Bedrock supports only OpenAI gpt-* model IDs")
		}
		options.Region = ""
		if options.CacheTTL == "5m" {
			options.CacheTTL = ""
		}
		if !reflect.DeepEqual(options, v1alpha3.BedrockConfig{}) {
			return codexconfig.Provider{}, nil, nil, v2translator.NewValidationError("Codex does not support Bedrock provider options beyond region")
		}
		if model.Spec.APIKeySecret == "" || model.Spec.APIKeySecretKey != "" {
			return codexconfig.Provider{}, nil, nil, v2translator.NewValidationError("Codex Bedrock requires apiKeySecret and an empty apiKeySecretKey")
		}
		secret, err := c.secret(ctx, model.Namespace, model.Spec.APIKeySecret)
		if err != nil {
			return codexconfig.Provider{}, nil, nil, err
		}
		environment := []corev1.EnvVar{{Name: awsRegionEnv, Value: region}}
		if len(secret.Data[awsBedrockTokenEnv]) != 0 {
			environment = append(environment, secretEnvironment(awsBedrockTokenEnv, secret.Name, awsBedrockTokenEnv))
		} else {
			for _, key := range []string{awsAccessKeyEnv, awsSecretKeyEnv} {
				if len(secret.Data[key]) == 0 {
					return codexconfig.Provider{}, nil, nil, v2translator.NewValidationError("Codex Bedrock Secret %q requires %s and %s, or %s", secret.Name, awsAccessKeyEnv, awsSecretKeyEnv, awsBedrockTokenEnv)
				}
				environment = append(environment, secretEnvironment(key, secret.Name, key))
			}
			if len(secret.Data[awsSessionTokenEnv]) != 0 {
				environment = append(environment, secretEnvironment(awsSessionTokenEnv, secret.Name, awsSessionTokenEnv))
			}
		}
		return codexconfig.Provider{Name: "amazon-bedrock"}, environment, []string{"bedrock-runtime." + region + ".amazonaws.com"}, nil
	default:
		return codexconfig.Provider{}, nil, nil, v2translator.NewValidationError("Codex does not support ModelConfig provider %q", model.Spec.Provider)
	}
}

func compileAgents(root *v2translator.AgentInput) (map[string]codexconfig.Agent, error) {
	if len(root.Shared) == 0 {
		return nil, nil
	}
	agents := make(map[string]codexconfig.Agent, len(root.Shared))
	for _, binding := range root.Shared {
		child := binding.Agent
		if child == nil || child.Template == nil || child.ResolvedModelConfig == nil || child.ResolvedModelConfig.Config == nil {
			return nil, fmt.Errorf("codex Shared agent %q is not fully resolved", binding.Name)
		}
		if len(child.MCPTools) != 0 || len(child.Shared) != 0 || len(child.Template.Spec.Tools) != 0 || len(child.Template.Spec.Skills) != 0 || len(child.Template.Spec.Plugins) != 0 {
			return nil, v2translator.NewValidationError("Codex Shared agent %q cannot contain tools, skills, plugins, or nested agents", binding.Name)
		}
		if !sameProviderConfiguration(root.ResolvedModelConfig.Config.Spec, child.ResolvedModelConfig.Config.Spec) {
			return nil, v2translator.NewValidationError("Codex Shared agent %q must use the root provider and authentication configuration", binding.Name)
		}
		if _, exists := agents[binding.Name]; exists {
			return nil, v2translator.NewValidationError("duplicate Codex Shared agent name %q", binding.Name)
		}
		agents[binding.Name] = codexconfig.Agent{Description: binding.Description, Instruction: child.Instruction, Model: child.ResolvedModelConfig.Config.Spec.Model}
	}
	return agents, nil
}

func sameProviderConfiguration(root, child v1alpha3.ModelConfigSpec) bool {
	root.Model, child.Model = "", ""
	return reflect.DeepEqual(root, child)
}

func (c *Compiler) requireSecretKey(ctx context.Context, namespace, name, key string) error {
	if name == "" || key == "" {
		return v2translator.NewValidationError("Codex OpenAI requires apiKeySecret and apiKeySecretKey")
	}
	secret, err := c.secret(ctx, namespace, name)
	if err != nil {
		return err
	}
	if len(secret.Data[key]) == 0 {
		return v2translator.NewValidationError("Codex credential Secret %q does not contain a non-empty key %q", name, key)
	}
	return nil
}

func (c *Compiler) secret(ctx context.Context, namespace, name string) (*corev1.Secret, error) {
	secret := krt.FetchOne(c.ctx, c.collections.Secrets, krt.FilterObjectName(types.NamespacedName{Namespace: namespace, Name: name}))
	if secret == nil {
		return nil, fmt.Errorf("read Codex credential Secret %q: not found", name)
	}
	return *secret, nil
}

func secretEnvironment(environmentName, secretName, key string) corev1.EnvVar {
	return corev1.EnvVar{Name: environmentName, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: secretName}, Key: key,
	}}}
}

func absoluteHTTPHostname(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", fmt.Errorf("must be an absolute HTTP(S) URL without credentials or fragment")
	}
	return parsed.Hostname(), nil
}

type provenanceEntry struct {
	APIVersion string    `json:"apiVersion"`
	Kind       string    `json:"kind"`
	Name       string    `json:"name"`
	Key        string    `json:"key,omitempty"`
	UID        types.UID `json:"uid"`
	Generation int64     `json:"generation,omitempty"`
	Hash       string    `json:"hash"`
}

func (c *Compiler) buildProvenance(ctx context.Context, input *v2translator.HarnessInput, environment []corev1.EnvVar, configJSON, cardJSON []byte) ([]byte, error) {
	entries := []provenanceEntry{objectProvenance(v1alpha3.GroupVersion.String(), "Harness", input.Harness.Name, input.Harness.UID, input.Harness.Generation, input.Harness.Spec)}
	entries = append(entries,
		objectProvenance("kagent.internal/v1", "GeneratedInput", "config.json", "", 0, json.RawMessage(configJSON)),
		objectProvenance("kagent.internal/v1", "GeneratedInput", "agent-card.json", "", 0, json.RawMessage(cardJSON)),
	)
	seenObjects := map[string]struct{}{}
	configMaps := map[string]struct{}{}
	var addAgent func(*v2translator.AgentInput)
	addAgent = func(agent *v2translator.AgentInput) {
		model := agent.ResolvedModelConfig.Config
		for _, object := range []struct {
			kind, name string
			uid        types.UID
			generation int64
			value      any
		}{
			{"AgentTemplate", agent.Template.Name, agent.Template.UID, agent.Template.Generation, agent.Template.Spec},
			{"ModelConfig", model.Name, model.UID, model.Generation, model.Spec},
		} {
			identity := object.kind + "\x00" + object.name
			if _, ok := seenObjects[identity]; !ok {
				seenObjects[identity] = struct{}{}
				entries = append(entries, objectProvenance(v1alpha3.GroupVersion.String(), object.kind, object.name, object.uid, object.generation, object.value))
			}
		}
		if agent.Template.Spec.SystemPromptFrom != nil {
			configMaps[agent.Template.Spec.SystemPromptFrom.Name] = struct{}{}
		}
		if agent.Template.Spec.PromptTemplate != nil {
			for _, source := range agent.Template.Spec.PromptTemplate.DataSources {
				configMaps[source.Name] = struct{}{}
			}
		}
		for _, tool := range agent.MCPTools {
			if tool.Server != nil {
				identity := "RemoteMCPServer\x00" + tool.Server.Name
				if _, ok := seenObjects[identity]; !ok {
					seenObjects[identity] = struct{}{}
					entries = append(entries, objectProvenance(v1alpha3.GroupVersion.String(), "RemoteMCPServer", tool.Server.Name, tool.Server.UID, tool.Server.Generation, tool.Server.Spec))
				}
				for _, header := range tool.Server.Spec.HeadersFrom {
					if header.ValueFrom != nil && header.ValueFrom.Type == v1alpha3.ConfigMapValueSource {
						configMaps[header.ValueFrom.Name] = struct{}{}
					}
				}
			}
		}
		for _, child := range agent.Shared {
			addAgent(child.Agent)
		}
	}
	addAgent(input.Root)
	for name := range configMaps {
		configMap := krt.FetchOne(c.ctx, c.collections.ConfigMaps, krt.FilterObjectName(types.NamespacedName{Namespace: input.Harness.Namespace, Name: name}))
		if configMap == nil {
			return nil, fmt.Errorf("ConfigMap %q not found", name)
		}
		entries = append(entries, objectProvenance("v1", "ConfigMap", name, (*configMap).UID, (*configMap).Generation, (*configMap).Data))
	}
	seenSecrets := map[string]struct{}{}
	for _, variable := range environment {
		if variable.ValueFrom == nil || variable.ValueFrom.SecretKeyRef == nil {
			continue
		}
		ref := variable.ValueFrom.SecretKeyRef
		identity := ref.Name + "\x00" + ref.Key
		if _, ok := seenSecrets[identity]; ok {
			continue
		}
		seenSecrets[identity] = struct{}{}
		secret, err := c.secret(ctx, input.Harness.Namespace, ref.Name)
		if err != nil {
			return nil, err
		}
		value, ok := secret.Data[ref.Key]
		if !ok {
			return nil, fmt.Errorf("secret %q does not contain key %q", ref.Name, ref.Key)
		}
		hash := sha256.Sum256(value)
		entries = append(entries, provenanceEntry{APIVersion: "v1", Kind: "Secret", Name: ref.Name, Key: ref.Key, UID: secret.UID, Hash: fmt.Sprintf("%x", hash[:])})
	}
	slices.SortFunc(entries, func(a, b provenanceEntry) int {
		return strings.Compare(a.APIVersion+"\x00"+a.Kind+"\x00"+a.Name+"\x00"+a.Key, b.APIVersion+"\x00"+b.Kind+"\x00"+b.Name+"\x00"+b.Key)
	})
	return json.Marshal(entries)
}

func objectProvenance(apiVersion, kind, name string, uid types.UID, generation int64, content any) provenanceEntry {
	raw, _ := json.Marshal(content)
	hash := sha256.Sum256(raw)
	return provenanceEntry{APIVersion: apiVersion, Kind: kind, Name: name, UID: uid, Generation: generation, Hash: fmt.Sprintf("%x", hash[:])}
}

func (c *Compiler) resolveEnvironment(ctx context.Context, namespace string, environment []corev1.EnvVar) ([]corev1.EnvVar, error) {
	resolved := append([]corev1.EnvVar(nil), environment...)
	for i, variable := range resolved {
		if variable.ValueFrom == nil {
			continue
		}
		if variable.ValueFrom.SecretKeyRef == nil {
			return nil, fmt.Errorf("environment variable %q uses an unsupported value source", variable.Name)
		}
		ref := variable.ValueFrom.SecretKeyRef
		secret, err := c.secret(ctx, namespace, ref.Name)
		if err != nil {
			return nil, err
		}
		value, ok := secret.Data[ref.Key]
		if !ok {
			return nil, fmt.Errorf("secret %q does not contain key %q", ref.Name, ref.Key)
		}
		resolved[i].Value, resolved[i].ValueFrom = string(value), nil
	}
	return resolved, nil
}

func agentTemplateCard(template *v1alpha3.AgentTemplate) *a2atype.AgentCard {
	return &a2atype.AgentCard{
		Name: strings.ReplaceAll(template.Name, "-", "_"), Description: template.Spec.Description, Version: "v1",
		SupportedInterfaces: []*a2atype.AgentInterface{{URL: "http://127.0.0.1:80", ProtocolBinding: a2atype.TransportProtocolGRPC, ProtocolVersion: a2atype.Version}},
		Capabilities:        a2atype.AgentCapabilities{Streaming: true}, Skills: []a2atype.AgentSkill{},
		DefaultInputModes: []string{"text"}, DefaultOutputModes: []string{"text"},
	}
}

var _ v2translator.HarnessCompiler = (*Compiler)(nil)
