package env

// KUBECONFIG remains an external client-go interface.
var Kubeconfig = RegisterStringVar("KUBECONFIG", "", "Kubernetes client configuration file list. When unset, client-go uses its normal in-cluster or user kubeconfig discovery.", ComponentController, ComponentCLI, ComponentTesting)

var AnthropicSandboxManagedEncryptionSecret = RegisterStringVar("KAGENT_ANTHROPIC_SANDBOX_MANAGED_ENCRYPTION_SECRET", "",
	"Name of the Anthropic sandbox encryption Secret to provision in the controller pod namespace.", ComponentController)
