package agent

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"text/template"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PromptTemplateContext holds the variables available to system message templates.
type PromptTemplateContext struct {
	// AgentName is the metadata.name of the Agent resource.
	AgentName string
	// AgentNamespace is the metadata.namespace of the Agent resource.
	AgentNamespace string
	// Description is the spec.description of the Agent resource.
	Description string
	// ToolNames is the list of tool names from all MCP server tools configured on the agent.
	ToolNames []string
	// SkillNames is the list of skill identifiers configured on the agent.
	SkillNames []string
}

// resolvePromptSources fetches all data from the referenced ConfigMaps/Secrets and builds
// a lookup map keyed by "identifier/key" where identifier is the alias (if set) or resource name.
func resolvePromptSources(ctx context.Context, kube client.Client, namespace string, sources []v1alpha2.PromptSource) (map[string]string, error) {
	lookup := make(map[string]string)

	for _, src := range sources {
		identifier := src.Name
		if src.Alias != "" {
			identifier = src.Alias
		}

		nn := src.NamespacedName(namespace)

		var data map[string]string
		var err error

		switch src.Kind {
		case "ConfigMap":
			data, err = utils.GetConfigMapData(ctx, kube, nn)
		case "Secret":
			data, err = utils.GetSecretData(ctx, kube, nn)
		default:
			return nil, fmt.Errorf("unsupported prompt source kind %q (apiGroup=%q) for %q", src.Kind, src.ApiGroup, src.Name)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to resolve prompt source %q: %w", src.Name, err)
		}

		for key, value := range data {
			lookupKey := identifier + "/" + key
			lookup[lookupKey] = value
		}
	}

	return lookup, nil
}

// buildTemplateContext constructs the template context from an Agent resource.
func buildTemplateContext(agent *v1alpha2.Agent) PromptTemplateContext {
	ctx := PromptTemplateContext{
		AgentName:      agent.Name,
		AgentNamespace: agent.Namespace,
		Description:    agent.Spec.Description,
	}

	// Collect tool names from all MCP server tools.
	if agent.Spec.Declarative != nil {
		for _, tool := range agent.Spec.Declarative.Tools {
			if tool.McpServer != nil {
				ctx.ToolNames = append(ctx.ToolNames, tool.McpServer.ToolNames...)
			}
		}
	}

	// Collect skill names from OCI refs and git refs.
	if agent.Spec.Skills != nil {
		for _, ref := range agent.Spec.Skills.Refs {
			// Use the last segment of the OCI reference as the skill name.
			parts := strings.Split(ref, "/")
			name := parts[len(parts)-1]
			// Strip tag if present (e.g., "image:v1" -> "image").
			if idx := strings.Index(name, ":"); idx != -1 {
				name = name[:idx]
			}
			ctx.SkillNames = append(ctx.SkillNames, name)
		}
		for _, gitRef := range agent.Spec.Skills.GitRefs {
			if gitRef.Name != "" {
				ctx.SkillNames = append(ctx.SkillNames, gitRef.Name)
			} else {
				// Fall back to repo URL last segment.
				parts := strings.Split(strings.TrimSuffix(gitRef.URL, ".git"), "/")
				ctx.SkillNames = append(ctx.SkillNames, parts[len(parts)-1])
			}
		}
	}

	return ctx
}

// executeSystemMessageTemplate parses and executes the system message as a Go text/template.
// The include function resolves "source/key" paths from the provided lookup map.
// Included content is treated as plain text (no nested template execution).
func executeSystemMessageTemplate(rawMessage string, lookup map[string]string, tplCtx PromptTemplateContext) (string, error) {
	funcMap := template.FuncMap{
		"include": func(path string) (string, error) {
			content, ok := lookup[path]
			if !ok {
				available := make([]string, 0, len(lookup))
				for k := range lookup {
					available = append(available, k)
				}
				sort.Strings(available)
				return "", fmt.Errorf("prompt template %q not found in promptSources, available: %v", path, available)
			}
			return content, nil
		},
	}

	tmpl, err := template.New("systemMessage").Funcs(funcMap).Parse(rawMessage)
	if err != nil {
		return "", fmt.Errorf("failed to parse system message template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, tplCtx); err != nil {
		return "", fmt.Errorf("failed to execute system message template: %w", err)
	}

	return buf.String(), nil
}
