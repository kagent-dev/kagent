# CRD Types & Patterns (v1alpha2)

## Existing CRDs
- `Agent` / `AgentList` — agent definition (Declarative or BYO)
- `ModelConfig` / `ModelConfigList` — LLM model configuration
- `ModelProviderConfig` / `ModelProviderConfigList` — provider endpoints
- `RemoteMCPServer` / `RemoteMCPServerList` — remote MCP server references

## Cross-Resource Reference Pattern
```go
type TypedLocalReference struct {
    Kind      string `json:"kind"`
    ApiGroup  string `json:"apiGroup"`
    Name      string `json:"name"`
    Namespace string `json:"namespace,omitempty"`
}
```
Used by Agent's Tool references. For simpler cases (same namespace), a plain string ref is used (e.g., `ModelConfig string`).

## Status Pattern
All CRDs use:
```go
type SomeStatus struct {
    ObservedGeneration int64              `json:"observedGeneration"`
    Conditions         []metav1.Condition `json:"conditions,omitempty"`
    // Additional fields as needed
}
```
Common condition types: `Ready`, `Accepted`.

## Kubebuilder Markers
- `+kubebuilder:object:root=true` — root type
- `+kubebuilder:subresource:status` — status subresource
- `+kubebuilder:storageversion` — storage version
- `+kubebuilder:printcolumn` — kubectl column display
- `+kubebuilder:validation:Enum`, `Required`, `XValidation` — validation

## Codegen
`make -C go generate` runs controller-gen for deepcopy and CRD manifests.
