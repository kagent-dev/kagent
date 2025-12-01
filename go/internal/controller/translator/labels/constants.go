package labels

// Standard Kubernetes label keys as defined in the Kubernetes documentation
// https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
// TODO: determine shared set of labels used for agents and MCP servers
const (
	AppName      = "app.kubernetes.io/name"
	AppInstance  = "app.kubernetes.io/instance"
	AppVersion   = "app.kubernetes.io/version"
	AppComponent = "app.kubernetes.io/component"
	AppPartOf    = "app.kubernetes.io/part-of"
	AppManagedBy = "app.kubernetes.io/managed-by"
)

// Common label values
const (
	ManagedByKagent = "kagent"
)
