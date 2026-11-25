package declarative

import "encoding/json"

// Document identity. The version is experimental: documents carry no
// compatibility promise until the language graduates.
const (
	APIVersion      = "adk.declarative/v0alpha1"
	KindAgentSystem = "AgentSystem"
)

// AgentClass selects how an agent is built.
type AgentClass string

const (
	// ClassWorkflow is a graph of nodes and edges executed by the ADK
	// workflow engine.
	ClassWorkflow AgentClass = "Workflow"
	// ClassLlmAgent is a model-backed agent.
	ClassLlmAgent AgentClass = "LlmAgent"
)

// NodeType selects how a workflow node is built.
type NodeType string

const (
	NodeFunction NodeType = "function"
	NodeJoin     NodeType = "join"
	NodeTool     NodeType = "tool"
	NodeAgent    NodeType = "agent"
	NodeLLM      NodeType = "llm"
	NodeWorkflow NodeType = "workflow"
)

// Reserved edge endpoints and routes.
const (
	// Start is the edge source that seeds a workflow with the user input.
	Start = "START"
	// RouteDefault marks the edge taken when no other route of the source
	// node matched.
	RouteDefault = "DEFAULT"
)

// Mode is the delegation mode of an LlmAgent.
type Mode string

const (
	ModeChat       Mode = "chat"
	ModeTask       Mode = "task"
	ModeSingleTurn Mode = "single_turn"
)

// IncludeContents controls how much conversation history a model sees.
type IncludeContents string

const (
	// IncludeContentsDefault sends the history that the ADK selects for the
	// agent's placement.
	IncludeContentsDefault IncludeContents = "default"
	// IncludeContentsNone sends only the current turn.
	IncludeContentsNone IncludeContents = "none"
)

// Spec is an AgentSystem document. The first agent is the root.
type Spec struct {
	APIVersion string      `json:"apiVersion"`
	Kind       string      `json:"kind"`
	Agents     []AgentSpec `json:"agents"`
}

// LLMSpec is the model configuration shared by LlmAgent agents and inline llm
// nodes. It is embedded, so its fields sit directly on the owner in YAML.
type LLMSpec struct {
	// Model is empty to use the registry's model. The only accepted value is
	// StubModel, the built-in offline model for tests and examples.
	Model string `json:"model,omitempty"`
	Mode  Mode   `json:"mode,omitempty"`
	// Instruction is the system prompt of the agent.
	Instruction string `json:"instruction,omitempty"`
	// GlobalInstruction applies to the whole agent tree below the agent.
	GlobalInstruction string `json:"global_instruction,omitempty"`
	// Tools names registered tools.
	Tools []string `json:"tools,omitempty"`
	// OutputKey stores the final reply in session state under this key.
	OutputKey string `json:"output_key,omitempty"`
	// OutputSchema is a JSON Schema the reply must satisfy.
	OutputSchema    json.RawMessage `json:"output_schema,omitempty"`
	IncludeContents IncludeContents `json:"include_contents,omitempty"`
}

// AgentSpec declares one named agent. Which fields apply depends on
// AgentClass; see agentRules.
type AgentSpec struct {
	Name        string     `json:"name"`
	AgentClass  AgentClass `json:"agent_class"`
	Description string     `json:"description,omitempty"`
	// SubAgents names other agents of the document. For a Workflow they are
	// made available to the engine; for an LlmAgent they are transfer
	// targets.
	SubAgents []string `json:"sub_agents,omitempty"`

	// Workflow fields.

	// Entry is shorthand for a START edge to the named node. It may repeat
	// an explicit START edge.
	Entry string     `json:"entry,omitempty"`
	Nodes []NodeSpec `json:"nodes,omitempty"`
	Edges []EdgeSpec `json:"edges,omitempty"`

	// LlmAgent fields.
	LLMSpec
	DisallowTransferToParent bool `json:"disallow_transfer_to_parent,omitempty"`
	DisallowTransferToPeers  bool `json:"disallow_transfer_to_peers,omitempty"`
}

// NodeSpec declares one workflow node. Which fields apply depends on Type;
// see nodeRules.
type NodeSpec struct {
	ID          string   `json:"id"`
	Type        NodeType `json:"type"`
	Description string   `json:"description,omitempty"`

	// Retry and Timeout apply to every node type that runs user code.
	Retry   *RetrySpec `json:"retry,omitempty"`
	Timeout string     `json:"timeout,omitempty"`

	// Function, Tool and Agent name the registered behavior a node runs.
	Function string `json:"function,omitempty"`
	Tool     string `json:"tool,omitempty"`
	Agent    string `json:"agent,omitempty"`

	// InputSchema is a JSON Schema that the node's input must satisfy. On a
	// join it applies to each predecessor's output separately.
	InputSchema json.RawMessage `json:"input_schema,omitempty"`

	// Inline llm node fields. OutputSchema also types the output of function
	// nodes: the JSON Schema their result must satisfy.
	LLMSpec

	// Nested workflow fields. A nested workflow has its own START.
	Nodes []NodeSpec `json:"nodes,omitempty"`
	Edges []EdgeSpec `json:"edges,omitempty"`
}

// EdgeSpec connects two nodes. An empty Route is unconditional.
type EdgeSpec struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Route string `json:"route,omitempty"`
}

// RetrySpec mirrors the ADK retry policy. Unset fields take the ADK defaults,
// and every error except an input validation error is retried.
type RetrySpec struct {
	MaxAttempts   int      `json:"max_attempts,omitempty"`
	InitialDelay  string   `json:"initial_delay,omitempty"`
	MaxDelay      string   `json:"max_delay,omitempty"`
	BackoffFactor *float64 `json:"backoff_factor,omitempty"`
	Jitter        *float64 `json:"jitter,omitempty"`
}
