// Package declarative compiles an adk.declarative/v0.2 AgentSystem document
// into native Google ADK Go agents.
//
// The package is split along the repository's semantic/pragmatic boundary:
//
//   - spec.go and load.go define and decode the document (data only).
//   - validate.go checks the document without touching any runtime
//     dependency. Every violation is reported with its field path. A Registry
//     extends the same pass with a check that every name resolves.
//   - registry.go holds the Go values a document may reference by name
//     (functions, tools, models, node types).
//   - build.go and nodes.go resolve a validated document against a Registry
//     and produce ADK workflow nodes, edges, and agents, including the
//     Sequential, Parallel and Loop agents.
//
// A document never carries code. Behavior is attached by name through the
// Registry, and new node types can be added with Registry.RegisterNodeType
// without modifying this package.
package declarative
