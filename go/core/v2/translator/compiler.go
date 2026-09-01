package translator

import (
	"context"
	"fmt"
	"maps"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

// Compiler resolves public API objects into a complete, immutable runtime
// revision. It owns the v2 translation boundary rather than delegating to an
// earlier API translator.
type Compiler struct {
	kube             Reader
	harnessCompilers map[HarnessType]HarnessCompiler
}

// HarnessType identifies the runtime selected by a Harness.
type HarnessType string

// Supported Harness runtime types.
const (
	HarnessTypeKagent HarnessType = "kagent"
	HarnessTypeCodex  HarnessType = "codex"
	HarnessTypeClaude HarnessType = "claude"
	HarnessTypeBYO    HarnessType = "byo"
)

// HarnessCompiler converts resolved, harness-neutral inputs into one runtime revision.
type HarnessCompiler interface {
	Compile(context.Context, *HarnessInput) (*Revision, error)
}

// ResolvedTree is the validated AgentTemplate topology for one Harness.
type ResolvedTree struct {
	Harness *v1alpha3.Harness
	Root    *ResolvedAgent
}

// ResolvedAgent is one template and its validated Shared children.
type ResolvedAgent struct {
	Template *v1alpha3.AgentTemplate
	Shared   []ResolvedAgentBinding
}

// ResolvedAgentBinding preserves the parent-specific identity of a Shared child.
type ResolvedAgentBinding struct {
	Name        string
	Description string
	Agent       *ResolvedAgent
}

// HarnessInput contains the Kubernetes inputs needed by a harness compiler.
type HarnessInput struct {
	Harness *v1alpha3.Harness
	Root    *AgentInput
}

// AgentInput contains resolved Kubernetes inputs for one agent.
type AgentInput struct {
	Template    *v1alpha3.AgentTemplate
	ModelConfig *v1alpha3.ModelConfig
	Instruction string
	MCPTools    []ResolvedMCPTool
	Shared      []AgentInputBinding
}

// ResolvedMCPTool pairs an exact tool allowlist with its resolved server.
type ResolvedMCPTool struct {
	Binding v1alpha3.MCPToolBinding
	Server  *v1alpha3.RemoteMCPServer
}

// AgentInputBinding preserves the parent-specific identity of a compiled child.
type AgentInputBinding struct {
	Name        string
	Description string
	Agent       *AgentInput
}

// NewCompiler constructs the v2 runtime compiler.
func NewCompiler(kube Reader, harnessCompilers map[HarnessType]HarnessCompiler) *Compiler {
	return &Compiler{kube: kube, harnessCompilers: maps.Clone(harnessCompilers)}
}

// CompileAgentTemplate resolves an API v2 attachment into an immutable runtime
// revision. Nothing below this boundary needs to read the public API objects.
func (c *Compiler) CompileAgentTemplate(ctx context.Context, harness *v1alpha3.Harness, template *v1alpha3.AgentTemplate) (*Revision, error) {
	harnessCompiler := c.harnessCompilers[harnessType(harness)]
	if harnessCompiler == nil {
		return nil, NewValidationError("Harness runtime is not supported by any compiler")
	}
	tree, err := c.resolveTree(ctx, harness, template)
	if err != nil {
		return nil, err
	}
	input, err := c.buildInputs(ctx, tree)
	if err != nil {
		return nil, err
	}
	return harnessCompiler.Compile(ctx, input)
}

func harnessType(harness *v1alpha3.Harness) HarnessType {
	switch {
	case harness.Spec.Kagent != nil:
		return HarnessTypeKagent
	case harness.Spec.Codex != nil:
		return HarnessTypeCodex
	case harness.Spec.Claude != nil:
		return HarnessTypeClaude
	case harness.Spec.BYO != nil:
		return HarnessTypeBYO
	default:
		return ""
	}
}

func (c *Compiler) resolveTree(ctx context.Context, harness *v1alpha3.Harness, root *v1alpha3.AgentTemplate) (*ResolvedTree, error) {
	selector, err := harnessSelector(harness)
	if err != nil {
		return nil, err
	}
	seen, path, names := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	var resolve func(*v1alpha3.AgentTemplate, bool) (*ResolvedAgent, error)
	resolve = func(template *v1alpha3.AgentTemplate, child bool) (*ResolvedAgent, error) {
		if template.Namespace != harness.Namespace {
			return nil, NewValidationError("Harness and AgentTemplate must be in the same namespace")
		}
		if !selector.Matches(labels.Set(template.Labels)) {
			return nil, NewValidationError("AgentTemplate %q is not admitted by Harness %q", template.Name, harness.Name)
		}
		if _, ok := path[template.Name]; ok {
			return nil, NewValidationError("AgentTemplate tool cycle includes %q", template.Name)
		}
		if _, ok := seen[template.Name]; ok {
			return nil, NewValidationError("AgentTemplate %q is referenced more than once in the Shared tree", template.Name)
		}
		seen[template.Name], path[template.Name] = struct{}{}, struct{}{}
		defer delete(path, template.Name)

		resolved := &ResolvedAgent{Template: template.DeepCopy()}
		for _, tool := range template.Spec.Tools {
			if tool.Agent == nil {
				continue
			}
			binding := tool.Agent
			if binding.Isolation == v1alpha3.AgentToolIsolationDedicated {
				return nil, NewValidationError("Dedicated AgentTemplate tools are not supported yet")
			}
			if _, ok := names[binding.Name]; ok {
				return nil, NewValidationError("duplicate Shared AgentTemplate binding name %q", binding.Name)
			}
			names[binding.Name] = struct{}{}
			childTemplate := &v1alpha3.AgentTemplate{}
			key := types.NamespacedName{Namespace: template.Namespace, Name: binding.TemplateRef.Name}
			if err := c.kube.Get(ctx, key, childTemplate); err != nil {
				return nil, fmt.Errorf("resolve AgentTemplate %q: %w", binding.TemplateRef.Name, err)
			}
			agent, err := resolve(childTemplate, true)
			if err != nil {
				return nil, err
			}
			if child {
				return nil, NewValidationError("consecutive Shared AgentTemplate tools exceed the kagent runtime boundary")
			}
			resolved.Shared = append(resolved.Shared, ResolvedAgentBinding{Name: binding.Name, Description: binding.Description, Agent: agent})
		}
		return resolved, nil
	}

	resolved, err := resolve(root, false)
	if err != nil {
		return nil, err
	}
	return &ResolvedTree{Harness: harness.DeepCopy(), Root: resolved}, nil
}

func harnessSelector(harness *v1alpha3.Harness) (labels.Selector, error) {
	if harness.Spec.AllowedAgentTemplates == nil {
		return labels.Nothing(), NewValidationError("Harness %q admits no AgentTemplates", harness.Name)
	}
	selector, err := metav1.LabelSelectorAsSelector(&harness.Spec.AllowedAgentTemplates.Selector)
	if err != nil {
		return nil, NewValidationError("Harness %q has an invalid AgentTemplate selector: %v", harness.Name, err)
	}
	return selector, nil
}

func (c *Compiler) buildInputs(ctx context.Context, tree *ResolvedTree) (*HarnessInput, error) {
	var build func(*ResolvedAgent) (*AgentInput, error)
	build = func(agent *ResolvedAgent) (*AgentInput, error) {
		template := agent.Template
		var model *v1alpha3.ModelConfig
		if template.Spec.ModelConfig != nil {
			model = &v1alpha3.ModelConfig{}
			if err := c.kube.Get(ctx, types.NamespacedName{Namespace: template.Namespace, Name: template.Spec.ModelConfig.Name}, model); err != nil {
				return nil, fmt.Errorf("resolve ModelConfig %q: %w", template.Spec.ModelConfig.Name, err)
			}
		}
		instruction, err := c.resolveAgentTemplatePrompt(ctx, template)
		if err != nil {
			return nil, err
		}
		input := &AgentInput{Template: template, ModelConfig: model, Instruction: instruction}
		toolNames := make([]string, 0)
		for _, tool := range template.Spec.Tools {
			if tool.MCP == nil {
				if tool.Agent == nil {
					return nil, NewValidationError("tool binding must select an MCP server or AgentTemplate")
				}
				continue
			}
			if tool.MCP.Server.Kind != "RemoteMCPServer" {
				return nil, NewValidationError("unsupported MCP server kind %q", tool.MCP.Server.Kind)
			}
			server := &v1alpha3.RemoteMCPServer{}
			key := types.NamespacedName{Namespace: template.Namespace, Name: tool.MCP.Server.Name}
			if err := c.kube.Get(ctx, key, server); err != nil {
				return nil, fmt.Errorf("resolve %s %q: %w", tool.MCP.Server.Kind, tool.MCP.Server.Name, err)
			}
			input.MCPTools = append(input.MCPTools, ResolvedMCPTool{Binding: *tool.MCP.DeepCopy(), Server: server})
			toolNames = append(toolNames, tool.MCP.Tools...)
		}
		if template.Spec.PromptTemplate != nil {
			refs := make([]promptSourceRef, 0, len(template.Spec.PromptTemplate.DataSources))
			for _, source := range template.Spec.PromptTemplate.DataSources {
				refs = append(refs, promptSourceRef{Name: source.Name, Alias: source.Alias})
			}
			lookup, err := resolvePromptSourceRefs(ctx, c.kube, template.Namespace, refs)
			if err != nil {
				return nil, fmt.Errorf("resolve prompt sources: %w", err)
			}
			input.Instruction, err = executeSystemMessageTemplate(input.Instruction, lookup, PromptTemplateContext{
				AgentTemplateName: template.Name, AgentTemplateNamespace: template.Namespace,
				Description: template.Spec.Description, ToolNames: toolNames,
			})
			if err != nil {
				return nil, err
			}
		}
		for _, binding := range agent.Shared {
			child, err := build(binding.Agent)
			if err != nil {
				return nil, err
			}
			input.Shared = append(input.Shared, AgentInputBinding{Name: binding.Name, Description: binding.Description, Agent: child})
		}
		return input, nil
	}

	root, err := build(tree.Root)
	if err != nil {
		return nil, err
	}
	return &HarnessInput{Harness: tree.Harness, Root: root}, nil
}
