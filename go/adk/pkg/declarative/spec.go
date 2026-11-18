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
	// NodeCEL computes its output, routes and state changes with CEL
	// expressions instead of registered Go code.
	NodeCEL NodeType = "cel"
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
	// MCPServers declares remote MCP servers. An LLM's tools list may name
	// a server to offer its tools to the model, and a tool node may call one
	// of its tools with server and tool.
	MCPServers []MCPServerSpec `json:"mcp_servers,omitempty"`
}

// MCPServerSpec is a remote MCP server reached over streamable HTTP.
type MCPServerSpec struct {
	// Name is how agents and nodes refer to the server.
	Name string `json:"name"`
	// URL is the server endpoint. URLFrom reads it from the environment
	// instead; exactly one is set.
	URL     string      `json:"url,omitempty"`
	URLFrom *TextSource `json:"url_from,omitempty"`
	// Headers are sent with every request, for example credentials.
	Headers []HeaderSpec `json:"headers,omitempty"`
	// Tools, when set, is the allowlist of tool names the server exposes to
	// the document. Unset exposes every tool the server lists.
	Tools []string `json:"tools,omitempty"`
}

// HeaderSpec is one HTTP header of an MCP server. Exactly one of Value and
// ValueFrom is set; ValueFrom reads from the runtime environment.
type HeaderSpec struct {
	Name      string      `json:"name"`
	Value     string      `json:"value,omitempty"`
	ValueFrom *TextSource `json:"value_from,omitempty"`
}

// LLMSpec is the model configuration shared by LlmAgent agents and inline llm
// nodes. It is embedded, so its fields sit directly on the owner in YAML.
type LLMSpec struct {
	// Model names a registered model, or one the registry's resolver knows.
	Model string `json:"model,omitempty"`
	Mode  Mode   `json:"mode,omitempty"`
	// Instruction is the system prompt of the agent.
	Instruction string `json:"instruction,omitempty"`
	// InstructionFrom reads the system prompt from a ConfigMap, a Secret or
	// an environment variable instead. It excludes Instruction.
	InstructionFrom *TextSource `json:"instruction_from,omitempty"`
	// GlobalInstruction applies to the whole agent tree below the agent.
	GlobalInstruction string `json:"global_instruction,omitempty"`
	// GlobalInstructionFrom is the TextSource form of GlobalInstruction.
	GlobalInstructionFrom *TextSource `json:"global_instruction_from,omitempty"`
	// Tools names registered tools and toolsets.
	Tools []string `json:"tools,omitempty"`
	// OutputKey stores the final reply in session state under this key.
	OutputKey string `json:"output_key,omitempty"`
	// OutputSchema is a JSON Schema the reply must satisfy.
	OutputSchema    json.RawMessage `json:"output_schema,omitempty"`
	IncludeContents IncludeContents `json:"include_contents,omitempty"`
	// Callbacks accept or reject model calls and tool calls.
	Callbacks *CallbacksSpec `json:"callbacks,omitempty"`
}

// TextSource names where sourced text comes from. Exactly one field is set.
//
// ConfigMap and Secret references require an embedding resolver before the
// runtime starts. The kagent controller inlines ConfigMap prompt sources.
// Env is resolved by the runtime when the agent is built; MCP URL and header
// sources use Env only.
type TextSource struct {
	ConfigMap *KeyRef `json:"config_map,omitempty"`
	Secret    *KeyRef `json:"secret,omitempty"`
	Env       string  `json:"env,omitempty"`
}

// KeyRef selects one key of a same-namespace ConfigMap or Secret.
type KeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// HookAction is the declarative decision of a hook rule.
type HookAction string

const (
	// HookAccept lets the call run unchanged and skips the remaining rules.
	HookAccept HookAction = "accept"
	// HookReject blocks the call: a model call is answered with Message, a
	// tool call returns {"error": Message} to the model.
	HookReject HookAction = "reject"
)

// HookSpec is one rule of a callback point. Exactly one of Action and
// Function is set. Rules run in order until one accepts or rejects.
type HookSpec struct {
	// When is a CEL condition; the rule applies only when it is true. Empty
	// always applies.
	When string `json:"when,omitempty"`
	// Action decides declaratively.
	Action HookAction `json:"action,omitempty"`
	// Message explains a rejection. It is required with action reject.
	Message string `json:"message,omitempty"`
	// Function names a registered Hook that decides in Go.
	Function string `json:"function,omitempty"`
}

// CallbacksSpec lists the hook rules of each ADK callback point.
type CallbacksSpec struct {
	BeforeModel []HookSpec `json:"before_model,omitempty"`
	AfterModel  []HookSpec `json:"after_model,omitempty"`
	BeforeTool  []HookSpec `json:"before_tool,omitempty"`
	AfterTool   []HookSpec `json:"after_tool,omitempty"`
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
	// MaxConcurrency bounds how many nodes of the whole graph run at once,
	// across fan-out branches; further ready nodes wait. Zero is unlimited.
	// Nested workflow nodes and the items of a parallel_worker run inside
	// their parent node and are not counted separately.
	MaxConcurrency int `json:"max_concurrency,omitempty"`

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
	// Server makes a tool node call Tool on a declared MCP server, or on a
	// registered toolset, instead of a registered tool.
	Server string `json:"server,omitempty"`

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

	// CEL node fields. Vars are named values the other expressions read as
	// vars.<name>. Output defaults to the node input. Route evaluates to a
	// string or a list of strings; Routes is the rule-table alternative.
	// StateDelta writes each key's value to session state.
	Vars       []CELVar          `json:"vars,omitempty"`
	Output     string            `json:"output,omitempty"`
	Route      string            `json:"route,omitempty"`
	Routes     []CELRoute        `json:"routes,omitempty"`
	StateDelta map[string]string `json:"state_delta,omitempty"`

	// Inline llm node fields. OutputSchema also types the output of function
	// and dynamic nodes: the JSON Schema their result must satisfy.
	LLMSpec

	// Nested workflow fields. A nested workflow has its own START.
	Nodes []NodeSpec `json:"nodes,omitempty"`
	Edges []EdgeSpec `json:"edges,omitempty"`
}

// CELVar is a named value of a cel node. Vars are evaluated in order, so an
// expression may read the vars declared before it.
type CELVar struct {
	Name string `json:"name"`
	Expr string `json:"expr"`
}

// CELRoute is one rule of a cel node's route table. The first rule whose
// When is true selects its Route; an empty When always matches. When no rule
// matches, the DEFAULT edge is taken.
type CELRoute struct {
	When  string `json:"when,omitempty"`
	Route string `json:"route"`
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
	// When is a CEL predicate over `error` ({message, timeout, canceled})
	// that decides whether a failure is retried. Empty retries every error.
	When string `json:"when,omitempty"`
}
