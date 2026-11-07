package env

// KUBECONFIG remains an external client-go interface.
var Kubeconfig = RegisterStringVar("KUBECONFIG", "", "Kubernetes client configuration file list. When unset, client-go uses its normal in-cluster or user kubeconfig discovery.", ComponentController, ComponentCLI, ComponentTesting)

// The chart projects these identities through the Kubernetes Downward API.
var (
	ControllerPodName = RegisterStringVar("K8S_POD_NAME", "", "Controller Pod name projected by Kubernetes for startup coordination and telemetry.", ComponentController)
	ControllerPodUID  = RegisterStringVar("K8S_POD_UID", "", "Controller Pod UID projected by Kubernetes for startup coordination and telemetry.", ComponentController)
)
