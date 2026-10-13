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
	"strconv"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	claudeconfig "github.com/kagent-dev/kagent/go/harness/claude/config"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

type Compiler struct {
	ctx         krt.HandlerContext
	collections translator.Collections
}

func NewCompiler(ctx krt.HandlerContext, collections translator.Collections) *Compiler {
	return &Compiler{ctx: ctx, collections: collections}
}

func (c *Compiler) Compile(ctx context.Context, input *translator.HarnessInput) (*translator.CompileResult, error) {
	if input == nil || input.Harness == nil || input.Root == nil || input.Root.Template == nil || input.Root.ResolvedModelConfig == nil || input.Root.ResolvedModelConfig.Config == nil {
		return nil, fmt.Errorf("claude compiler requires a resolved Harness, AgentTemplate, and ModelConfig")
	}
	model := input.Root.ResolvedModelConfig.Config
	if strings.TrimSpace(model.Spec.Model) == "" {
		return nil, translator.NewValidationError("Claude ModelConfig model is required")
	}
	if len(model.Spec.DefaultHeaders) != 0 || !model.Spec.TLS.IsEmpty() || model.Spec.APIKeyPassthrough {
		return nil, translator.NewValidationError("Claude does not support ModelConfig defaultHeaders, TLS, or apiKeyPassthrough yet")
	}
	telemetryConfig, _ := translator.TelemetryConfigFromProcess()
	logConfig := telemetryConfig.Logs
	template, harness := input.Root.Template, input.Harness
	// The runtime reports this identity on every invocation span and on its
	// resource, so a user-supplied resource marker is never required.
	runtimeTelemetry := telemetryConfig.RuntimeTelemetry(
		tracing.RuntimeClaude, input.AgentName, template.Namespace, model.Spec)

	providerEnvironment, egress, err := c.provider(ctx, model)
	if err != nil {
		return nil, err
	}
	skills, err := translator.CompileSkillResources(input.Root.Template)
	if err != nil {
		return nil, err
	}
	mcp, err := c.compileMCP(ctx, input.Root.Template.Namespace, input.Root.MCPTools)
	if err != nil {
		return nil, err
	}
	environment := append([]corev1.EnvVar(nil), providerEnvironment...)
	environment = append(environment, mcp.environment...)
	environment = append(environment, skills.Environment...)
	harnessAttributes := translator.HarnessResourceAttributes(input.Harness)
	for _, variable := range input.Harness.Spec.Env {
		if translator.IsResourceAttributesVariable(variable.Name) {
			continue
		}
		if variable.Name == env.KagentAPIURL.Name() || claudeconfig.OwnsEnvironment(variable.Name) || translator.OwnsTelemetryEnvironment(variable.Name) {
			return nil, translator.NewValidationError("Harness env %q conflicts with Claude-owned runtime configuration", variable.Name)
		}
		environment = append(environment, corev1.EnvVar{Name: variable.Name, Value: variable.Value})
	}
	// Claude sends these on every model call: the identity the Go ADK runtime
	// sends from KAGENT_AGENT_TEMPLATE and KAGENT_NAMESPACE. The Claude harness
	// knows no user per turn, so it names the agent only.
	modelHeaders := map[string]string{adk.AgentHeader: input.Root.Template.Name, adk.AgentNamespaceHeader: input.Root.Template.Namespace}
	if model.Spec.Provider == v1alpha3.ModelProviderAnthropicVertexAI {
		// Under CLAUDE_CODE_SKIP_VERTEX_AUTH Claude Code sends no Authorization,
		// and the gateway replaces the header a request carries and adds none.
		modelHeaders["Authorization"] = "Bearer " + translator.CredentialPlaceholder
	}
	// Substrate v0.0.20 runs Actor processes as root even when the image declares
	// a non-root USER. Claude otherwise rejects --dangerously-skip-permissions.
	environment = append(environment,
		corev1.EnvVar{Name: claudeconfig.SandboxEnvName, Value: "1"},
		corev1.EnvVar{Name: env.KagentName.Name(), Value: input.AgentName},
		corev1.EnvVar{Name: env.KagentNamespace.Name(), Value: template.Namespace},
		corev1.EnvVar{Name: env.KagentAPIURL.Name(), Value: fmt.Sprintf("http://%s.%s:8083", utils.GetControllerName(), utils.GetResourceNamespace())},
		corev1.EnvVar{Name: claudeconfig.AnthropicCustomHeadersEnvName, Value: claudeconfig.CustomHeaders(modelHeaders)},
	)
	environment = append(environment, telemetryConfig.TelemetryEnvironment(runtimeTelemetry, harnessAttributes)...)
	// The adapter derives Claude Code's own telemetry flags; raw bodies have no
	// compiled field yet.
	if telemetryConfig.CaptureRawAPIBodies && logConfig.Enabled {
		environment = append(environment, corev1.EnvVar{Name: "OTEL_LOG_RAW_API_BODIES", Value: "1"})
	}

	localAgents, err := c.compileLocalAgents(input.Root)
	if err != nil {
		return nil, err
	}
	config := claudeconfig.Production(model.Spec.Model, input.Root.Instruction)
	config.Agents = localAgents
	config.RuntimeTelemetry = runtimeTelemetry
	if len(skills.Resources.Skills) != 0 || len(skills.Resources.Plugins) != 0 {
		config.SkillResources = &skills.Resources
	}
	config.MCPServers = mcp.servers
	if err := config.Validate(); err != nil {
		return nil, translator.NewValidationError("invalid compiled Claude configuration: %v", err)
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal Claude config: %w", err)
	}
	card, err := pbconv.ToProtoAgentCard(translator.ManagedAgentCard(input.AgentName, input.Root.Template))
	if err != nil {
		return nil, fmt.Errorf("convert Claude agent card: %w", err)
	}
	provenance, err := c.buildProvenance(ctx, input, environment)
	if err != nil {
		return nil, fmt.Errorf("build Claude revision provenance: %w", err)
	}
	environment, credentials, err := translator.CompileCredentials(input, nil, environment)
	if err != nil {
		return nil, err
	}

	egress = append(egress, skills.Egress...)
	egress = append(egress, mcp.egress...)
	egress = append(egress, telemetryConfig.Destinations()...)
	egress = append(egress, "http://"+utils.GetControllerName()+"."+utils.GetResourceNamespace()+":8083")
	slices.Sort(egress)
	egress = slices.Compact(egress)
	return &translator.CompileResult{
		Revision: translator.Revision{
			Namespace: template.Namespace,
			Image:     harness.Spec.Workload.Image, Environment: environment,
			ConfigJSON: configJSON, AgentCard: card,
			WorkerPoolName:   harness.Spec.Substrate.WorkerPoolRef.Name,
			SnapshotLocation: harness.Spec.Substrate.SnapshotPolicy.Location,
			Credentials:      credentials, Provenance: provenance, EgressDestinations: egress,
			Capabilities: unprivilegedClaudeCapabilities,
		},
		Warnings: mcp.warnings,
	}, nil
}

// unprivilegedClaudeCapabilities let the harness, which Substrate starts as
// root, hand the workspace and Claude's state to the image's unprivileged user
// and start Claude Code as that user, so Claude and its tools cannot read the
// harness process that holds the turn's credential.
var unprivilegedClaudeCapabilities = []string{"CHOWN", "SETGID", "SETUID"}

func (c *Compiler) compileLocalAgents(root *translator.AgentInput) (map[string]claudeconfig.Agent, error) {
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
			return nil, translator.NewValidationError("Claude local agent %q cannot contain MCP or nested agent tools yet", binding.Name)
		}
		if len(child.Template.Spec.Skills) != 0 || len(child.Template.Spec.Plugins) != 0 {
			return nil, translator.NewValidationError("Claude local agent %q cannot contain skills or plugins yet", binding.Name)
		}
		if strings.TrimSpace(childModel.Spec.Model) == "" {
			return nil, translator.NewValidationError("Claude local agent %q ModelConfig model is required", binding.Name)
		}
		if !sameProviderConfiguration(root.ResolvedModelConfig.Config.Spec, childModel.Spec) {
			return nil, translator.NewValidationError("Claude local agent %q must use the root agent's provider and authentication configuration", binding.Name)
		}
		if _, exists := agents[binding.Name]; exists {
			return nil, translator.NewValidationError("duplicate Claude local agent name %q", binding.Name)
		}
		agents[binding.Name] = claudeconfig.Agent{
			Description: binding.Description,
			Prompt:      child.Instruction,
			Model:       childModel.Spec.Model,
		}
	}
	return agents, nil
}

// maxOutputTokens caps the output of every model request Claude Code makes, as
// maxTokens caps a Go ADK model call. Claude Code lowers a value above the
// model's own cap to that cap.
func maxOutputTokens(tokens int) []corev1.EnvVar {
	if tokens <= 0 {
		return nil
	}
	return []corev1.EnvVar{{Name: claudeconfig.MaxOutputTokensEnvName, Value: strconv.Itoa(tokens)}}
}

func sameProviderConfiguration(root, child v1alpha3.ModelConfigSpec) bool {
	root.Model, child.Model = "", ""
	return reflect.DeepEqual(root, child)
}

func (c *Compiler) provider(ctx context.Context, model *v1alpha3.ModelConfig) ([]corev1.EnvVar, []string, error) {
	switch model.Spec.Provider {
	case v1alpha3.ModelProviderAnthropic:
		var baseURL string
		var maxTokens int
		if model.Spec.Anthropic != nil {
			options := *model.Spec.Anthropic
			baseURL = strings.TrimSpace(options.BaseURL)
			options.BaseURL = ""
			maxTokens = options.MaxTokens
			options.MaxTokens = 0
			// "5m" is the CRD default and matches Claude Code's native cache TTL.
			if options.CacheTTL == "5m" {
				options.CacheTTL = ""
			}
			// Claude Code caches prompts on its own, so promptCaching asks for
			// what already happens; a ModelConfig shared with the Go ADK runtime
			// carries it. Nothing is passed on.
			options.PromptCaching = false
			if !reflect.DeepEqual(options, v1alpha3.AnthropicConfig{}) {
				return nil, nil, translator.NewValidationError("Claude does not support Anthropic provider options beyond baseUrl, maxTokens, promptCaching and a 5m cacheTTL yet")
			}
		}
		if err := c.requireSecretKey(ctx, model, model.Spec.APIKeySecret, model.Spec.APIKeySecretKey, false); err != nil {
			return nil, nil, err
		}
		environment := []corev1.EnvVar{secretEnvironment(claudeconfig.AnthropicAPIKeyEnvName, model.Spec.APIKeySecret, model.Spec.APIKeySecretKey)}
		egress := []string{"https://api.anthropic.com:443"}
		if baseURL != "" {
			hostname, err := anthropicBaseURLOrigin(baseURL)
			if err != nil {
				return nil, nil, err
			}
			environment = append(environment, corev1.EnvVar{Name: claudeconfig.AnthropicBaseURLEnvName, Value: baseURL})
			egress = []string{hostname}
		}
		return append(environment, maxOutputTokens(maxTokens)...), egress, nil

	case v1alpha3.ModelProviderBedrock:
		if model.Spec.Bedrock == nil || strings.TrimSpace(model.Spec.Bedrock.Region) == "" {
			return nil, nil, translator.NewValidationError("Claude Bedrock requires bedrock.region")
		}
		options := *model.Spec.Bedrock
		options.Region = ""
		// "5m" is the CRD default and matches Claude Code's native cache TTL.
		if options.CacheTTL == "5m" {
			options.CacheTTL = ""
		}
		// Claude Code caches prompts on its own, as for Anthropic.
		options.PromptCaching = false
		if !reflect.DeepEqual(options, v1alpha3.BedrockConfig{}) {
			return nil, nil, translator.NewValidationError("Claude does not support Bedrock provider options beyond region, promptCaching and a 5m cacheTTL yet")
		}
		if model.Spec.APIKeySecret == "" {
			return nil, nil, translator.NewValidationError("Claude Bedrock requires apiKeySecret with AWS credentials")
		}
		if model.Spec.APIKeySecretKey != "" {
			return nil, nil, translator.NewValidationError("Claude Bedrock reads standard AWS keys from apiKeySecret; apiKeySecretKey must be empty")
		}
		secret, err := c.secret(ctx, model.Namespace, model.Spec.APIKeySecret)
		if err != nil {
			return nil, nil, err
		}
		environment := []corev1.EnvVar{{Name: claudeconfig.UseBedrockEnvName, Value: "1"}, {Name: claudeconfig.AWSRegionEnvName, Value: model.Spec.Bedrock.Region}}
		if value := secret.Data[claudeconfig.AWSBedrockTokenEnvName]; len(value) != 0 {
			environment = append(environment, secretEnvironment(claudeconfig.AWSBedrockTokenEnvName, secret.Name, claudeconfig.AWSBedrockTokenEnvName))
		} else {
			for _, key := range []string{claudeconfig.AWSAccessKeyEnvName, claudeconfig.AWSSecretKeyEnvName} {
				if len(secret.Data[key]) == 0 {
					return nil, nil, translator.NewValidationError("Claude Bedrock Secret %q requires %s and %s, or %s", secret.Name, claudeconfig.AWSAccessKeyEnvName, claudeconfig.AWSSecretKeyEnvName, claudeconfig.AWSBedrockTokenEnvName)
				}
				environment = append(environment, secretEnvironment(key, secret.Name, key))
			}
			if len(secret.Data[claudeconfig.AWSSessionTokenEnvName]) != 0 {
				environment = append(environment, secretEnvironment(claudeconfig.AWSSessionTokenEnvName, secret.Name, claudeconfig.AWSSessionTokenEnvName))
			}
		}
		return environment, []string{"https://bedrock-runtime." + model.Spec.Bedrock.Region + ".amazonaws.com:443"}, nil

	case v1alpha3.ModelProviderAnthropicVertexAI:
		if model.Spec.AnthropicVertexAI == nil || strings.TrimSpace(model.Spec.AnthropicVertexAI.ProjectID) == "" || strings.TrimSpace(model.Spec.AnthropicVertexAI.Location) == "" {
			return nil, nil, translator.NewValidationError("Claude Vertex requires anthropicVertexAI.projectID and location")
		}
		options := *model.Spec.AnthropicVertexAI
		options.ProjectID, options.Location, options.MaxTokens = "", "", 0
		if !reflect.DeepEqual(options, v1alpha3.AnthropicVertexAIConfig{}) {
			return nil, nil, translator.NewValidationError("Claude does not support AnthropicVertexAI provider options beyond projectID, location and maxTokens yet")
		}
		if err := c.requireGoogleCredentials(ctx, model); err != nil {
			return nil, nil, err
		}
		cfg := model.Spec.AnthropicVertexAI
		// The gateway completes the request with an access token Substrate mints
		// from the key, so Claude Code skips its own Google authentication and
		// the key never enters the runtime.
		return append([]corev1.EnvVar{
			{Name: claudeconfig.UseVertexEnvName, Value: "1"}, {Name: claudeconfig.SkipVertexAuthEnvName, Value: "1"},
			{Name: claudeconfig.VertexProjectEnvName, Value: cfg.ProjectID}, {Name: claudeconfig.VertexRegionEnvName, Value: cfg.Location},
		}, maxOutputTokens(cfg.MaxTokens)...), []string{translator.VertexAIOrigin(cfg.Location)}, nil
	default:
		return nil, nil, translator.NewValidationError("Claude does not support ModelConfig provider %q", model.Spec.Provider)
	}
}

func anthropicBaseURLOrigin(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", translator.NewValidationError("Claude Anthropic baseUrl must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	return egress.Origin(parsed), nil
}

func (c *Compiler) requireSecretKey(ctx context.Context, model *v1alpha3.ModelConfig, name, key string, requireJSON bool) error {
	if name == "" || key == "" {
		return translator.NewValidationError("Claude %s requires apiKeySecret and apiKeySecretKey", model.Spec.Provider)
	}
	secret, err := c.secret(ctx, model.Namespace, name)
	if err != nil {
		return err
	}
	value, ok := secret.Data[key]
	if !ok || len(value) == 0 {
		return translator.NewValidationError("Claude credential Secret %q does not contain a non-empty key %q", name, key)
	}
	if requireJSON && !json.Valid(value) {
		return translator.NewValidationError("Claude Vertex credential Secret %q key %q must contain valid JSON", name, key)
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
	return translator.ValidateGoogleServiceAccountKey(secret.Data[model.Spec.APIKeySecretKey], model.Spec.AnthropicVertexAI.ProjectID)
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

type provenanceEntry struct {
	APIVersion string    `json:"apiVersion"`
	Kind       string    `json:"kind"`
	Name       string    `json:"name"`
	Key        string    `json:"key,omitempty"`
	UID        types.UID `json:"uid"`
	Generation int64     `json:"generation,omitempty"`
	Hash       string    `json:"hash"`
}

func (c *Compiler) buildProvenance(ctx context.Context, input *translator.HarnessInput, environment []corev1.EnvVar) ([]byte, error) {
	harness := input.Harness
	var entries []provenanceEntry
	// Inline configuration is recorded by the enclosing Agent provenance.
	if harness.Source != nil {
		entries = append(entries, objectProvenance(v1alpha3.GroupVersion.String(), "Harness", harness.Name, harness.Source.UID, harness.Source.Generation, harness.Spec))
	}
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
	var addAgent func(*translator.AgentInput)
	addAgent = func(agent *translator.AgentInput) {
		template, model := agent.Template, agent.ResolvedModelConfig.Config
		if template.Source != nil {
			addObject("AgentTemplate", template.Name, template.Source.UID, template.Source.Generation, template.Spec)
		}
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
		_, ok := secret.Data[ref.Key]
		if !ok {
			return nil, fmt.Errorf("secret %q does not contain key %q", ref.Name, ref.Key)
		}
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

var _ translator.HarnessCompiler = (*Compiler)(nil)
