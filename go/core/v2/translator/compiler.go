package translator

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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// revisionSource is safe provenance for a resolved revision. It records object
// identity and content hashes, never Secret values.
type revisionSource struct {
	APIVersion string    `json:"apiVersion"`
	Kind       string    `json:"kind"`
	Name       string    `json:"name"`
	Key        string    `json:"key,omitempty"`
	UID        types.UID `json:"uid"`
	Generation int64     `json:"generation,omitempty"`
	Hash       string    `json:"hash"`
}

// Compiler resolves public API objects into a complete, immutable runtime
// revision. It owns the v2 translation boundary rather than delegating to an
// earlier API translator.
type Compiler struct {
	kube               client.Client
	mcpEgressPlaintext bool
}

// NewCompiler constructs the v2 runtime compiler.
func NewCompiler(kube client.Client, mcpEgressPlaintext bool) *Compiler {
	return &Compiler{kube: kube, mcpEgressPlaintext: mcpEgressPlaintext}
}

// CompileAgentTemplate resolves an API v2 attachment into an immutable runtime
// revision. Nothing below this boundary needs to read the public API objects.
func (c *Compiler) CompileAgentTemplate(ctx context.Context, harness *v1alpha3.Harness, template *v1alpha3.AgentTemplate) (*Revision, error) {
	// Reject API features that the K3 runtime cannot represent before resolving
	// references or reading credentials.
	if harness.Namespace != template.Namespace {
		return nil, newValidationError("Harness and AgentTemplate must be in the same namespace")
	}
	if harness.Spec.Kagent == nil {
		return nil, newValidationError("Harness runtime is not supported by the K3 kagent adapter")
	}
	if len(template.Spec.Skills) > 0 {
		return nil, newValidationError("spec.skills is not supported yet")
	}
	if len(template.Spec.Plugins) > 0 {
		return nil, newValidationError("spec.plugins is not supported yet")
	}
	for _, tool := range template.Spec.Tools {
		if tool.Agent != nil {
			return nil, newValidationError("AgentTemplate-backed tools are not supported yet")
		}
	}

	modelConfig := &v1alpha3.ModelConfig{}
	if err := c.kube.Get(ctx, types.NamespacedName{Namespace: template.Namespace, Name: template.Spec.ModelConfig.Name}, modelConfig); err != nil {
		return nil, fmt.Errorf("resolve ModelConfig %q: %w", template.Spec.ModelConfig.Name, err)
	}
	modelRuntime, err := c.resolveModel(ctx, modelConfig)
	if err != nil {
		return nil, fmt.Errorf("resolve ModelConfig %q: %w", template.Spec.ModelConfig.Name, err)
	}
	if modelRuntime.HasUnsupportedVolumes {
		return nil, newValidationError("ModelConfig requires volume mounts unsupported by Substrate ActorTemplate")
	}

	instruction, err := c.resolveAgentTemplatePrompt(ctx, template)
	if err != nil {
		return nil, err
	}
	stream := true
	cfg := &adk.AgentConfig{
		Model:       modelRuntime.Model,
		Description: template.Spec.Description,
		Instruction: instruction,
		Stream:      &stream,
	}

	// Secret values stay in Kubernetes. Their hashes participate in the revision
	// identity so rotating a credential creates a new immutable revision.
	secretHashes := append([]byte(nil), modelRuntime.SecretHash...)
	for _, binding := range template.Spec.Tools {
		if binding.MCP == nil {
			return nil, newValidationError("tool binding must select an MCP server")
		}
		ref := &v1alpha3.McpServerTool{
			TypedReference: v1alpha3.TypedReference{
				ApiGroup: "kagent.dev",
				Kind:     binding.MCP.Server.Kind,
				Name:     binding.MCP.Server.Name,
			},
			ToolNames: append([]string(nil), binding.MCP.Tools...),
		}
		server, headers, credentialEnv, hash, err := c.resolveAgentTemplateMCPServer(ctx, template.Namespace, binding.MCP.Server)
		if err != nil {
			return nil, fmt.Errorf("resolve %s %q: %w", binding.MCP.Server.Kind, binding.MCP.Server.Name, err)
		}
		if err := c.addRemoteMCPServer(cfg, modelRuntime, server, ref, headers); err != nil {
			return nil, fmt.Errorf("compile %s %q: %w", binding.MCP.Server.Kind, binding.MCP.Server.Name, err)
		}
		modelRuntime.Environment = append(modelRuntime.Environment, credentialEnv...)
		secretHashes = append(secretHashes, hash...)
	}
	if modelRuntime.HasUnsupportedVolumes {
		return nil, newValidationError("resolved model or MCP configuration requires volume mounts unsupported by Substrate ActorTemplate")
	}

	if template.Spec.PromptTemplate != nil {
		// Execute after tool resolution so templates see the final enabled tool
		// names rather than the unresolved API bindings.
		refs := make([]promptSourceRef, 0, len(template.Spec.PromptTemplate.DataSources))
		for _, source := range template.Spec.PromptTemplate.DataSources {
			refs = append(refs, promptSourceRef{Name: source.Name, Alias: source.Alias})
		}
		lookup, err := resolvePromptSourceRefs(ctx, c.kube, template.Namespace, refs)
		if err != nil {
			return nil, fmt.Errorf("resolve prompt sources: %w", err)
		}
		toolNames := make([]string, 0)
		for _, binding := range template.Spec.Tools {
			if binding.MCP != nil {
				toolNames = append(toolNames, binding.MCP.Tools...)
			}
		}
		cfg.Instruction, err = executeSystemMessageTemplate(cfg.Instruction, lookup, PromptTemplateContext{
			AgentTemplateName:      template.Name,
			AgentTemplateNamespace: template.Namespace,
			Description:            template.Spec.Description,
			ToolNames:              toolNames,
		})
		if err != nil {
			return nil, err
		}
	}

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
	environment := append([]corev1.EnvVar(nil), modelRuntime.Environment...)
	for _, value := range harness.Spec.Env {
		envVar := corev1.EnvVar{Name: value.Name}
		if value.Value != nil {
			envVar.Value = *value.Value
		} else {
			envVar.ValueFrom = &corev1.EnvVarSource{SecretKeyRef: value.CredentialRef.DeepCopy()}
			hash, err := hashSecretKey(ctx, c.kube, template.Namespace, value.CredentialRef)
			if err != nil {
				return nil, fmt.Errorf("resolve Harness credential env %q: %w", value.Name, err)
			}
			secretHashes = append(secretHashes, hash...)
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

	// Capture provenance separately from runtime config so operators can explain
	// why two revisions differ without exposing credential contents.
	sources, err := c.revisionSourceSnapshot(ctx, harness, template, modelConfig, environment)
	if err != nil {
		return nil, fmt.Errorf("marshal revision sources: %w", err)
	}

	return &Revision{
		Namespace:          template.Namespace,
		AgentTemplateName:  template.Name,
		HarnessName:        harness.Name,
		Image:              harness.Spec.Workload.Image,
		Environment:        dedupeEnv(environment),
		ConfigJSON:         configJSON,
		AgentCardJSON:      cardJSON,
		WorkerPoolName:     harness.Spec.Substrate.WorkerPoolRef.Name,
		SnapshotLocation:   harness.Spec.Substrate.SnapshotPolicy.Location,
		SourceSnapshot:     sources,
		SecretHashes:       secretHashes,
		EgressDestinations: agentConfigDestinations(cfg, modelConfig, modelRuntime.Model),
	}, nil
}

// revisionSourceSnapshot records every Kubernetes input that can change the
// compiled runtime. Sorting makes the JSON stable across map iteration order.
func (c *Compiler) revisionSourceSnapshot(ctx context.Context, harness *v1alpha3.Harness, template *v1alpha3.AgentTemplate, model *v1alpha3.ModelConfig, environment []corev1.EnvVar) ([]byte, error) {
	sources := []revisionSource{
		objectRevisionSource(v1alpha3.GroupVersion.String(), "Harness", harness.Name, harness.UID, harness.Generation, harness.Spec),
		objectRevisionSource(v1alpha3.GroupVersion.String(), "AgentTemplate", template.Name, template.UID, template.Generation, template.Spec),
		objectRevisionSource(v1alpha3.GroupVersion.String(), "ModelConfig", model.Name, model.UID, model.Generation, model.Spec),
	}
	configMaps := map[string]struct{}{}
	if template.Spec.SystemPromptFrom != nil {
		configMaps[template.Spec.SystemPromptFrom.Name] = struct{}{}
	}
	if template.Spec.PromptTemplate != nil {
		for _, source := range template.Spec.PromptTemplate.DataSources {
			configMaps[source.Name] = struct{}{}
		}
	}
	for name := range configMaps {
		configMap := &corev1.ConfigMap{}
		if err := c.kube.Get(ctx, types.NamespacedName{Namespace: template.Namespace, Name: name}, configMap); err != nil {
			return nil, err
		}
		sources = append(sources, objectRevisionSource("v1", "ConfigMap", name, configMap.UID, configMap.Generation, configMap.Data))
	}
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
			sources = append(sources, objectRevisionSource(v1alpha3.GroupVersion.String(), "RemoteMCPServer", server.Name, server.UID, server.Generation, server.Spec))
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
		if err := c.kube.Get(ctx, types.NamespacedName{Namespace: template.Namespace, Name: ref.Name}, secret); err != nil {
			return nil, err
		}
		value, ok := secret.Data[ref.Key]
		if !ok {
			return nil, fmt.Errorf("secret %q does not contain key %q", ref.Name, ref.Key)
		}
		hash := sha256.Sum256(value)
		sources = append(sources, revisionSource{APIVersion: "v1", Kind: "Secret", Name: ref.Name, Key: ref.Key, UID: secret.UID, Hash: fmt.Sprintf("%x", hash[:])})
	}
	slices.SortFunc(sources, func(a, b revisionSource) int {
		return strings.Compare(a.APIVersion+"\x00"+a.Kind+"\x00"+a.Name+"\x00"+a.Key, b.APIVersion+"\x00"+b.Kind+"\x00"+b.Name+"\x00"+b.Key)
	})
	return json.Marshal(sources)
}

// objectRevisionSource hashes the relevant object content rather than relying
// on generation alone, which is not available or meaningful for every input.
func objectRevisionSource(apiVersion, kind, name string, uid types.UID, generation int64, content any) revisionSource {
	raw, _ := json.Marshal(content)
	hash := sha256.Sum256(raw)
	return revisionSource{APIVersion: apiVersion, Kind: kind, Name: name, UID: uid, Generation: generation, Hash: fmt.Sprintf("%x", hash[:])}
}

// resolveAgentTemplateMCPServer separates public server configuration from
// credentials. The returned copy has HeadersFrom cleared because the runtime
// receives resolved header placeholders and Secret-backed environment entries.
func (c *Compiler) resolveAgentTemplateMCPServer(ctx context.Context, namespace string, ref v1alpha3.AgentTemplateTypedLocalReference) (*v1alpha3.RemoteMCPServer, map[string]string, []corev1.EnvVar, []byte, error) {
	switch ref.Kind {
	case "RemoteMCPServer":
		server := &v1alpha3.RemoteMCPServer{}
		if err := c.kube.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, server); err != nil {
			return nil, nil, nil, nil, err
		}
		headers, environment, hashes, err := c.resolveAgentTemplateHeaders(ctx, namespace, server.Spec.HeadersFrom)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		server = server.DeepCopy()
		server.Spec.HeadersFrom = nil
		return server, headers, environment, hashes, nil
	default:
		return nil, nil, nil, nil, newValidationError("unsupported MCP server kind %q", ref.Kind)
	}
}

// resolveAgentTemplateHeaders keeps Secret values out of serialized agent
// config. The runtime expands __KAGENT_ENV[...]__ from the corresponding
// Secret-backed environment variable when it constructs the MCP request.
func (c *Compiler) resolveAgentTemplateHeaders(ctx context.Context, namespace string, refs []v1alpha3.ValueRef) (map[string]string, []corev1.EnvVar, []byte, error) {
	headers := make(map[string]string, len(refs))
	var environment []corev1.EnvVar
	var hashes []byte
	for _, ref := range refs {
		if ref.ValueFrom == nil || ref.ValueFrom.Type != v1alpha3.SecretValueSource {
			name, value, err := ref.Resolve(ctx, c.kube, namespace)
			if err != nil {
				return nil, nil, nil, err
			}
			headers[name] = value
			continue
		}
		selector := &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: ref.ValueFrom.Name}, Key: ref.ValueFrom.Key}
		hash, err := hashSecretKey(ctx, c.kube, namespace, selector)
		if err != nil {
			return nil, nil, nil, err
		}
		sum := sha256.Sum256([]byte(namespace + "\x00" + selector.Name + "\x00" + selector.Key))
		envName := "KAGENT_CREDENTIAL_" + strings.ToUpper(fmt.Sprintf("%x", sum[:8]))
		headers[ref.Name] = "__KAGENT_ENV[" + envName + "]__"
		environment = append(environment, corev1.EnvVar{Name: envName, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: selector}})
		hashes = append(hashes, hash...)
	}
	return headers, environment, hashes, nil
}

func (c *Compiler) resolveAgentTemplatePrompt(ctx context.Context, template *v1alpha3.AgentTemplate) (string, error) {
	if template.Spec.SystemPromptFrom != nil {
		ref := template.Spec.SystemPromptFrom
		value, err := utils.GetConfigMapValue(ctx, c.kube, types.NamespacedName{Namespace: template.Namespace, Name: ref.Name}, ref.Key)
		if err != nil {
			return "", fmt.Errorf("resolve systemPromptFrom: %w", err)
		}
		return value, nil
	}
	return template.Spec.SystemPrompt, nil
}

// hashSecretKey makes credential rotation part of revision identity without
// copying the credential into the database or source snapshot.
func hashSecretKey(ctx context.Context, kube client.Client, namespace string, selector *corev1.SecretKeySelector) ([]byte, error) {
	if selector == nil {
		return nil, fmt.Errorf("secretKeyRef is required")
	}
	secret := &corev1.Secret{}
	if err := kube.Get(ctx, types.NamespacedName{Namespace: namespace, Name: selector.Name}, secret); err != nil {
		return nil, err
	}
	value, ok := secret.Data[selector.Key]
	if !ok {
		if selector.Optional != nil && *selector.Optional {
			return nil, nil
		}
		return nil, fmt.Errorf("secret %q does not contain key %q", selector.Name, selector.Key)
	}
	sum := sha256.Sum256(value)
	return sum[:], nil
}

// agentTemplateCard describes the runtime-local A2A server. Substrate routes
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
		Capabilities:       a2atype.AgentCapabilities{Streaming: true},
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
