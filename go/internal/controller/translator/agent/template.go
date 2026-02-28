package agent

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"text/template"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/adk"
	"k8s.io/apimachinery/pkg/types"
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

// resolvePromptSources fetches all data from the referenced ConfigMaps and builds
// a lookup map keyed by "identifier/key" where identifier is the alias (if set) or resource name.
func resolvePromptSources(ctx context.Context, kube client.Client, namespace string, sources []v1alpha2.PromptSource) (map[string]string, error) {
	lookup := make(map[string]string)

	for _, src := range sources {
		identifier := src.Name
		if src.Alias != "" {
			identifier = src.Alias
		}

		nn := types.NamespacedName{Namespace: namespace, Name: src.Name}

		data, err := utils.GetConfigMapData(ctx, kube, nn)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve prompt source %q: %w", src.Name, err)
		}

		for key, value := range data {
			lookupKey := identifier + "/" + key
			if _, exists := lookup[lookupKey]; exists {
				return nil, fmt.Errorf("duplicate prompt template identifier %q from prompt source %q (kind=%q, apiGroup=%q)", lookupKey, src.Name, src.Kind, src.ApiGroup)
			}
			lookup[lookupKey] = value
		}
	}

	return lookup, nil
}

// buildTemplateContext constructs the template context from an Agent resource and its
// already-translated AgentConfig. Tool names are extracted from the config rather than
// recomputed from the spec.
func buildTemplateContext(agent *v1alpha2.Agent, cfg *adk.AgentConfig) PromptTemplateContext {
	tplCtx := PromptTemplateContext{
		AgentName:      agent.Name,
		AgentNamespace: agent.Namespace,
		Description:    agent.Spec.Description,
	}

	// Collect tool names from the already-translated agent config.
	for _, t := range cfg.HttpTools {
		tplCtx.ToolNames = append(tplCtx.ToolNames, t.Tools...)
	}
	for _, t := range cfg.SseTools {
		tplCtx.ToolNames = append(tplCtx.ToolNames, t.Tools...)
	}

	// Collect skill names using the shared OCI/Git name helpers.
	if agent.Spec.Skills != nil {
		for _, ref := range agent.Spec.Skills.Refs {
			if name := ociSkillName(ref); name != "" {
				tplCtx.SkillNames = append(tplCtx.SkillNames, name)
			}
		}
		for _, gitRef := range agent.Spec.Skills.GitRefs {
			if name := gitSkillName(gitRef); name != "" {
				tplCtx.SkillNames = append(tplCtx.SkillNames, name)
			}
		}
	}

	return tplCtx
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
				slices.Sort(available)
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
