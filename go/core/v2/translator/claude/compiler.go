// Package claude compiles resolved v1alpha3 inputs for the native Claude
// Harness adapter.
package claude

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
	v2translator "github.com/kagent-dev/kagent/go/core/v2/translator"
	claudeconfig "github.com/kagent-dev/kagent/go/harness/claude/config"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	useBedrockEnv       = "CLAUDE_CODE_USE_BEDROCK"
	useVertexEnv        = "CLAUDE_CODE_USE_VERTEX"
	awsRegionEnv        = "AWS_REGION"
	awsAccessKeyEnv     = "AWS_ACCESS_KEY_ID"
	awsSecretKeyEnv     = "AWS_SECRET_ACCESS_KEY"
	awsSessionTokenEnv  = "AWS_SESSION_TOKEN"
	awsBedrockTokenEnv  = "AWS_BEARER_TOKEN_BEDROCK"
	anthropicAPIKeyEnv  = "ANTHROPIC_API_KEY"
	anthropicBaseURLEnv = "ANTHROPIC_BASE_URL"
	vertexProjectEnv    = "ANTHROPIC_VERTEX_PROJECT_ID"
	vertexRegionEnv     = "CLOUD_ML_REGION"
	sandboxEnv          = "IS_SANDBOX"
	preResponseFlushEnv = "KAGENT_PRE_RESPONSE_TRACE_FLUSH"
)

var ownedEnvironment = map[string]struct{}{
	useBedrockEnv: {}, useVertexEnv: {}, awsRegionEnv: {}, awsAccessKeyEnv: {},
	awsSecretKeyEnv: {}, awsSessionTokenEnv: {}, awsBedrockTokenEnv: {},
	anthropicAPIKeyEnv: {}, anthropicBaseURLEnv: {}, vertexProjectEnv: {}, vertexRegionEnv: {},
	sandboxEnv: {}, claudeconfig.GoogleCredentialsJSONEnvName: {},
}

type Compiler struct {
	ctx         krt.HandlerContext
	collections v2translator.Collections
}

func NewCompiler(ctx krt.HandlerContext, collections v2translator.Collections) *Compiler {
	return &Compiler{ctx: ctx, collections: collections}
}

func (c *Compiler) Compile(ctx context.Context, input *v2translator.HarnessInput) (*v2translator.Revision, error) {
	if input == nil || input.Harness == nil || input.Root == nil || input.Root.Template == nil || input.Root.ResolvedModelConfig == nil || input.Root.ResolvedModelConfig.Config == nil {
		return nil, fmt.Errorf("claude compiler requires a resolved Harness, AgentTemplate, and ModelConfig")
	}
	model := input.Root.ResolvedModelConfig.Config
	if strings.TrimSpace(model.Spec.Model) == "" {
		return nil, v2translator.NewValidationError("Claude ModelConfig model is required")
	}
	if len(model.Spec.DefaultHeaders) != 0 || !model.Spec.TLS.IsEmpty() || model.Spec.APIKeyPassthrough {
		return nil, v2translator.NewValidationError("Claude does not support ModelConfig defaultHeaders, TLS, or apiKeyPassthrough yet")
	}

	providerEnvironment, egress, err := c.provider(ctx, model)
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
	environment := append([]corev1.EnvVar(nil), providerEnvironment...)
	environment = append(environment, mcp.environment...)
	for _, variable := range input.Harness.Spec.Env {
		if _, reserved := ownedEnvironment[variable.Name]; reserved {
			return nil, v2translator.NewValidationError("Harness env %q conflicts with Claude's compiled provider configuration", variable.Name)
		}
		if strings.HasPrefix(variable.Name, mcpCredentialPrefix) {
			return nil, v2translator.NewValidationError("Harness env %q conflicts with Claude's compiled MCP credentials", variable.Name)
		}
		envVar := corev1.EnvVar{Name: variable.Name}
		if variable.Value != nil {
			envVar.Value = *variable.Value
		} else {
			envVar.ValueFrom = &corev1.EnvVarSource{SecretKeyRef: variable.CredentialRef.DeepCopy()}
		}
		environment = append(environment, envVar)
	}
	// Substrate v0.0.20 runs Actor processes as root even when the image declares
	// a non-root USER. Claude otherwise rejects --dangerously-skip-permissions.
	environment = append(environment,
		corev1.EnvVar{Name: sandboxEnv, Value: "1"},
		corev1.EnvVar{Name: preResponseFlushEnv, Value: "true"},
	)

	localAgents, err := c.compileLocalAgents(input.Root)
	if err != nil {
		return nil, err
	}
	config := claudeconfig.Production(model.Spec.Model, input.Root.Instruction)
	config.Agents = localAgents
	if len(skillResources.Skills) != 0 || len(skillResources.Plugins) != 0 {
		config.SkillResources = &skillResources
	}
	config.MCPServers = mcp.servers
	if err := config.Validate(); err != nil {
		return nil, v2translator.NewValidationError("invalid compiled Claude configuration: %v", err)
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal Claude config: %w", err)
	}
	cardJSON, err := json.Marshal(agentTemplateCard(input.Root.Template))
	if err != nil {
		return nil, fmt.Errorf("marshal Claude agent card: %w", err)
	}
	provenance, err := c.buildProvenance(ctx, input, environment)
	if err != nil {
		return nil, fmt.Errorf("build Claude revision provenance: %w", err)
	}
	environment, err = c.resolveEnvironment(ctx, input.Harness.Namespace, environment)
	if err != nil {
		return nil, fmt.Errorf("resolve Claude runtime environment: %w", err)
	}

	egress = append(egress, skillEgress...)
	egress = append(egress, mcp.egress...)
	slices.Sort(egress)
	egress = slices.Compact(egress)
	template, harness := input.Root.Template, input.Harness
	return &v2translator.Revision{
		Namespace: template.Namespace, AgentTemplateName: template.Name, HarnessName: harness.Name,
		Image: harness.Spec.Workload.Image, Environment: environment,
		ConfigJSON: configJSON, AgentCardJSON: cardJSON,
		WorkerPoolName:   harness.Spec.Substrate.WorkerPoolRef.Name,
		SnapshotLocation: harness.Spec.Substrate.SnapshotPolicy.Location,
		Provenance:       provenance, EgressDestinations: egress, Warnings: mcp.warnings,
	}, nil
}

func (c *Compiler) compileLocalAgents(root *v2translator.AgentInput) (map[string]claudeconfig.Agent, error) {
	if len(root.Shared) == 0 {
		return nil, nil
	}
	agents := make(map[string]claudeconfig.Agent, len(root.Shared))
	for _, binding := range root.Shared {
		child := binding.Agent
		if child == nil || child.Template == nil || child.ResolvedModelConfig == nil || child.ResolvedModelConfig.Config == nil {
			return nil, fmt.Errorf("claude local agent %q is not fully resolved", binding.Name)
		}
		childModel := child.ResolvedModelConfig.Config
		if len(child.MCPTools) != 0 || len(child.Shared) != 0 || len(child.Template.Spec.Tools) != 0 {
			return nil, v2translator.NewValidationError("Claude local agent %q cannot contain MCP or nested agent tools yet", binding.Name)
		}
		if len(child.Template.Spec.Skills) != 0 || len(child.Template.Spec.Plugins) != 0 {
			return nil, v2translator.NewValidationError("Claude local agent %q cannot contain skills or plugins yet", binding.Name)
		}
		if strings.TrimSpace(childModel.Spec.Model) == "" {
			return nil, v2translator.NewValidationError("Claude local agent %q ModelConfig model is required", binding.Name)
		}
		if !sameProviderConfiguration(root.ResolvedModelConfig.Config.Spec, childModel.Spec) {
			return nil, v2translator.NewValidationError("Claude local agent %q must use the root agent's provider and authentication configuration", binding.Name)
		}
		if _, exists := agents[binding.Name]; exists {
			return nil, v2translator.NewValidationError("duplicate Claude local agent name %q", binding.Name)
		}
		agents[binding.Name] = claudeconfig.Agent{
			Description: binding.Description,
			Prompt:      child.Instruction,
			Model:       childModel.Spec.Model,
		}
	}
	return agents, nil
}

func sameProviderConfiguration(root, child v1alpha3.ModelConfigSpec) bool {
	root.Model, child.Model = "", ""
	return reflect.DeepEqual(root, child)
}

func (c *Compiler) provider(ctx context.Context, model *v1alpha3.ModelConfig) ([]corev1.EnvVar, []string, error) {
	switch model.Spec.Provider {
	case v1alpha3.ModelProviderAnthropic:
		var baseURL string
		if model.Spec.Anthropic != nil {
			options := *model.Spec.Anthropic
			baseURL = strings.TrimSpace(options.BaseURL)
			options.BaseURL = ""
			if !reflect.DeepEqual(options, v1alpha3.AnthropicConfig{}) {
				return nil, nil, v2translator.NewValidationError("Claude does not support Anthropic provider options beyond baseUrl yet")
			}
		}
		if err := c.requireSecretKey(ctx, model, model.Spec.APIKeySecret, model.Spec.APIKeySecretKey, false); err != nil {
			return nil, nil, err
		}
		environment := []corev1.EnvVar{secretEnvironment(anthropicAPIKeyEnv, model.Spec.APIKeySecret, model.Spec.APIKeySecretKey)}
		egress := []string{"api.anthropic.com"}
		if baseURL != "" {
			hostname, err := anthropicBaseURLHostname(baseURL)
			if err != nil {
				return nil, nil, err
			}
			environment = append(environment, corev1.EnvVar{Name: anthropicBaseURLEnv, Value: baseURL})
			egress = []string{hostname}
		}
		return environment, egress, nil

	case v1alpha3.ModelProviderBedrock:
		if model.Spec.Bedrock == nil || strings.TrimSpace(model.Spec.Bedrock.Region) == "" {
			return nil, nil, v2translator.NewValidationError("Claude Bedrock requires bedrock.region")
		}
		options := *model.Spec.Bedrock
		options.Region = ""
		// "5m" is the CRD default and matches Claude Code's native cache TTL.
		if options.CacheTTL == "5m" {
			options.CacheTTL = ""
		}
		if !reflect.DeepEqual(options, v1alpha3.BedrockConfig{}) {
			return nil, nil, v2translator.NewValidationError("Claude does not support Bedrock provider options beyond region yet")
		}
		if model.Spec.APIKeySecret == "" {
			return nil, nil, v2translator.NewValidationError("Claude Bedrock requires apiKeySecret with AWS credentials")
		}
		if model.Spec.APIKeySecretKey != "" {
			return nil, nil, v2translator.NewValidationError("Claude Bedrock reads standard AWS keys from apiKeySecret; apiKeySecretKey must be empty")
		}
		secret, err := c.secret(ctx, model.Namespace, model.Spec.APIKeySecret)
		if err != nil {
			return nil, nil, err
		}
		environment := []corev1.EnvVar{{Name: useBedrockEnv, Value: "1"}, {Name: awsRegionEnv, Value: model.Spec.Bedrock.Region}}
		if value := secret.Data[awsBedrockTokenEnv]; len(value) != 0 {
			environment = append(environment, secretEnvironment(awsBedrockTokenEnv, secret.Name, awsBedrockTokenEnv))
		} else {
			for _, key := range []string{awsAccessKeyEnv, awsSecretKeyEnv} {
				if len(secret.Data[key]) == 0 {
					return nil, nil, v2translator.NewValidationError("Claude Bedrock Secret %q requires %s and %s, or %s", secret.Name, awsAccessKeyEnv, awsSecretKeyEnv, awsBedrockTokenEnv)
				}
				environment = append(environment, secretEnvironment(key, secret.Name, key))
			}
			if len(secret.Data[awsSessionTokenEnv]) != 0 {
				environment = append(environment, secretEnvironment(awsSessionTokenEnv, secret.Name, awsSessionTokenEnv))
			}
		}
		return environment, []string{"bedrock-runtime." + model.Spec.Bedrock.Region + ".amazonaws.com"}, nil

	case v1alpha3.ModelProviderAnthropicVertexAI:
		if model.Spec.AnthropicVertexAI == nil || strings.TrimSpace(model.Spec.AnthropicVertexAI.ProjectID) == "" || strings.TrimSpace(model.Spec.AnthropicVertexAI.Location) == "" {
			return nil, nil, v2translator.NewValidationError("Claude Vertex requires anthropicVertexAI.projectID and location")
		}
		options := *model.Spec.AnthropicVertexAI
		options.ProjectID, options.Location = "", ""
		if !reflect.DeepEqual(options, v1alpha3.AnthropicVertexAIConfig{}) {
			return nil, nil, v2translator.NewValidationError("Claude does not support AnthropicVertexAI provider options beyond projectID and location yet")
		}
		if err := c.requireGoogleCredentials(ctx, model); err != nil {
			return nil, nil, err
		}
		cfg := model.Spec.AnthropicVertexAI
		return []corev1.EnvVar{
			{Name: useVertexEnv, Value: "1"}, {Name: vertexProjectEnv, Value: cfg.ProjectID}, {Name: vertexRegionEnv, Value: cfg.Location},
			secretEnvironment(claudeconfig.GoogleCredentialsJSONEnvName, model.Spec.APIKeySecret, model.Spec.APIKeySecretKey),
		}, []string{vertexHostname(cfg.Location), "oauth2.googleapis.com"}, nil
	default:
		return nil, nil, v2translator.NewValidationError("Claude does not support ModelConfig provider %q", model.Spec.Provider)
	}
}

func anthropicBaseURLHostname(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", v2translator.NewValidationError("Claude Anthropic baseUrl must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	return parsed.Hostname(), nil
}

func (c *Compiler) requireSecretKey(ctx context.Context, model *v1alpha3.ModelConfig, name, key string, requireJSON bool) error {
	if name == "" || key == "" {
		return v2translator.NewValidationError("Claude %s requires apiKeySecret and apiKeySecretKey", model.Spec.Provider)
	}
	secret, err := c.secret(ctx, model.Namespace, name)
	if err != nil {
		return err
	}
	value, ok := secret.Data[key]
	if !ok || len(value) == 0 {
		return v2translator.NewValidationError("Claude credential Secret %q does not contain a non-empty key %q", name, key)
	}
	if requireJSON && !json.Valid(value) {
		return v2translator.NewValidationError("Claude Vertex credential Secret %q key %q must contain valid JSON", name, key)
	}
	return nil
}

func (c *Compiler) requireGoogleCredentials(ctx context.Context, model *v1alpha3.ModelConfig) error {
	if err := c.requireSecretKey(ctx, model, model.Spec.APIKeySecret, model.Spec.APIKeySecretKey, true); err != nil {
		return err
	}
	secret, err := c.secret(ctx, model.Namespace, model.Spec.APIKeySecret)
	if err != nil {
		return err
	}
	var credentials struct {
		Type      string `json:"type"`
		ProjectID string `json:"project_id"`
		TokenURI  string `json:"token_uri"`
	}
	if err := json.Unmarshal(secret.Data[model.Spec.APIKeySecretKey], &credentials); err != nil {
		return v2translator.NewValidationError("decode Claude Vertex credentials: %v", err)
	}
	if credentials.Type != "service_account" {
		return v2translator.NewValidationError("Claude Vertex credentials must be a service_account key in the first release")
	}
	if credentials.ProjectID != model.Spec.AnthropicVertexAI.ProjectID {
		return v2translator.NewValidationError("Claude Vertex credential project_id must match anthropicVertexAI.projectID")
	}
	if credentials.TokenURI != "" {
		parsed, err := url.Parse(credentials.TokenURI)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "oauth2.googleapis.com" {
			return v2translator.NewValidationError("Claude Vertex credential token_uri must use https://oauth2.googleapis.com")
		}
	}
	return nil
}

func (c *Compiler) secret(ctx context.Context, namespace, name string) (*corev1.Secret, error) {
	secret := krt.FetchOne(c.ctx, c.collections.Secrets, krt.FilterObjectName(types.NamespacedName{Namespace: namespace, Name: name}))
	if secret == nil {
		return nil, fmt.Errorf("read Claude credential Secret %q: not found", name)
	}
	return *secret, nil
}

func secretEnvironment(environmentName, secretName, key string) corev1.EnvVar {
	return corev1.EnvVar{Name: environmentName, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: secretName}, Key: key,
	}}}
}

func vertexHostname(location string) string {
	switch location {
	case "global":
		return "aiplatform.googleapis.com"
	case "us", "eu":
		return "aiplatform." + location + ".rep.googleapis.com"
	default:
		return location + "-aiplatform.googleapis.com"
	}
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

func (c *Compiler) buildProvenance(ctx context.Context, input *v2translator.HarnessInput, environment []corev1.EnvVar) ([]byte, error) {
	harness := input.Harness
	entries := []provenanceEntry{objectProvenance(v1alpha3.GroupVersion.String(), "Harness", harness.Name, harness.UID, harness.Generation, harness.Spec)}
	configMaps := map[string]struct{}{}
	objects := map[string]struct{}{}
	addObject := func(kind, name string, uid types.UID, generation int64, content any) {
		identity := kind + "\x00" + name
		if _, exists := objects[identity]; exists {
			return
		}
		objects[identity] = struct{}{}
		entries = append(entries, objectProvenance(v1alpha3.GroupVersion.String(), kind, name, uid, generation, content))
	}
	var addAgent func(*v2translator.AgentInput)
	addAgent = func(agent *v2translator.AgentInput) {
		template, model := agent.Template, agent.ResolvedModelConfig.Config
		addObject("AgentTemplate", template.Name, template.UID, template.Generation, template.Spec)
		addObject("ModelConfig", model.Name, model.UID, model.Generation, model.Spec)
		if template.Spec.SystemPromptFrom != nil {
			configMaps[template.Spec.SystemPromptFrom.Name] = struct{}{}
		}
		if template.Spec.PromptTemplate != nil {
			for _, source := range template.Spec.PromptTemplate.DataSources {
				configMaps[source.Name] = struct{}{}
			}
		}
		for _, child := range agent.Shared {
			addAgent(child.Agent)
		}
		for _, tool := range agent.MCPTools {
			server := tool.Server
			if server != nil {
				addObject("RemoteMCPServer", server.Name, server.UID, server.Generation, server.Spec)
			}
		}
	}
	addAgent(input.Root)
	for name := range configMaps {
		configMap := krt.FetchOne(c.ctx, c.collections.ConfigMaps, krt.FilterObjectName(types.NamespacedName{Namespace: harness.Namespace, Name: name}))
		if configMap == nil {
			return nil, fmt.Errorf("ConfigMap %q not found", name)
		}
		entries = append(entries, objectProvenance("v1", "ConfigMap", name, (*configMap).UID, (*configMap).Generation, (*configMap).Data))
	}
	seen := map[string]struct{}{}
	for _, variable := range environment {
		if variable.ValueFrom == nil || variable.ValueFrom.SecretKeyRef == nil {
			continue
		}
		ref := variable.ValueFrom.SecretKeyRef
		identity := ref.Name + "\x00" + ref.Key
		if _, ok := seen[identity]; ok {
			continue
		}
		seen[identity] = struct{}{}
		secret, err := c.secret(ctx, harness.Namespace, ref.Name)
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
