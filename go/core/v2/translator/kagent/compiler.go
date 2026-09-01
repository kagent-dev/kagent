package kagent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	v2translator "github.com/kagent-dev/kagent/go/core/v2/translator"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// provenanceEntry records one Kubernetes input to a compiled revision. Secret
// entries identify a single key and hash its value; secret values are never stored.
type provenanceEntry struct {
	APIVersion string    `json:"apiVersion"`
	Kind       string    `json:"kind"`
	Name       string    `json:"name"`
	Key        string    `json:"key,omitempty"`
	UID        types.UID `json:"uid"`
	Generation int64     `json:"generation,omitempty"`
	Hash       string    `json:"hash"`
}

// Compiler translates resolved inputs into a kagent runtime revision.
type Compiler struct{ kube v2translator.Reader }

// NewCompiler constructs a kagent harness compiler.
func NewCompiler(kube v2translator.Reader) *Compiler { return &Compiler{kube: kube} }

type compiledAgent struct {
	config      *adk.AgentConfig
	models      []*v1alpha3.ModelConfig
	templates   []*v1alpha3.AgentTemplate
	environment []corev1.EnvVar
	egress      []string
}

func (c *Compiler) Compile(ctx context.Context, input *v2translator.HarnessInput) (*v2translator.Compilation, error) {
	compiled, err := c.compileAgent(ctx, input.Root)
	if err != nil {
		return nil, err
	}
	template, harness := input.Root.Template, input.Harness
	cfg := compiled.config
	if memory := harness.Spec.Kagent.Memory; memory != nil {
		modelConfig := &v1alpha3.ModelConfig{}
		name := memory.ModelConfigRef.Name
		if err := c.kube.Get(ctx, types.NamespacedName{Namespace: harness.Namespace, Name: name}, modelConfig); err != nil {
			return nil, fmt.Errorf("resolve memory ModelConfig %q: %w", name, err)
		}
		modelRuntime, err := c.resolveModel(ctx, modelConfig)
		if err != nil {
			return nil, fmt.Errorf("resolve memory ModelConfig %q: %w", name, err)
		}
		if modelRuntime.HasUnsupportedVolumes {
			return nil, v2translator.NewValidationError("memory ModelConfig requires volume mounts unsupported by Substrate ActorTemplate")
		}
		cfg.Memory = &adk.MemoryConfig{TTLDays: memory.TTLDays, Embedding: adk.ModelToEmbeddingConfig(modelRuntime.Model)}
		compiled.models = append(compiled.models, modelConfig)
		compiled.environment = append(compiled.environment, modelRuntime.Environment...)
		compiled.egress = append(compiled.egress, agentConfigDestinations(&adk.AgentConfig{}, modelConfig, modelRuntime.Model)...)
	}
	// The async driver is named, because the Python runtime cannot infer it and the Go
	// one does not need it. `DatabaseSessionService` builds an asyncio engine and
	// refuses a bare `sqlite:` URL with "the asyncio extension requires an async
	// driver" — so a kagent-adk actor never opened its readiness port, and the harness
	// sat in ResumeGoldenActor until the golden actor timed out. The Go ADK accepts
	// `sqlite+<driver>` and strips the driver (see adk/pkg/session.sqlitePathFromURL),
	// so one URL serves both.
	cfg.SessionDBURL = "sqlite+aiosqlite:////data/sessions.db"

	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal agent config: %w", err)
	}
	cardJSON, err := json.Marshal(agentTemplateCard(template))
	if err != nil {
		return nil, fmt.Errorf("marshal agent card: %w", err)
	}

	// Harness env is applied after provider env. dedupeEnv deliberately gives
	// later entries precedence, allowing the Harness to override defaults.
	environment := append([]corev1.EnvVar(nil), compiled.environment...)
	for _, value := range harness.Spec.Env {
		envVar := corev1.EnvVar{Name: value.Name}
		if value.Value != nil {
			envVar.Value = *value.Value
		} else {
			envVar.ValueFrom = &corev1.EnvVarSource{SecretKeyRef: value.CredentialRef.DeepCopy()}
		}
		environment = append(environment, envVar)
	}
	environment = append(environment,
		corev1.EnvVar{Name: env.KagentName.Name(), Value: template.Name + "-" + harness.Name},
		corev1.EnvVar{Name: env.KagentNamespace.Name(), Value: template.Namespace},
		corev1.EnvVar{Name: env.KagentURL.Name(), Value: fmt.Sprintf("http://%s.%s:8083", utils.GetControllerName(), utils.GetResourceNamespace())},
		corev1.EnvVar{Name: env.KagentGRPCURL.Name(), Value: fmt.Sprintf("%s.%s:8084", utils.GetControllerName(), utils.GetResourceNamespace())},
		corev1.EnvVar{Name: "PORT", Value: "80"},
		corev1.EnvVar{Name: "KAGENT_A2A_GRPC_ADDRESS", Value: "[::]:80"},
		corev1.EnvVar{Name: "KAGENT_PRE_RESPONSE_TRACE_FLUSH", Value: "true"},
	)
	environment = dedupeEnv(environment)

	// One provenance list covers every Kubernetes input, including hashed Secret
	// keys, so it both explains and participates in revision identity.
	provenance, err := c.buildProvenance(ctx, harness, compiled.templates, compiled.models, environment)
	if err != nil {
		return nil, fmt.Errorf("build revision provenance: %w", err)
	}
	environment, err = c.resolveEnvironment(ctx, template.Namespace, environment)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime environment: %w", err)
	}

	egressDestinations := compiled.egress
	slices.Sort(egressDestinations)
	egressDestinations = slices.Compact(egressDestinations)

	return &v2translator.Compilation{Revision: v2translator.Revision{
		Namespace:          template.Namespace,
		AgentTemplateName:  template.Name,
		HarnessName:        harness.Name,
		Image:              harness.Spec.Workload.Image,
		Environment:        environment,
		ConfigJSON:         configJSON,
		AgentCardJSON:      cardJSON,
		WorkerPoolName:     harness.Spec.Substrate.WorkerPoolRef.Name,
		SnapshotLocation:   harness.Spec.Substrate.SnapshotPolicy.Location,
		Provenance:         provenance,
		EgressDestinations: egressDestinations,
	},
	}, nil
}

func (c *Compiler) compileAgent(ctx context.Context, input *v2translator.AgentInput) (*compiledAgent, error) {
	if input.ResolvedModelConfig == nil {
		return nil, fmt.Errorf("resolved ModelConfig is required")
	}
	modelConfig := input.ResolvedModelConfig.Config
	if modelConfig == nil {
		return nil, fmt.Errorf("resolved ModelConfig configuration is required")
	}
	model, data, err := c.renderModel(ctx, input.ResolvedModelConfig)
	if err != nil {
		return nil, fmt.Errorf("render ModelConfig %q: %w", modelConfig.Name, err)
	}
	runtimeModel := &modelRuntime{
		Model: model, Environment: data.EnvVars,
		HasUnsupportedVolumes: len(data.Volumes) > 0 || len(data.VolumeMounts) > 0,
		data:                  data,
	}
	if runtimeModel.HasUnsupportedVolumes {
		return nil, v2translator.NewValidationError("ModelConfig requires volume mounts unsupported by Substrate ActorTemplate")
	}
	stream := true
	cfg := &adk.AgentConfig{Model: runtimeModel.Model, Description: input.Template.Spec.Description, Instruction: input.Instruction, Stream: &stream}
	pluginConfig, pluginEgress, err := v2translator.CompileSkillResources(input.Template)
	if err != nil {
		return nil, err
	}
	if len(pluginConfig.Skills) > 0 || len(pluginConfig.Plugins) > 0 {
		cfg.AgentPlugins = &pluginConfig
	}
	for _, tool := range input.MCPTools {
		ref := &v1alpha3.McpServerTool{TypedReference: v1alpha3.TypedReference{
			ApiGroup: "kagent.dev", Kind: tool.Binding.Server.Kind, Name: tool.Binding.Server.Name,
		}, ToolNames: append([]string(nil), tool.Binding.Tools...)}
		headers, credentialEnv, err := c.resolveAgentTemplateHeaders(ctx, input.Template.Namespace, tool.Server.Spec.HeadersFrom)
		if err != nil {
			return nil, fmt.Errorf("resolve %s %q: %w", tool.Binding.Server.Kind, tool.Binding.Server.Name, err)
		}
		server := tool.Server.DeepCopy()
		server.Spec.HeadersFrom = nil
		if err := c.addRemoteMCPServer(cfg, runtimeModel, server, ref, headers); err != nil {
			return nil, fmt.Errorf("compile %s %q: %w", tool.Binding.Server.Kind, tool.Binding.Server.Name, err)
		}
		runtimeModel.Environment = append(runtimeModel.Environment, credentialEnv...)
	}
	if runtimeModel.HasUnsupportedVolumes {
		return nil, v2translator.NewValidationError("resolved model or MCP configuration requires volume mounts unsupported by Substrate ActorTemplate")
	}
	result := &compiledAgent{
		config: cfg, models: []*v1alpha3.ModelConfig{modelConfig}, templates: []*v1alpha3.AgentTemplate{input.Template},
		environment: runtimeModel.Environment,
		egress:      append(agentConfigDestinations(cfg, modelConfig, runtimeModel.Model), pluginEgress...),
	}
	for _, binding := range input.Shared {
		child, err := c.compileAgent(ctx, binding.Agent)
		if err != nil {
			return nil, err
		}
		child.config.Name, child.config.Description = binding.Name, binding.Description
		cfg.SubAgents = append(cfg.SubAgents, child.config)
		result.models = append(result.models, child.models...)
		result.templates = append(result.templates, child.templates...)
		result.environment = append(result.environment, child.environment...)
		result.egress = append(result.egress, child.egress...)
	}
	return result, nil
}

// resolveEnvironment replaces Kubernetes Secret references with literals
// because Substrate ActorTemplates accept only literal environment values.
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
		secret := &corev1.Secret{}
		if err := c.kube.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, secret); err != nil {
			return nil, err
		}
		value, ok := secret.Data[ref.Key]
		if !ok {
			return nil, fmt.Errorf("secret %q does not contain key %q", ref.Name, ref.Key)
		}
		resolved[i].Value = string(value)
		resolved[i].ValueFrom = nil
	}
	return resolved, nil
}

// buildProvenance records every Kubernetes input that can change the compiled
// runtime. Sorting makes the JSON stable across map iteration order.
func (c *Compiler) buildProvenance(ctx context.Context, harness *v1alpha3.Harness, templates []*v1alpha3.AgentTemplate, models []*v1alpha3.ModelConfig, environment []corev1.EnvVar) ([]byte, error) {
	entries := []provenanceEntry{objectProvenance(v1alpha3.GroupVersion.String(), "Harness", harness.Name, harness.UID, harness.Generation, harness.Spec)}
	configMaps := map[string]struct{}{}
	for _, template := range templates {
		entries = append(entries, objectProvenance(v1alpha3.GroupVersion.String(), "AgentTemplate", template.Name, template.UID, template.Generation, template.Spec))
		if template.Spec.SystemPromptFrom != nil {
			configMaps[template.Spec.SystemPromptFrom.Name] = struct{}{}
		}
		if template.Spec.PromptTemplate != nil {
			for _, source := range template.Spec.PromptTemplate.DataSources {
				configMaps[source.Name] = struct{}{}
			}
		}
	}
	for _, model := range models {
		entries = append(entries, objectProvenance(v1alpha3.GroupVersion.String(), "ModelConfig", model.Name, model.UID, model.Generation, model.Spec))
	}
	for name := range configMaps {
		configMap := &corev1.ConfigMap{}
		if err := c.kube.Get(ctx, types.NamespacedName{Namespace: harness.Namespace, Name: name}, configMap); err != nil {
			return nil, err
		}
		entries = append(entries, objectProvenance("v1", "ConfigMap", name, configMap.UID, configMap.Generation, configMap.Data))
	}
	for _, template := range templates {
		for _, binding := range template.Spec.Tools {
			if binding.MCP == nil {
				continue
			}
			switch binding.MCP.Server.Kind {
			case "RemoteMCPServer":
				server := &v1alpha3.RemoteMCPServer{}
				if err := c.kube.Get(ctx, types.NamespacedName{Namespace: template.Namespace, Name: binding.MCP.Server.Name}, server); err != nil {
					return nil, err
				}
				entries = append(entries, objectProvenance(v1alpha3.GroupVersion.String(), "RemoteMCPServer", server.Name, server.UID, server.Generation, server.Spec))
			}
		}
	}
	// Secret provenance contains only UID and value hash. Name+key deduplication
	// keeps repeated references from changing the digest.
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
		secret := &corev1.Secret{}
		if err := c.kube.Get(ctx, types.NamespacedName{Namespace: harness.Namespace, Name: ref.Name}, secret); err != nil {
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
	entries = slices.Compact(entries)
	return json.Marshal(entries)
}

// objectProvenance hashes the relevant object content rather than relying
// on generation alone, which is not available or meaningful for every input.
func objectProvenance(apiVersion, kind, name string, uid types.UID, generation int64, content any) provenanceEntry {
	raw, _ := json.Marshal(content)
	hash := sha256.Sum256(raw)
	return provenanceEntry{APIVersion: apiVersion, Kind: kind, Name: name, UID: uid, Generation: generation, Hash: fmt.Sprintf("%x", hash[:])}
}

// resolveAgentTemplateHeaders keeps Secret values out of serialized agent
// config. The runtime expands __KAGENT_ENV[...]__ from the corresponding
// Secret-backed environment variable when it constructs the MCP request.
func (c *Compiler) resolveAgentTemplateHeaders(ctx context.Context, namespace string, refs []v1alpha3.ValueRef) (map[string]string, []corev1.EnvVar, error) {
	headers := make(map[string]string, len(refs))
	var environment []corev1.EnvVar
	for _, ref := range refs {
		if ref.ValueFrom == nil || ref.ValueFrom.Type != v1alpha3.SecretValueSource {
			name, value, err := c.resolveValueRef(ctx, namespace, ref)
			if err != nil {
				return nil, nil, err
			}
			headers[name] = value
			continue
		}
		selector := &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: ref.ValueFrom.Name}, Key: ref.ValueFrom.Key}
		sum := sha256.Sum256([]byte(namespace + "\x00" + selector.Name + "\x00" + selector.Key))
		envName := "KAGENT_CREDENTIAL_" + strings.ToUpper(fmt.Sprintf("%x", sum[:8]))
		headers[ref.Name] = "__KAGENT_ENV[" + envName + "]__"
		environment = append(environment, corev1.EnvVar{Name: envName, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: selector}})
	}
	return headers, environment, nil
}

func (c *Compiler) resolveValueRef(ctx context.Context, namespace string, ref v1alpha3.ValueRef) (string, string, error) {
	if ref.ValueFrom == nil {
		return ref.Name, ref.Value, nil
	}
	if ref.ValueFrom.Type != v1alpha3.ConfigMapValueSource {
		return "", "", fmt.Errorf("unsupported value source type %q", ref.ValueFrom.Type)
	}
	configMap := &corev1.ConfigMap{}
	if err := c.kube.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.ValueFrom.Name}, configMap); err != nil {
		return "", "", err
	}
	value, found := configMap.Data[ref.ValueFrom.Key]
	if !found {
		return "", "", fmt.Errorf("ConfigMap %q does not contain key %q", ref.ValueFrom.Name, ref.ValueFrom.Key)
	}
	return ref.Name, value, nil
}

// agentTemplateCard describes the runtime-local A2A server. Substrate routes
// hitlExtensionURI is the human-in-the-loop A2A extension the kagent runtime
// negotiates. Spelled here rather than imported from the ADK package so the
// controller does not depend on the runtime's module for one constant; the two
// must agree, and `agentcard.go` is the definition.
const hitlExtensionURI = "https://kagent.dev/extensions/hitl/v1"

// public traffic to this loopback interface; the card must not advertise a
// cluster-specific external address.
func agentTemplateCard(template *v1alpha3.AgentTemplate) *a2atype.AgentCard {
	return &a2atype.AgentCard{
		Name:        strings.ReplaceAll(template.Name, "-", "_"),
		Description: template.Spec.Description,
		Version:     "v1",
		SupportedInterfaces: []*a2atype.AgentInterface{{
			URL:             "http://127.0.0.1:80",
			ProtocolBinding: a2atype.TransportProtocolGRPC,
			ProtocolVersion: a2atype.Version,
		}},
		// This compiler builds cards for the kagent runtime specifically, whose A2A
		// layer always negotiates human-in-the-loop (see adk/pkg/a2a/agentcard.go,
		// which appends this extension unconditionally). Declaring it here is what
		// makes an agent's question discoverably answerable: a client reads the card
		// to learn it may request the extension and render the choices. Other
		// harnesses compile their own cards and make no such claim.
		Capabilities: a2atype.AgentCapabilities{
			Streaming: true,
			Extensions: []a2atype.AgentExtension{{
				URI:         hitlExtensionURI,
				Description: "Human in the loop for tool approval, ask user, and nested subagents",
			}},
		},
		Skills:             []a2atype.AgentSkill{},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}
}

// dedupeEnv preserves first-seen ordering but gives the last value for a name
// precedence, matching how compiler layers are applied.
func dedupeEnv(values []corev1.EnvVar) []corev1.EnvVar {
	result := make([]corev1.EnvVar, 0, len(values))
	index := map[string]int{}
	for _, value := range values {
		if i, ok := index[value.Name]; ok {
			result[i] = value
			continue
		}
		index[value.Name] = len(result)
		result = append(result, value)
	}
	return result
}

// agentConfigDestinations extracts the network allowlist required by the
// resolved model and MCP configuration. Provider defaults are included when
// no explicit endpoint appears in the serialized model.
func agentConfigDestinations(cfg *adk.AgentConfig, modelConfig *v1alpha3.ModelConfig, model adk.Model) []string {
	destinations := make([]string, 0, len(cfg.HttpTools)+len(cfg.SseTools)+1)
	for _, tool := range cfg.HttpTools {
		destinations = appendURLHost(destinations, tool.Params.Url)
	}
	for _, tool := range cfg.SseTools {
		destinations = appendURLHost(destinations, tool.Params.Url)
	}
	modelJSON, _ := json.Marshal(model)
	var values any
	if json.Unmarshal(modelJSON, &values) == nil {
		destinations = appendURLValues(destinations, values)
	}
	switch modelConfig.Spec.Provider {
	case v1alpha3.ModelProviderOpenAI:
		destinations = append(destinations, "api.openai.com")
	case v1alpha3.ModelProviderAnthropic:
		destinations = append(destinations, "api.anthropic.com")
	case v1alpha3.ModelProviderGemini:
		destinations = append(destinations, "generativelanguage.googleapis.com")
	}
	slices.Sort(destinations)
	return slices.Compact(destinations)
}

// appendURLValues walks serialized provider config because endpoint fields are
// provider-specific but all URLs reduce to the same hostname allowlist.
func appendURLValues(destinations []string, value any) []string {
	switch value := value.(type) {
	case string:
		return appendURLHost(destinations, value)
	case []any:
		for _, item := range value {
			destinations = appendURLValues(destinations, item)
		}
	case map[string]any:
		for _, item := range value {
			destinations = appendURLValues(destinations, item)
		}
	}
	return destinations
}

func appendURLHost(destinations []string, raw string) []string {
	parsed, err := url.Parse(raw)
	if err == nil && parsed.Hostname() != "" {
		return append(destinations, parsed.Hostname())
	}
	return destinations
}
