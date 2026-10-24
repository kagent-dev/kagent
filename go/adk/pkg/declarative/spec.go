package declarative

import "encoding/json"

// Document identity.
const (
	APIVersion      = "adk.declarative/v0.2"
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
	// ClassSequential runs its sub-agents one after another.
	ClassSequential AgentClass = "SequentialAgent"
	// ClassParallel runs its sub-agents concurrently in isolated branches.
	ClassParallel AgentClass = "ParallelAgent"
	// ClassLoop runs its sub-agents repeatedly until max_iterations or until a
	// sub-agent escalates.
	ClassLoop AgentClass = "LoopAgent"
)

// NodeType selects how a workflow node is built.
type NodeType string

const (
	NodeFunction       NodeType = "function"
	NodeJoin           NodeType = "join"
	NodeTool           NodeType = "tool"
	NodeAgent          NodeType = "agent"
	NodeLLM            NodeType = "llm"
	NodeWorkflow       NodeType = "workflow"
	NodeParallelWorker NodeType = "parallel_worker"
	NodeDynamic        NodeType = "dynamic"
	NodeHITL           NodeType = "hitl"
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

// Isolation says how much of the session a child run can see. Each level
// hides more than the one before it.
type Isolation string

const (
	// IsolationShared runs the child on its parent's branch: it sees the
	// whole conversation, including what earlier children said. This is the
	// ADK default for children of a dynamic node.
	IsolationShared Isolation = "shared"
	// IsolationBranch gives every child run its own branch: it sees its
	// ancestors' history but not its siblings'. Static fan-out, parallel
	// workers and ParallelAgent always run this way.
	IsolationBranch Isolation = "branch"
	// IsolationScope also tags the child's events with a scope unique to the
	// run, so it sees only its own events.
	IsolationScope Isolation = "scope"
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
	// Model names a registered model, or one the registry's resolver knows.
	Model string `json:"model,omitempty"`
	Mode  Mode   `json:"mode,omitempty"`
	// Instruction is the system prompt of the agent.
	Instruction string `json:"instruction,omitempty"`
	// GlobalInstruction applies to the whole agent tree below the agent.
	GlobalInstruction string `json:"global_instruction,omitempty"`
	// Tools names registered tools and toolsets.
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
	// targets; for Sequential, Parallel and Loop agents they are the agents
	// being orchestrated.
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

	// AgentTools names agents that this LlmAgent calls as tools. Every call
	// runs in a new session seeded with a copy of the caller's state, so the
	// agent never sees the caller's history and parallel calls cannot
	// interfere. An agent is either a sub_agent (shared session, transfer) or
	// an agent tool (separate session), not both.
	AgentTools []string `json:"agent_tools,omitempty"`

	// MaxIterations bounds a LoopAgent. Zero loops until a sub-agent
	// escalates.
	MaxIterations uint `json:"max_iterations,omitempty"`
}

// NodeSpec declares one workflow node. Which fields apply depends on Type;
// see builtinNodeRules.
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

	// MaxConcurrency bounds a parallel_worker. Zero is unlimited.
	MaxConcurrency int `json:"max_concurrency,omitempty"`
	// MaxIters bounds the child runs of a dynamic node.
	MaxIters int `json:"max_iters,omitempty"`
	// Children names nodes of the same graph that a dynamic node may run by
	// id. A child has no edges: only its dynamic parent schedules it.
	Children []string `json:"children,omitempty"`
	// Isolation sets how much of the session each child run of a dynamic
	// node sees. Unset is shared.
	Isolation Isolation `json:"isolation,omitempty"`
	// InputSchema is a JSON Schema that the node's input must satisfy. On a
	// join it applies to each predecessor's output separately.
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
	// Message is the prompt a hitl node shows the human.
	Message string `json:"message,omitempty"`

	// Inline llm node fields. OutputSchema also types the output of function
	// and dynamic nodes: the JSON Schema their result must satisfy.
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

// RetrySpec mirrors the ADK retry policy. Unset fields take the ADK defaults.
type RetrySpec struct {
	MaxAttempts   int      `json:"max_attempts,omitempty"`
	InitialDelay  string   `json:"initial_delay,omitempty"`
	MaxDelay      string   `json:"max_delay,omitempty"`
	BackoffFactor *float64 `json:"backoff_factor,omitempty"`
	Jitter        *float64 `json:"jitter,omitempty"`
}
