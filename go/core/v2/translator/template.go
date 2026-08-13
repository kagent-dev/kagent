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

type promptSourceRef struct {
	Name  string
	Alias string
}

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

func executeSystemMessageTemplate(raw string, lookup map[string]string, data agentTemplatePromptContext) (string, error) {
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
