// Package declarative compiles an AgentSystem document into native Google ADK
// Go agents.
//
// # Stability
//
// The document version is adk.declarative/v0alpha1. It is experimental and
// carries no compatibility promise: fields may change or disappear between
// releases until the language graduates.
//
// # Layout
//
// The package is split along the repository's semantic/pragmatic boundary:
//
//   - spec.go and load.go define and decode the document (data only).
//   - validate.go checks the document without touching any runtime
//     dependency. Every violation is reported with its field path. A Registry
//     extends the same pass with a check that every name resolves.
//   - registry.go holds the Go values a document may reference by name
//     (functions and tools) and the workflow's model.
//   - build.go and nodes.go resolve a validated document against a Registry
//     and produce ADK workflow nodes, edges, and agents.
//
// A document never carries code. Behavior is attached by name through the
// Registry.
//
// # Models
//
// A workflow runs on one model, the one its embedder passes to NewRegistry
// (in kagent, the agent's ModelConfig). LlmAgent agents and
// llm nodes therefore omit model. The only value model accepts is "stub",
// which selects the built-in offline model so that documents run in tests and
// examples without credentials. Registry.Validate reports an LLM that omits
// model when no model is set.
//
// # Prompts
//
// Instructions are inline text. A document cannot read prompts from Secrets
// or the process environment.
package declarative
