package env

import "time"

// KUBECONFIG remains an external client-go interface.
var Kubeconfig = RegisterStringVar("KUBECONFIG", "", "Kubernetes client configuration file list. When unset, client-go uses its normal in-cluster or user kubeconfig discovery.", ComponentController, ComponentCLI, ComponentTesting)

var (
	ManagedAgentsEnabled           = RegisterBoolVar("KAGENT_MANAGED_AGENTS_ENABLED", false, "Enable the bundled Anthropic Managed Agents launcher on every controller replica.", ComponentController)
	ManagedAgentsEnvironmentID     = RegisterStringVar("ANTHROPIC_ENVIRONMENT_ID", "", "Anthropic self-hosted environment whose work the launcher claims.", ComponentController)
	ManagedAgentsEnvironmentKey    = RegisterStringVar("ANTHROPIC_ENVIRONMENT_KEY", "", "Standing Anthropic environment credential for the launcher; never forwarded into sandboxes.", ComponentController)
	ManagedAgentsBaseURL           = RegisterStringVar("ANTHROPIC_BASE_URL", "", "Optional Anthropic API endpoint override for the launcher and sandbox CLI.", ComponentController)
	ManagedAgentsTemplateNamespace = RegisterStringVar("KAGENT_MANAGED_AGENTS_TEMPLATE_NAMESPACE", "", "Namespace of the SandboxTemplate containing the Anthropic CLI.", ComponentController)
	ManagedAgentsTemplateName      = RegisterStringVar("KAGENT_MANAGED_AGENTS_TEMPLATE_NAME", "", "SandboxTemplate containing the Anthropic CLI.", ComponentController)
	ManagedAgentsSandboxTTL        = RegisterDurationVar("KAGENT_MANAGED_AGENTS_SANDBOX_TTL", 12*time.Hour, "Lifetime of new Anthropic managed sandboxes and timeout of their CLI workers.", ComponentController)
)
