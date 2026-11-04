package env

import "time"

// KUBECONFIG remains an external client-go interface.
var Kubeconfig = RegisterStringVar("KUBECONFIG", "", "Kubernetes client configuration file list. When unset, client-go uses its normal in-cluster or user kubeconfig discovery.", ComponentController, ComponentCLI, ComponentTesting)

var (
	AnthropicSelfHostedSandboxesEnabled           = RegisterBoolVar("KAGENT_ANTHROPIC_SELF_HOSTED_SANDBOXES_ENABLED", false, "Enable the bundled Anthropic self-hosted sandboxes launcher on every controller replica.", ComponentController)
	AnthropicSelfHostedSandboxesEnvironmentID     = RegisterStringVar("ANTHROPIC_ENVIRONMENT_ID", "", "Anthropic self-hosted environment whose work the launcher claims.", ComponentController)
	AnthropicSelfHostedSandboxesEnvironmentKey    = RegisterStringVar("ANTHROPIC_ENVIRONMENT_KEY", "", "Standing Anthropic environment credential for the launcher; never forwarded into sandboxes.", ComponentController)
	AnthropicSelfHostedSandboxesBaseURL           = RegisterStringVar("ANTHROPIC_BASE_URL", "", "Optional Anthropic API endpoint override for the launcher and sandbox CLI.", ComponentController)
	AnthropicSelfHostedSandboxesTemplateNamespace = RegisterStringVar("KAGENT_ANTHROPIC_SELF_HOSTED_SANDBOXES_TEMPLATE_NAMESPACE", "", "Namespace of the SandboxTemplate containing the Anthropic CLI.", ComponentController)
	AnthropicSelfHostedSandboxesTemplateName      = RegisterStringVar("KAGENT_ANTHROPIC_SELF_HOSTED_SANDBOXES_TEMPLATE_NAME", "", "SandboxTemplate containing the Anthropic CLI.", ComponentController)
	AnthropicSelfHostedSandboxesTTL               = RegisterDurationVar("KAGENT_ANTHROPIC_SELF_HOSTED_SANDBOXES_TTL", 12*time.Hour, "Lifetime of new Anthropic self-hosted sandboxes and timeout of their CLI workers.", ComponentController)
)
