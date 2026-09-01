package adkconfig

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
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

// Builder assembles resolved inputs into an ADK agent configuration.
type Builder struct{ kube v2translator.Reader }

// NewBuilder constructs an ADK configuration builder.
func NewBuilder(kube v2translator.Reader) *Builder { return &Builder{kube: kube} }

type Result struct {
	Config      *adk.AgentConfig
	Models      []*v1alpha3.ModelConfig
	Templates   []*v1alpha3.AgentTemplate
	Environment []corev1.EnvVar
	Egress      []string
}

// ModelResult is the runtime configuration contributed by one ModelConfig.
type ModelResult struct {
	Config      *v1alpha3.ModelConfig
	Model       adk.Model
	Environment []corev1.EnvVar
	Egress      []string
}

// BuildModel translates a standalone ModelConfig without building an agent.
func (c *Builder) BuildModel(ctx context.Context, namespace, name string) (*ModelResult, error) {
	config := &v1alpha3.ModelConfig{}
	if err := c.kube.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, config); err != nil {
		return nil, err
	}
	runtime, err := c.resolveModel(ctx, config)
	if err != nil {
		return nil, err
	}
	if runtime.HasUnsupportedVolumes {
		return nil, v2translator.NewValidationError("ModelConfig requires volume mounts unsupported by Substrate ActorTemplate")
	}
	return &ModelResult{
		Config: config, Model: runtime.Model, Environment: runtime.Environment,
		Egress: agentConfigDestinations(&adk.AgentConfig{}, config, runtime.Model),
	}, nil
}

// HarnessEnvironment converts portable Harness environment entries to Pod environment variables.
func HarnessEnvironment(harness *v1alpha3.Harness) []corev1.EnvVar {
	environment := make([]corev1.EnvVar, 0, len(harness.Spec.Env))
	for _, value := range harness.Spec.Env {
		variable := corev1.EnvVar{Name: value.Name}
		if value.Value != nil {
			variable.Value = *value.Value
		} else {
			variable.ValueFrom = &corev1.EnvVarSource{SecretKeyRef: value.CredentialRef.DeepCopy()}
		}
		environment = append(environment, variable)
	}
	return environment
}

func (c *Builder) Build(ctx context.Context, input *v2translator.AgentInput) (*Result, error) {
	return c.compileAgent(ctx, input)
}

func (c *Builder) compileAgent(ctx context.Context, input *v2translator.AgentInput) (*Result, error) {
	modelRuntime := &modelRuntime{data: &modelDeploymentData{}}
	if input.ModelConfig != nil {
		var err error
		modelRuntime, err = c.resolveModel(ctx, input.ModelConfig)
		if err != nil {
			return nil, fmt.Errorf("resolve ModelConfig %q: %w", input.ModelConfig.Name, err)
		}
	}
	if modelRuntime.HasUnsupportedVolumes {
		return nil, v2translator.NewValidationError("ModelConfig requires volume mounts unsupported by Substrate ActorTemplate")
	}
	stream := true
	cfg := &adk.AgentConfig{Model: modelRuntime.Model, Description: input.Template.Spec.Description, Instruction: input.Instruction, Stream: &stream}
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
		if err := c.addRemoteMCPServer(cfg, modelRuntime, server, ref, headers); err != nil {
			return nil, fmt.Errorf("compile %s %q: %w", tool.Binding.Server.Kind, tool.Binding.Server.Name, err)
		}
		modelRuntime.Environment = append(modelRuntime.Environment, credentialEnv...)
	}
	if modelRuntime.HasUnsupportedVolumes {
		return nil, v2translator.NewValidationError("resolved model or MCP configuration requires volume mounts unsupported by Substrate ActorTemplate")
	}
	result := &Result{
		Config: cfg, Templates: []*v1alpha3.AgentTemplate{input.Template},
		Environment: modelRuntime.Environment,
		Egress:      append(agentConfigDestinations(cfg, input.ModelConfig, modelRuntime.Model), pluginEgress...),
	}
	if input.ModelConfig != nil {
		result.Models = []*v1alpha3.ModelConfig{input.ModelConfig}
	}
	for _, binding := range input.Shared {
		child, err := c.compileAgent(ctx, binding.Agent)
		if err != nil {
			return nil, err
		}
		child.Config.Name, child.Config.Description = binding.Name, binding.Description
		cfg.SubAgents = append(cfg.SubAgents, child.Config)
		result.Models = append(result.Models, child.Models...)
		result.Templates = append(result.Templates, child.Templates...)
		result.Environment = append(result.Environment, child.Environment...)
		result.Egress = append(result.Egress, child.Egress...)
	}
	return result, nil
}

// ResolveEnvironment replaces Kubernetes Secret references with literals
// because Substrate ActorTemplates accept only literal environment values.
func (c *Builder) ResolveEnvironment(ctx context.Context, namespace string, environment []corev1.EnvVar) ([]corev1.EnvVar, error) {
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

// BuildProvenance records every Kubernetes input that can change the compiled
// runtime. Sorting makes the JSON stable across map iteration order.
func (c *Builder) BuildProvenance(ctx context.Context, harness *v1alpha3.Harness, templates []*v1alpha3.AgentTemplate, models []*v1alpha3.ModelConfig, environment []corev1.EnvVar) ([]byte, error) {
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
func (c *Builder) resolveAgentTemplateHeaders(ctx context.Context, namespace string, refs []v1alpha3.ValueRef) (map[string]string, []corev1.EnvVar, error) {
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

func (c *Builder) resolveValueRef(ctx context.Context, namespace string, ref v1alpha3.ValueRef) (string, string, error) {
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
// DedupeEnv preserves first-seen ordering but gives the last value for a name
// precedence, matching how compiler layers are applied.
func DedupeEnv(values []corev1.EnvVar) []corev1.EnvVar {
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
	if modelConfig == nil {
		slices.Sort(destinations)
		return slices.Compact(destinations)
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
