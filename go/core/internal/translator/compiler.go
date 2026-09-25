package translator

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"istio.io/istio/pkg/kube/krt"
	"k8s.io/apimachinery/pkg/types"
)

// Compiler resolves public API objects into a complete, immutable runtime
// revision. It owns the v2 translation boundary rather than delegating to an
// earlier API translator.
type Compiler struct {
	ctx              krt.HandlerContext
	collections      Collections
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

// HarnessCompiler converts resolved, harness-neutral inputs into one runtime
// revision and its user-facing diagnostics.
type HarnessCompiler interface {
	Compile(context.Context, *HarnessInput) (*CompileResult, error)
}

// resolvedTemplateTree is the validated AgentTemplate topology for one Harness.
type resolvedTemplateTree struct {
	Harness *HarnessConfiguration
	Root    *resolvedTemplate
}

// resolvedTemplate is one template and its validated Shared children.
type resolvedTemplate struct {
	Template *TemplateConfiguration
	Shared   []resolvedTemplateBinding
}

// resolvedTemplateBinding preserves the parent-specific identity of a Shared child.
type resolvedTemplateBinding struct {
	Name        string
	Description string
	Agent       *resolvedTemplate
}

// HarnessInput contains the Kubernetes inputs needed by a harness compiler.
type HarnessInput struct {
	// AgentName is the runnable identity, independent of inline or referenced inputs.
	AgentName    string
	Harness      *HarnessConfiguration
	Root         *AgentInput
	OutputSchema *ResolvedOutputSchema
}

// AgentInput contains resolved Kubernetes inputs for one agent.
type AgentInput struct {
	Template            *TemplateConfiguration
	ResolvedModelConfig *ResolvedModelConfig
	Instruction         string
	MCPTools            []ResolvedMCPTool
	Shared              []AgentInputBinding
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
func NewCompiler(ctx krt.HandlerContext, collections Collections, harnessCompilers map[HarnessType]HarnessCompiler) *Compiler {
	return &Compiler{ctx: ctx, collections: collections, harnessCompilers: maps.Clone(harnessCompilers)}
}

// CompileAgent resolves either inline or referenced configuration through the
// same compiler. Inline values are in-memory inputs, never Kubernetes objects.
func (c *Compiler) CompileAgent(ctx context.Context, agent *v1alpha3.Agent) (*CompileResult, error) {
	if (agent.Spec.Template == nil) == (agent.Spec.TemplateRef == nil) ||
		(agent.Spec.Harness == nil) == (agent.Spec.HarnessRef == nil) {
		return nil, NewValidationError("Agent requires exactly one of template/templateRef and harness/harnessRef")
	}
	var template *TemplateConfiguration
	if agent.Spec.Template != nil {
		template = &TemplateConfiguration{Name: agent.Name, Namespace: agent.Namespace, Spec: *agent.Spec.Template.DeepCopy()}
	} else {
		key := types.NamespacedName{Namespace: agent.Namespace, Name: agent.Spec.TemplateRef.Name}
		found := krt.FetchOne(c.ctx, c.collections.AgentTemplates, krt.FilterObjectName(key))
		if found == nil {
			return nil, fmt.Errorf("resolve AgentTemplate %q: not found", key)
		}
		template = templateConfiguration(*found)
	}
	var harness *HarnessConfiguration
	if agent.Spec.Harness != nil {
		harness = &HarnessConfiguration{Name: agent.Name, Namespace: agent.Namespace, Spec: *agent.Spec.Harness.DeepCopy()}
	} else {
		key := types.NamespacedName{Namespace: agent.Namespace, Name: agent.Spec.HarnessRef.Name}
		found := krt.FetchOne(c.ctx, c.collections.Harnesses, krt.FilterObjectName(key))
		if found == nil {
			return nil, fmt.Errorf("resolve Harness %q: not found", key)
		}
		harness = harnessConfiguration(*found)
	}
	result, err := c.compileConfiguration(ctx, agent.Name, harness, template)
	if err != nil {
		return nil, err
	}
	result.AgentUID = string(agent.UID)
	result.Provenance, err = json.Marshal(struct {
		AgentName string             `json:"agentName"`
		AgentUID  string             `json:"agentUID"`
		Spec      v1alpha3.AgentSpec `json:"spec"`
		Inputs    json.RawMessage    `json:"inputs"`
	}{agent.Name, string(agent.UID), agent.Spec, result.Provenance})
	if err != nil {
		return nil, fmt.Errorf("encode Agent provenance: %w", err)
	}
	return result, nil
}

// compileConfiguration compiles resolved configuration for the named Agent.
// The Agent name owns runtime identity; template and Harness names are provenance.
func (c *Compiler) compileConfiguration(ctx context.Context, agentName string, harness *HarnessConfiguration, template *TemplateConfiguration) (*CompileResult, error) {
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
	input.AgentName = agentName
	result, err := harnessCompiler.Compile(ctx, input)
	if err != nil {
		return nil, err
	}
	workerKey := types.NamespacedName{Namespace: harness.Namespace, Name: harness.Spec.Substrate.WorkerPoolRef.Name}
	workerPool := krt.FetchOne(c.ctx, c.collections.WorkerPools, krt.FilterObjectName(workerKey))
	if workerPool == nil {
		return nil, &WorkerPoolNotFoundError{WorkerPool: workerKey}
	}
	result.AgentName = agentName
	result.SandboxClass = (*workerPool).Spec.SandboxClass
	return result, nil
}

func harnessType(harness *HarnessConfiguration) HarnessType {
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

func (c *Compiler) resolveTree(ctx context.Context, harness *HarnessConfiguration, root *TemplateConfiguration) (*resolvedTemplateTree, error) {
	seen, path, names := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	var resolve func(*TemplateConfiguration, bool) (*resolvedTemplate, error)
	resolve = func(template *TemplateConfiguration, child bool) (*resolvedTemplate, error) {
		if template.Namespace != harness.Namespace {
			return nil, NewValidationError("Harness and AgentTemplate must be in the same namespace")
		}
		// Inline roots are not references and cannot participate in a resource cycle.
		if template.Source != nil {
			if _, ok := path[template.Name]; ok {
				return nil, NewValidationError("AgentTemplate tool cycle includes %q", template.Name)
			}
			if _, ok := seen[template.Name]; ok {
				return nil, NewValidationError("AgentTemplate %q is referenced more than once in the Shared tree", template.Name)
			}
			seen[template.Name], path[template.Name] = struct{}{}, struct{}{}
			defer delete(path, template.Name)
		}

		resolved := &resolvedTemplate{Template: template}
		for _, tool := range template.Spec.Tools {
			if tool.SubAgent == nil {
				continue
			}
			binding := tool.SubAgent
			if binding.Isolation == v1alpha3.SubAgentToolIsolationDedicated {
				return nil, NewValidationError("Dedicated AgentTemplate tools are not supported yet")
			}
			if _, ok := names[binding.Name]; ok {
				return nil, NewValidationError("duplicate Shared AgentTemplate binding name %q", binding.Name)
			}
			names[binding.Name] = struct{}{}
			key := types.NamespacedName{Namespace: template.Namespace, Name: binding.TemplateRef.Name}
			childTemplate := krt.FetchOne(c.ctx, c.collections.AgentTemplates, krt.FilterObjectName(key))
			if childTemplate == nil {
				return nil, fmt.Errorf("resolve AgentTemplate %q: not found", binding.TemplateRef.Name)
			}
			agent, err := resolve(templateConfiguration(*childTemplate), true)
			if err != nil {
				return nil, err
			}
			if child {
				return nil, NewValidationError("consecutive Shared AgentTemplate tools exceed the kagent runtime boundary")
			}
			resolved.Shared = append(resolved.Shared, resolvedTemplateBinding{Name: binding.Name, Description: binding.Description, Agent: agent})
		}
		return resolved, nil
	}

	resolved, err := resolve(root, false)
	if err != nil {
		return nil, err
	}
	return &resolvedTemplateTree{Harness: harness, Root: resolved}, nil
}

func (c *Compiler) buildInputs(ctx context.Context, tree *resolvedTemplateTree) (*HarnessInput, error) {
	outputSchema, err := c.resolveOutputSchema(ctx, tree.Root.Template)
	if err != nil {
		return nil, err
	}
	if outputSchema != nil && harnessType(tree.Harness) != HarnessTypeKagent {
		return nil, NewValidationError("Harness runtime %q does not support structured output", harnessType(tree.Harness))
	}
	var build func(*resolvedTemplate) (*AgentInput, error)
	build = func(agent *resolvedTemplate) (*AgentInput, error) {
		template := agent.Template
		instruction, err := c.resolveAgentTemplatePrompt(ctx, template)
		if err != nil {
			return nil, err
		}
		input := &AgentInput{Template: template, Instruction: instruction}
		if template.Spec.ModelConfig != nil {
			input.ResolvedModelConfig = krt.FetchOne(c.ctx, c.collections.ResolvedModelConfigs, krt.FilterObjectName(types.NamespacedName{Namespace: template.Namespace, Name: template.Spec.ModelConfig.Name}))
			if input.ResolvedModelConfig == nil {
				return nil, fmt.Errorf("resolve ModelConfig %q: not found", template.Spec.ModelConfig.Name)
			}
			if failures := input.ResolvedModelConfig.SemanticFailures; len(failures) > 0 {
				return nil, NewValidationError("ModelConfig %q: %s", template.Spec.ModelConfig.Name, failures[0].Message)
			}
			if failures := input.ResolvedModelConfig.ReferenceFailures; len(failures) > 0 {
				return nil, fmt.Errorf("resolve ModelConfig %q: %s", template.Spec.ModelConfig.Name, failures[0].Message)
			}
		}
		toolNames := make([]string, 0)
		for _, tool := range template.Spec.Tools {
			if tool.MCP == nil {
				if tool.SubAgent == nil {
					return nil, NewValidationError("tool binding must select an MCP server or AgentTemplate")
				}
				continue
			}
			if tool.MCP.Server.Kind != "RemoteMCPServer" {
				return nil, NewValidationError("unsupported MCP server kind %q", tool.MCP.Server.Kind)
			}
			key := types.NamespacedName{Namespace: template.Namespace, Name: tool.MCP.Server.Name}
			server := krt.FetchOne(c.ctx, c.collections.RemoteMCPServers, krt.FilterObjectName(key))
			if server == nil {
				return nil, fmt.Errorf("resolve %s %q: not found", tool.MCP.Server.Kind, tool.MCP.Server.Name)
			}
			input.MCPTools = append(input.MCPTools, ResolvedMCPTool{Binding: *tool.MCP.DeepCopy(), Server: *server})
			toolNames = append(toolNames, tool.MCP.Tools...)
		}
		if template.Spec.PromptTemplate != nil {
			refs := make([]promptSourceRef, 0, len(template.Spec.PromptTemplate.DataSources))
			for _, source := range template.Spec.PromptTemplate.DataSources {
				refs = append(refs, promptSourceRef{Name: source.Name, Alias: source.Alias})
			}
			lookup, err := resolvePromptSourceRefs(c.ctx, c.collections.ConfigMaps, template.Namespace, refs)
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
	return &HarnessInput{Harness: tree.Harness, Root: root, OutputSchema: outputSchema}, nil
}
