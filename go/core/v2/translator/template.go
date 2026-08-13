package translator

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"text/template"

	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// promptSourceRef names one ConfigMap exposed to a prompt template. Alias
// changes only its template identifier, not the Kubernetes lookup name.
type promptSourceRef struct {
	Name  string
	Alias string
}

// resolvePromptSourceRefs flattens ConfigMap keys into "source/key" identifiers
// and rejects collisions before template execution.
func resolvePromptSourceRefs(ctx context.Context, kube client.Client, namespace string, sources []promptSourceRef) (map[string]string, error) {
	lookup := make(map[string]string)
	for _, source := range sources {
		identifier := source.Name
		if source.Alias != "" {
			identifier = source.Alias
		}
		data, err := utils.GetConfigMapData(ctx, kube, types.NamespacedName{Namespace: namespace, Name: source.Name})
		if err != nil {
			return nil, fmt.Errorf("resolve prompt source %q: %w", source.Name, err)
		}
		for key, value := range data {
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
