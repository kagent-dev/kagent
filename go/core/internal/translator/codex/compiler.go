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

	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
	codexconfig "github.com/kagent-dev/kagent/go/harness/codex/config"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

const (
	codexHomeEnv       = "CODEX_HOME"
	openAIAPIKeyEnv    = "OPENAI_API_KEY"
	awsRegionEnv       = "AWS_REGION"
	awsBedrockTokenEnv = "AWS_BEARER_TOKEN_BEDROCK"
	awsAccessKeyEnv    = "AWS_ACCESS_KEY_ID"
	awsSecretKeyEnv    = "AWS_SECRET_ACCESS_KEY"
	awsSessionTokenEnv = "AWS_SESSION_TOKEN"
)

var ownedEnvironment = map[string]struct{}{
	codexHomeEnv: {}, openAIAPIKeyEnv: {}, awsRegionEnv: {}, awsBedrockTokenEnv: {},
	awsAccessKeyEnv: {}, awsSecretKeyEnv: {}, awsSessionTokenEnv: {},
}

type Compiler struct {
	settings    *configv1.RuntimeDefaultsConfig
	ctx         krt.HandlerContext
	collections translator.Collections
}

func NewCompiler(ctx krt.HandlerContext, collections translator.Collections, settings *configv1.RuntimeDefaultsConfig) *Compiler {
	return &Compiler{settings: settings, ctx: ctx, collections: collections}
}

func (c *Compiler) Compile(ctx context.Context, input *translator.HarnessInput) (*translator.CompileResult, error) {
	if input == nil || input.Harness == nil || input.Root == nil || input.Root.Template == nil || input.Root.ResolvedModelConfig == nil || input.Root.ResolvedModelConfig.Config == nil {
		return nil, fmt.Errorf("codex compiler requires a resolved Harness, AgentTemplate, and ModelConfig")
	}
	model := input.Root.ResolvedModelConfig.Config
	if strings.TrimSpace(model.Spec.Model) == "" {
		return nil, translator.NewValidationError("Codex ModelConfig model is required")
	}
	if !model.Spec.TLS.IsEmpty() {
		return nil, translator.NewValidationError("Codex does not support ModelConfig TLS")
	}
	telemetryConfig, err := translator.TelemetryConfigFromSettings(c.settings.GetTelemetry())
	if err != nil {
		return nil, err
	}
	traceConfig, logConfig := telemetryConfig.Traces, telemetryConfig.Logs
	template, harness := input.Root.Template, input.Harness
	// The runtime reports this identity on every invocation span and on its
	// resource, so a user-supplied resource marker is never required.
	runtimeTelemetry := telemetryConfig.RuntimeTelemetry(
		tracing.RuntimeCodex, input.AgentName, template.Namespace, model.Spec)

	provider, providerEnvironment, egress, err := c.compileProvider(ctx, model)
	if err != nil {
		return nil, err
	}
	skills, err := translator.CompileSkillResources(input.Root.Template)
	if err != nil {
		return nil, err
	}
	mcp, err := compileMCP(input.Root.MCPTools)
	if err != nil {
		return nil, err
	}
	environment := providerEnvironment
	for _, variable := range input.Harness.Spec.Env {
		_, reserved := ownedEnvironment[variable.Name]
		if reserved {
			return nil, translator.NewValidationError("Harness env %q conflicts with Codex's compiled configuration", variable.Name)
		}
		environment = append(environment, corev1.EnvVar{Name: variable.Name, Value: variable.Value})
	}

	agents, err := compileAgents(input.Root)
	if err != nil {
		return nil, err
	}
	cfg := codexconfig.Production(model.Spec.Model, input.Root.Instruction)
	headers, passthrough, _, err := translator.ModelCredentialHeaders(input.Root.ResolvedModelConfig, model.Spec.DefaultHeaders)
	if err != nil {
		return nil, err
	}
	if len(passthrough) > 0 {
		return nil, translator.NewValidationError("Codex does not support tokenPassthrough")
	}
	cfg.ModelHeaders = headers
	cfg.Provider, cfg.Agents, cfg.MCPServers = provider, agents, mcp.servers
	cfg.RuntimeTelemetry = runtimeTelemetry
	cfg.ControlPlaneURL = c.settings.GetControlPlaneUrl()
	cfg.MaxContentLength = c.settings.MaxContentLength // Preserve unset versus unlimited.
	cfg.LogLevel = c.settings.GetLogging().GetLevel()
	if traceConfig.Enabled || logConfig.Enabled {
		cfg.Telemetry = &codexconfig.Telemetry{CaptureContent: telemetryConfig.Runtime.CaptureContent}
		if traceConfig.Enabled {
			cfg.Telemetry.Traces = &codexconfig.OTLPExporter{Endpoint: traceConfig.Endpoint, Protocol: traceConfig.Protocol}
		}
		if logConfig.Enabled {
			cfg.Telemetry.Logs = &codexconfig.OTLPExporter{Endpoint: logConfig.Endpoint, Protocol: logConfig.Protocol}
		}
	}
	if len(skills.Resources.Skills) != 0 || len(skills.Resources.Plugins) != 0 {
		cfg.SkillResources = &skills.Resources
	}
	if err := cfg.Validate(); err != nil {
		return nil, translator.NewValidationError("invalid compiled Codex configuration: %v", err)
	}
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal Codex config: %w", err)
	}
	card, err := pbconv.ToProtoAgentCard(translator.ManagedAgentCard(input.AgentName, input.Root.Template))
	if err != nil {
		return nil, fmt.Errorf("convert Codex agent card: %w", err)
	}
	provenance, err := c.buildProvenance(ctx, input, environment)
	if err != nil {
		return nil, fmt.Errorf("build Codex revision provenance: %w", err)
	}
	credentials, err := translator.CompileCredentials(input, nil, environment)
	if err != nil {
		return nil, err
	}
	egress = append(egress, skills.Egress...)
	egress = append(egress, mcp.egress...)
	egress = append(egress, telemetryConfig.Destinations(environment)...)
	egress = append(egress, c.settings.GetControlPlaneUrl())
	slices.Sort(egress)
	egress = slices.Compact(egress)
	return &translator.CompileResult{IntegrationGrants: credentials.IntegrationGrants,
		Revision: translator.Revision{
			Namespace:   template.Namespace,
			SkillImages: skills.Images,
			Environment: environment, ConfigJSON: configJSON, AgentCard: card,
			WorkerPoolName: harness.Spec.Substrate.WorkerPoolRef.Name, SnapshotLocation: harness.Spec.Substrate.SnapshotPolicy.Location,
			TokenPassthroughTargets: credentials.TokenPassthroughTargets, Credentials: credentials.Credentials, Provenance: provenance, EgressDestinations: egress,
		},
		Warnings: mcp.warnings,
	}, nil
}

func (c *Compiler) compileProvider(ctx context.Context, model *v1alpha3.ModelConfig) (codexconfig.Provider, []corev1.EnvVar, []string, error) {
	switch model.Spec.Provider {
	case v1alpha3.ModelProviderOpenAI:
		if model.Spec.OpenAI == nil || model.Spec.OpenAI.APIFormat == nil || *model.Spec.OpenAI.APIFormat != v1alpha3.OpenAIAPIFormatResponses {
			return codexconfig.Provider{}, nil, nil, translator.NewValidationError("Codex OpenAI requires openAI.apiFormat responses")
		}
		options := *model.Spec.OpenAI
		baseURL := strings.TrimSpace(options.BaseURL)
		options.BaseURL, options.APIFormat = "", nil
		if !reflect.DeepEqual(options, v1alpha3.OpenAIConfig{}) {
			return codexconfig.Provider{}, nil, nil, translator.NewValidationError("Codex does not support OpenAI provider options beyond baseUrl and apiFormat responses")
		}

		provider := codexconfig.Provider{Name: "openai", BaseURL: baseURL}
		egress := []string{"https://api.openai.com:443"}
		if baseURL != "" {
			host, err := absoluteHTTPOrigin(baseURL)
			if err != nil {
				return codexconfig.Provider{}, nil, nil, translator.NewValidationError("Codex OpenAI baseUrl %v", err)
			}
			egress = []string{host}
		}
		return provider, []corev1.EnvVar{{Name: openAIAPIKeyEnv, Value: translator.CredentialPlaceholder}}, egress, nil
	case v1alpha3.ModelProviderBedrock:
		if model.Spec.Bedrock == nil || strings.TrimSpace(model.Spec.Bedrock.Region) == "" {
			return codexconfig.Provider{}, nil, nil, translator.NewValidationError("Codex Bedrock requires bedrock.region")
		}
		options := *model.Spec.Bedrock
		region := strings.TrimSpace(options.Region)
		if !strings.HasPrefix(model.Spec.Model, "gpt-") {
			return codexconfig.Provider{}, nil, nil, translator.NewValidationError("Codex Bedrock supports only OpenAI gpt-* model IDs")
		}
		options.Region = ""
		if options.CacheTTL == "5m" {
			options.CacheTTL = ""
		}
		if !reflect.DeepEqual(options, v1alpha3.BedrockConfig{}) {
			return codexconfig.Provider{}, nil, nil, translator.NewValidationError("Codex does not support Bedrock provider options beyond region")
		}
		environment := []corev1.EnvVar{{Name: awsRegionEnv, Value: region}, {Name: awsBedrockTokenEnv, Value: translator.CredentialPlaceholder}}
		return codexconfig.Provider{Name: "amazon-bedrock"}, environment, []string{"https://bedrock-runtime." + region + ".amazonaws.com:443"}, nil
	default:
		return codexconfig.Provider{}, nil, nil, translator.NewValidationError("Codex does not support ModelConfig provider %q", model.Spec.Provider)
	}
}

func compileAgents(root *translator.AgentInput) (map[string]codexconfig.Agent, error) {
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
			return nil, translator.NewValidationError("Codex Shared agent %q cannot contain tools, skills, plugins, or nested agents", binding.Name)
		}
		if !sameProviderConfiguration(root.ResolvedModelConfig.Config.Spec, child.ResolvedModelConfig.Config.Spec) {
			return nil, translator.NewValidationError("Codex Shared agent %q must use the root provider and authentication configuration", binding.Name)
		}
		if _, exists := agents[binding.Name]; exists {
			return nil, translator.NewValidationError("duplicate Codex Shared agent name %q", binding.Name)
		}
		agents[binding.Name] = codexconfig.Agent{Description: binding.Description, Instruction: child.Instruction, Model: child.ResolvedModelConfig.Config.Spec.Model}
	}
	return agents, nil
}

func sameProviderConfiguration(root, child v1alpha3.ModelConfigSpec) bool {
	root.Model, child.Model = "", ""
	return reflect.DeepEqual(root, child)
}

func (c *Compiler) secret(ctx context.Context, namespace, name string) (*corev1.Secret, error) {
	secret := krt.FetchOne(c.ctx, c.collections.Secrets, krt.FilterObjectName(types.NamespacedName{Namespace: namespace, Name: name}))
	if secret == nil {
		return nil, fmt.Errorf("read Codex credential Secret %q: not found", name)
	}
	return *secret, nil
}

func absoluteHTTPOrigin(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", fmt.Errorf("must be an absolute HTTP(S) URL without credentials or fragment")
	}
	return egresspolicy.Origin(parsed), nil
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
	var entries []provenanceEntry
	// The Harness is always a standalone resource. Inline template configuration
	// is recorded by the enclosing Agent provenance below.
	entries = append(entries, objectProvenance(v1alpha3.GroupVersion.String(), "Harness", input.Harness.Name, input.Harness.Source.UID, input.Harness.Source.Generation, input.Harness.Spec))
	seenObjects := map[string]struct{}{}
	addObject := func(kind, name string, uid types.UID, generation int64, value any) {
		identity := kind + "\x00" + name
		if _, ok := seenObjects[identity]; !ok {
			seenObjects[identity] = struct{}{}
			entries = append(entries, objectProvenance(v1alpha3.GroupVersion.String(), kind, name, uid, generation, value))
		}
	}
	configMaps := map[string]struct{}{}
	var addAgent func(*translator.AgentInput)
	addAgent = func(agent *translator.AgentInput) {
		model := agent.ResolvedModelConfig.Config
		if source := agent.Template.Source; source != nil {
			addObject("AgentTemplate", source.Name, source.UID, source.Generation, agent.Template.Spec)
		}
		addObject("ModelConfig", model.Name, model.UID, model.Generation, model.Spec)
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
