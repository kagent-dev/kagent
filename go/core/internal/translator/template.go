package translator

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"text/template"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// PromptTemplateContext holds the v2 AgentTemplate values available to system
// prompt templates. It deliberately excludes legacy Agent fields and skills,
// which the current K3 adapter does not support.
type PromptTemplateContext struct {
	// AgentTemplateName is the metadata.name of the AgentTemplate.
	AgentTemplateName string
	// AgentTemplateNamespace is the metadata.namespace of the AgentTemplate.
	AgentTemplateNamespace string
	// Description is the AgentTemplate's spec.description.
	Description string
	// ToolNames contains tools selected from all configured MCP servers.
	ToolNames []string
}

func (c *Compiler) resolveAgentTemplatePrompt(_ context.Context, agentTemplate *v1alpha3.AgentTemplate) (string, error) {
	if agentTemplate.Spec.SystemPromptFrom != nil {
		ref := agentTemplate.Spec.SystemPromptFrom
		configMap := krt.FetchOne(c.ctx, c.collections.ConfigMaps, krt.FilterObjectName(types.NamespacedName{Namespace: agentTemplate.Namespace, Name: ref.Name}))
		if configMap == nil {
			return "", fmt.Errorf("resolve systemPromptFrom: ConfigMap %q not found", ref.Name)
		}
		value, found := (*configMap).Data[ref.Key]
		if !found {
			return "", fmt.Errorf("resolve systemPromptFrom: ConfigMap %q does not contain key %q", ref.Name, ref.Key)
		}
		return value, nil
	}
	return agentTemplate.Spec.SystemPrompt, nil
}

// promptSourceRef names one ConfigMap exposed to a prompt template. Alias
// changes only its template identifier, not the Kubernetes lookup name.
type promptSourceRef struct {
	Name  string
	Alias string
}

// resolvePromptSourceRefs flattens ConfigMap keys into "source/key" identifiers
// and rejects collisions before template execution.
func resolvePromptSourceRefs(ctx krt.HandlerContext, configMaps krt.Collection[*corev1.ConfigMap], namespace string, sources []promptSourceRef) (map[string]string, error) {
	lookup := make(map[string]string)
	for _, source := range sources {
		identifier := source.Name
		if source.Alias != "" {
			identifier = source.Alias
		}
		configMap := krt.FetchOne(ctx, configMaps, krt.FilterObjectName(types.NamespacedName{Namespace: namespace, Name: source.Name}))
		if configMap == nil {
			return nil, fmt.Errorf("resolve prompt source %q: ConfigMap not found", source.Name)
		}
		for key, value := range (*configMap).Data {
			lookupKey := identifier + "/" + key
			if _, exists := lookup[lookupKey]; exists {
				return nil, fmt.Errorf("duplicate prompt template identifier %q", lookupKey)
			}
			lookup[lookupKey] = value
		}
	}
	return lookup, nil
}

// executeSystemMessageTemplate exposes only the include helper and the small
// PromptTemplateContext; prompt templates cannot access Kubernetes.
func executeSystemMessageTemplate(raw string, lookup map[string]string, data PromptTemplateContext) (string, error) {
	functions := template.FuncMap{"include": func(path string) (string, error) {
		content, ok := lookup[path]
		if ok {
			return content, nil
		}
		available := make([]string, 0, len(lookup))
		for key := range lookup {
			available = append(available, key)
		}
		slices.Sort(available)
		return "", fmt.Errorf("prompt template %q not found, available: %v", path, available)
	}}
	parsed, err := template.New("systemMessage").Funcs(functions).Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse system message template: %w", err)
	}
	var output bytes.Buffer
	if err := parsed.Execute(&output, data); err != nil {
		return "", fmt.Errorf("execute system message template: %w", err)
	}
	return output.String(), nil
}
