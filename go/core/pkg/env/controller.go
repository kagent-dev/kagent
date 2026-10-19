package env

// KUBECONFIG remains an external client-go interface.
var Kubeconfig = RegisterStringVar("KUBECONFIG", "", "Kubernetes client configuration file list. When unset, client-go uses its normal in-cluster or user kubeconfig discovery.", ComponentController, ComponentCLI, ComponentTesting)
