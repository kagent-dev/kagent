package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"strings"

	"github.com/kagent-dev/kagent/go/tools/internal/common"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// K8sClient wraps Kubernetes client operations
type K8sClient struct {
	clientset kubernetes.Interface
	config    *rest.Config
}

// NewK8sClient creates a new Kubernetes client
func NewK8sClient() (*K8sClient, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fallback to kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create k8s config: %v", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s clientset: %v", err)
	}

	return &K8sClient{
		clientset: clientset,
		config:    config,
	}, nil
}

// Enhanced kubectl get with native K8s client
func handleKubectlGetEnhanced(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resourceType := mcp.ParseString(request, "resource_type", "")
	resourceName := mcp.ParseString(request, "resource_name", "")
	namespace := mcp.ParseString(request, "namespace", "")
	allNamespaces := mcp.ParseString(request, "all_namespaces", "") == "true"
	output := mcp.ParseString(request, "output", "json")

	if resourceType == "" {
		return mcp.NewToolResultError("resource_type parameter is required"), nil
	}

	client, err := NewK8sClient()
	if err != nil {
		// Fallback to kubectl command if client creation fails
		return handleKubectlGetTool(ctx, request)
	}

	switch resourceType {
	case "pods", "pod":
		return getPodsNative(ctx, client, resourceName, namespace, allNamespaces, output)
	case "services", "service", "svc":
		return getServicesNative(ctx, client, resourceName, namespace, allNamespaces, output)
	case "deployments", "deployment", "deploy":
		return getDeploymentsNative(ctx, client, resourceName, namespace, allNamespaces, output)
	case "configmaps", "configmap", "cm":
		return getConfigMapsNative(ctx, client, resourceName, namespace, allNamespaces, output)
	default:
		// Fallback to kubectl for unsupported resource types
		return handleKubectlGetTool(ctx, request)
	}
}

func getPodsNative(ctx context.Context, client *K8sClient, name, namespace string, allNamespaces bool, output string) (*mcp.CallToolResult, error) {
	var pods *corev1.PodList
	var err error

	if name != "" {
		pod, err := client.clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get pod: %v", err)), nil
		}
		pods = &corev1.PodList{Items: []corev1.Pod{*pod}}
	} else if allNamespaces {
		pods, err = client.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	} else {
		pods, err = client.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	}

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list pods: %v", err)), nil
	}

	return formatResourceOutput(pods, output)
}

func getServicesNative(ctx context.Context, client *K8sClient, name, namespace string, allNamespaces bool, output string) (*mcp.CallToolResult, error) {
	var services *corev1.ServiceList
	var err error

	if name != "" {
		service, err := client.clientset.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get service: %v", err)), nil
		}
		services = &corev1.ServiceList{Items: []corev1.Service{*service}}
	} else if allNamespaces {
		services, err = client.clientset.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	} else {
		services, err = client.clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	}

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list services: %v", err)), nil
	}

	return formatResourceOutput(services, output)
}

func getDeploymentsNative(ctx context.Context, client *K8sClient, name, namespace string, allNamespaces bool, output string) (*mcp.CallToolResult, error) {
	var deployments *v1.DeploymentList
	var err error

	if name != "" {
		deployment, err := client.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get deployment: %v", err)), nil
		}
		deployments = &v1.DeploymentList{Items: []v1.Deployment{*deployment}}
	} else if allNamespaces {
		deployments, err = client.clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	} else {
		deployments, err = client.clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	}

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list deployments: %v", err)), nil
	}

	return formatResourceOutput(deployments, output)
}

func getConfigMapsNative(ctx context.Context, client *K8sClient, name, namespace string, allNamespaces bool, output string) (*mcp.CallToolResult, error) {
	var configMaps *corev1.ConfigMapList
	var err error

	if name != "" {
		configMap, err := client.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get configmap: %v", err)), nil
		}
		configMaps = &corev1.ConfigMapList{Items: []corev1.ConfigMap{*configMap}}
	} else if allNamespaces {
		configMaps, err = client.clientset.CoreV1().ConfigMaps("").List(ctx, metav1.ListOptions{})
	} else {
		configMaps, err = client.clientset.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
	}

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list configmaps: %v", err)), nil
	}

	return formatResourceOutput(configMaps, output)
}

func formatResourceOutput(data interface{}, output string) (*mcp.CallToolResult, error) {
	if output == "json" || output == "" {
		jsonData, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal JSON: %v", err)), nil
		}
		return mcp.NewToolResultText(string(jsonData)), nil
	}

	// For other output formats, convert to string representation
	jsonData, _ := json.Marshal(data)
	return mcp.NewToolResultText(string(jsonData)), nil
}

// Enhanced get pod logs with native client
func handleKubectlLogsEnhanced(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	podName := mcp.ParseString(request, "pod_name", "")
	namespace := mcp.ParseString(request, "namespace", "default")
	container := mcp.ParseString(request, "container", "")
	tailLines := mcp.ParseInt(request, "tail_lines", 50)

	if podName == "" {
		return mcp.NewToolResultError("pod_name parameter is required"), nil
	}

	client, err := NewK8sClient()
	if err != nil {
		// Fallback to kubectl command
		return handleKubectlLogsTool(ctx, request)
	}

	lines := int64(tailLines)
	logOptions := &corev1.PodLogOptions{
		TailLines: &lines,
	}

	if container != "" {
		logOptions.Container = container
	}

	logs, err := client.clientset.CoreV1().Pods(namespace).GetLogs(podName, logOptions).DoRaw(ctx)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get pod logs: %v", err)), nil
	}

	return mcp.NewToolResultText(string(logs)), nil
}

// Scale deployment using native client
func handleScaleDeployment(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	deploymentName := mcp.ParseString(request, "name", "")
	namespace := mcp.ParseString(request, "namespace", "default")
	replicas := mcp.ParseInt(request, "replicas", 1)

	if deploymentName == "" {
		return mcp.NewToolResultError("name parameter is required"), nil
	}

	client, err := NewK8sClient()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to create k8s client: %v", err)), nil
	}

	deployment, err := client.clientset.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get deployment: %v", err)), nil
	}

	replicasInt32 := int32(replicas)
	deployment.Spec.Replicas = &replicasInt32

	_, err = client.clientset.AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to scale deployment: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("deployment.apps/%s scaled to %d replicas", deploymentName, replicas)), nil
}

// Patch resource using native client
func handlePatchResource(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resourceType := mcp.ParseString(request, "resource_type", "")
	resourceName := mcp.ParseString(request, "resource_name", "")
	patch := mcp.ParseString(request, "patch", "")
	namespace := mcp.ParseString(request, "namespace", "default")

	if resourceType == "" || resourceName == "" || patch == "" {
		return mcp.NewToolResultError("resource_type, resource_name, and patch parameters are required"), nil
	}

	client, err := NewK8sClient()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to create k8s client: %v", err)), nil
	}

	patchBytes := []byte(patch)

	switch resourceType {
	case "deployment", "deployments":
		result, err := client.clientset.AppsV1().Deployments(namespace).Patch(ctx, resourceName, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to patch deployment: %v", err)), nil
		}
		return formatResourceOutput(result, "json")

	case "service", "services":
		result, err := client.clientset.CoreV1().Services(namespace).Patch(ctx, resourceName, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to patch service: %v", err)), nil
		}
		return formatResourceOutput(result, "json")

	default:
		return mcp.NewToolResultError(fmt.Sprintf("Resource type %s not supported for native patching", resourceType)), nil
	}
}

// Apply manifest from content
func handleApplyManifest(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	manifest := mcp.ParseString(request, "manifest", "")

	if manifest == "" {
		return mcp.NewToolResultError("manifest parameter is required"), nil
	}

	// Create temporary file
	tmpFile, err := ioutil.TempFile("", "k8s-manifest-*.yaml")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to create temp file: %v", err)), nil
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(manifest); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to write manifest: %v", err)), nil
	}
	tmpFile.Close()

	// Use kubectl apply
	args := []string{"apply", "-f", tmpFile.Name()}
	result, err := common.RunCommand("kubectl", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("kubectl apply failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Delete resource using native client
func handleDeleteResource(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resourceType := mcp.ParseString(request, "resource_type", "")
	resourceName := mcp.ParseString(request, "resource_name", "")
	namespace := mcp.ParseString(request, "namespace", "default")

	if resourceType == "" || resourceName == "" {
		return mcp.NewToolResultError("resource_type and resource_name parameters are required"), nil
	}

	client, err := NewK8sClient()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to create k8s client: %v", err)), nil
	}

	switch resourceType {
	case "pod", "pods":
		err := client.clientset.CoreV1().Pods(namespace).Delete(ctx, resourceName, metav1.DeleteOptions{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to delete pod: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("pod \"%s\" deleted", resourceName)), nil

	case "service", "services":
		err := client.clientset.CoreV1().Services(namespace).Delete(ctx, resourceName, metav1.DeleteOptions{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to delete service: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("service \"%s\" deleted", resourceName)), nil

	case "deployment", "deployments":
		err := client.clientset.AppsV1().Deployments(namespace).Delete(ctx, resourceName, metav1.DeleteOptions{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to delete deployment: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("deployment.apps \"%s\" deleted", resourceName)), nil

	default:
		return mcp.NewToolResultError(fmt.Sprintf("Resource type %s not supported for native deletion", resourceType)), nil
	}
}

// Check service connectivity
func handleCheckServiceConnectivity(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	serviceName := mcp.ParseString(request, "service_name", "")

	if serviceName == "" {
		return mcp.NewToolResultError("service_name parameter is required"), nil
	}

	// Create a temporary curl pod
	podName := fmt.Sprintf("curlpod-%d", rand.Intn(1000))

	// Create pod
	args := []string{"run", podName, "--image=curlimages/curl", "--restart=Never", "--command", "--", "sleep", "3600"}
	_, err := common.RunCommand("kubectl", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to create curl pod: %v", err)), nil
	}

	// Wait for pod to be ready
	waitArgs := []string{"wait", "--for=condition=ready", fmt.Sprintf("pod/%s", podName), "--timeout=60s"}
	_, err = common.RunCommand("kubectl", waitArgs)
	if err != nil {
		// Clean up the pod
		common.RunCommand("kubectl", []string{"delete", "pod", podName})
		return mcp.NewToolResultError(fmt.Sprintf("Pod failed to become ready: %v", err)), nil
	}

	// Execute curl command
	curlArgs := []string{"exec", podName, "--", "curl", serviceName}
	result, err := common.RunCommand("kubectl", curlArgs)

	// Clean up the pod
	common.RunCommand("kubectl", []string{"delete", "pod", podName})

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Curl command failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Get cluster events using native client
func handleGetEvents(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	namespace := mcp.ParseString(request, "namespace", "")

	client, err := NewK8sClient()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to create k8s client: %v", err)), nil
	}

	events, err := client.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get events: %v", err)), nil
	}

	return formatResourceOutput(events, "json")
}

// Execute command in pod using native client
func handleExecCommand(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	podName := mcp.ParseString(request, "pod_name", "")
	namespace := mcp.ParseString(request, "namespace", "default")
	command := mcp.ParseString(request, "command", "")

	if podName == "" || command == "" {
		return mcp.NewToolResultError("pod_name and command parameters are required"), nil
	}

	args := []string{"exec", podName, "-n", namespace, "--", "sh", "-c", command}
	result, err := common.RunCommand("kubectl", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Command execution failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Original kubectl functions for backward compatibility
func handleKubectlGetTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resourceType := mcp.ParseString(request, "resource_type", "")
	resourceName := mcp.ParseString(request, "resource_name", "")
	namespace := mcp.ParseString(request, "namespace", "")

	if resourceType == "" {
		return mcp.NewToolResultError("resource_type parameter is required"), nil
	}

	args := []string{"get", resourceType}

	if resourceName != "" {
		args = append(args, resourceName)
	}

	if namespace != "" {
		args = append(args, "-n", namespace)
	}

	args = append(args, "-o", "json")

	result, err := common.RunCommand("kubectl", args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(result), nil
}

func handleKubectlDescribeTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resourceType := mcp.ParseString(request, "resource_type", "")
	resourceName := mcp.ParseString(request, "resource_name", "")
	namespace := mcp.ParseString(request, "namespace", "")

	if resourceType == "" {
		return mcp.NewToolResultError("resource_type parameter is required"), nil
	}
	if resourceName == "" {
		return mcp.NewToolResultError("resource_name parameter is required"), nil
	}

	args := []string{"describe", resourceType, resourceName}

	if namespace != "" {
		args = append(args, "-n", namespace)
	}

	result, err := common.RunCommand("kubectl", args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(result), nil
}

func handleKubectlLogsTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	podName := mcp.ParseString(request, "pod_name", "")
	namespace := mcp.ParseString(request, "namespace", "")
	container := mcp.ParseString(request, "container", "")

	if podName == "" {
		return mcp.NewToolResultError("pod_name parameter is required"), nil
	}

	args := []string{"logs", podName}

	if namespace != "" {
		args = append(args, "-n", namespace)
	}

	if container != "" {
		args = append(args, "-c", container)
	}

	result, err := common.RunCommand("kubectl", args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(result), nil
}

func RegisterK8sTools(s *server.MCPServer) {
	// Enhanced kubectl get with native K8s client support
	s.AddTool(mcp.NewTool("kubectl_get",
		mcp.WithDescription("Get Kubernetes resources using kubectl with enhanced native client support"),
		mcp.WithString("resource_type", mcp.Description("Type of resource (pod, service, deployment, etc.)"), mcp.Required()),
		mcp.WithString("resource_name", mcp.Description("Name of specific resource (optional)")),
		mcp.WithString("namespace", mcp.Description("Namespace to query (optional)")),
		mcp.WithString("all_namespaces", mcp.Description("Query all namespaces (true/false)")),
		mcp.WithString("output", mcp.Description("Output format (json, yaml, wide, etc.)")),
	), handleKubectlGetEnhanced)

	// Original kubectl describe (kept for compatibility)
	s.AddTool(mcp.NewTool("kubectl_describe",
		mcp.WithDescription("Describe a Kubernetes resource using kubectl"),
		mcp.WithString("resource_type", mcp.Description("Type of resource (pod, service, deployment, etc.)"), mcp.Required()),
		mcp.WithString("resource_name", mcp.Description("Name of the resource"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace of the resource (optional)")),
	), handleKubectlDescribeTool)

	// Enhanced kubectl logs with native client
	s.AddTool(mcp.NewTool("kubectl_logs",
		mcp.WithDescription("Get logs from a Kubernetes pod with enhanced native client support"),
		mcp.WithString("pod_name", mcp.Description("Name of the pod"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace of the pod (default: default)")),
		mcp.WithString("container", mcp.Description("Container name (for multi-container pods)")),
		mcp.WithNumber("tail_lines", mcp.Description("Number of lines to show from the end (default: 50)")),
	), handleKubectlLogsEnhanced)

	// Scale deployment
	s.AddTool(mcp.NewTool("scale_deployment",
		mcp.WithDescription("Scale a Kubernetes deployment using native client"),
		mcp.WithString("name", mcp.Description("Name of the deployment"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace of the deployment (default: default)")),
		mcp.WithNumber("replicas", mcp.Description("Number of replicas"), mcp.Required()),
	), handleScaleDeployment)

	// Patch resource
	s.AddTool(mcp.NewTool("patch_resource",
		mcp.WithDescription("Patch a Kubernetes resource using strategic merge patch"),
		mcp.WithString("resource_type", mcp.Description("Type of resource (deployment, service, etc.)"), mcp.Required()),
		mcp.WithString("resource_name", mcp.Description("Name of the resource"), mcp.Required()),
		mcp.WithString("patch", mcp.Description("JSON patch to apply"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace of the resource (default: default)")),
	), handlePatchResource)

	// Apply manifest
	s.AddTool(mcp.NewTool("apply_manifest",
		mcp.WithDescription("Apply a YAML manifest to the Kubernetes cluster"),
		mcp.WithString("manifest", mcp.Description("YAML manifest content"), mcp.Required()),
	), handleApplyManifest)

	// Delete resource
	s.AddTool(mcp.NewTool("delete_resource",
		mcp.WithDescription("Delete a Kubernetes resource using native client"),
		mcp.WithString("resource_type", mcp.Description("Type of resource (pod, service, deployment, etc.)"), mcp.Required()),
		mcp.WithString("resource_name", mcp.Description("Name of the resource"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace of the resource (default: default)")),
	), handleDeleteResource)

	// Check service connectivity
	s.AddTool(mcp.NewTool("check_service_connectivity",
		mcp.WithDescription("Check connectivity to a service using a temporary curl pod"),
		mcp.WithString("service_name", mcp.Description("Service name to test (e.g., my-service.my-namespace.svc.cluster.local:80)"), mcp.Required()),
	), handleCheckServiceConnectivity)

	// Get events
	s.AddTool(mcp.NewTool("get_events",
		mcp.WithDescription("Get Kubernetes cluster events using native client"),
		mcp.WithString("namespace", mcp.Description("Namespace to query events from (optional, default: all namespaces)")),
	), handleGetEvents)

	// Execute command in pod
	s.AddTool(mcp.NewTool("exec_command",
		mcp.WithDescription("Execute a command inside a Kubernetes pod"),
		mcp.WithString("pod_name", mcp.Description("Name of the pod"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace of the pod (default: default)")),
		mcp.WithString("command", mcp.Description("Command to execute"), mcp.Required()),
	), handleExecCommand)

	// Get API resources
	s.AddTool(mcp.NewTool("get_api_resources",
		mcp.WithDescription("Get available API resources in the cluster"),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := common.RunCommand("kubectl", []string{"api-resources"})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get API resources: %v", err)), nil
		}
		return mcp.NewToolResultText(result), nil
	})

	// Get cluster configuration
	s.AddTool(mcp.NewTool("get_cluster_config",
		mcp.WithDescription("Get Kubernetes cluster configuration"),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := common.RunCommand("kubectl", []string{"config", "view"})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get cluster config: %v", err)), nil
		}
		return mcp.NewToolResultText(result), nil
	})

	// Rollout operations
	s.AddTool(mcp.NewTool("rollout",
		mcp.WithDescription("Perform rollout operations on Kubernetes resources"),
		mcp.WithString("action", mcp.Description("Rollout action (status, history, pause, resume, restart, undo)"), mcp.Required()),
		mcp.WithString("resource_type", mcp.Description("Type of resource (deployment, daemonset, statefulset)"), mcp.Required()),
		mcp.WithString("resource_name", mcp.Description("Name of the resource"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace of the resource (optional)")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		action := mcp.ParseString(request, "action", "")
		resourceType := mcp.ParseString(request, "resource_type", "")
		resourceName := mcp.ParseString(request, "resource_name", "")
		namespace := mcp.ParseString(request, "namespace", "")

		if action == "" || resourceType == "" || resourceName == "" {
			return mcp.NewToolResultError("action, resource_type, and resource_name are required"), nil
		}

		args := []string{"rollout", action, fmt.Sprintf("%s/%s", resourceType, resourceName)}
		if namespace != "" {
			args = append(args, "-n", namespace)
		}

		result, err := common.RunCommand("kubectl", args)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Rollout command failed: %v", err)), nil
		}

		return mcp.NewToolResultText(result), nil
	})

	// Label resource
	s.AddTool(mcp.NewTool("label_resource",
		mcp.WithDescription("Add or update labels on a Kubernetes resource"),
		mcp.WithString("resource_type", mcp.Description("Type of resource"), mcp.Required()),
		mcp.WithString("resource_name", mcp.Description("Name of the resource"), mcp.Required()),
		mcp.WithString("labels", mcp.Description("Labels in key=value format, space-separated"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace of the resource (optional)")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		resourceType := mcp.ParseString(request, "resource_type", "")
		resourceName := mcp.ParseString(request, "resource_name", "")
		labels := mcp.ParseString(request, "labels", "")
		namespace := mcp.ParseString(request, "namespace", "")

		if resourceType == "" || resourceName == "" || labels == "" {
			return mcp.NewToolResultError("resource_type, resource_name, and labels are required"), nil
		}

		args := []string{"label", resourceType, resourceName}
		if namespace != "" {
			args = append(args, "-n", namespace)
		}
		args = append(args, strings.Split(labels, " ")...)

		result, err := common.RunCommand("kubectl", args)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Label command failed: %v", err)), nil
		}

		return mcp.NewToolResultText(result), nil
	})

	// Annotate resource
	s.AddTool(mcp.NewTool("annotate_resource",
		mcp.WithDescription("Add or update annotations on a Kubernetes resource"),
		mcp.WithString("resource_type", mcp.Description("Type of resource"), mcp.Required()),
		mcp.WithString("resource_name", mcp.Description("Name of the resource"), mcp.Required()),
		mcp.WithString("annotations", mcp.Description("Annotations in key=value format, space-separated"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace of the resource (optional)")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		resourceType := mcp.ParseString(request, "resource_type", "")
		resourceName := mcp.ParseString(request, "resource_name", "")
		annotations := mcp.ParseString(request, "annotations", "")
		namespace := mcp.ParseString(request, "namespace", "")

		if resourceType == "" || resourceName == "" || annotations == "" {
			return mcp.NewToolResultError("resource_type, resource_name, and annotations are required"), nil
		}

		args := []string{"annotate", resourceType, resourceName}
		if namespace != "" {
			args = append(args, "-n", namespace)
		}
		args = append(args, strings.Split(annotations, " ")...)

		result, err := common.RunCommand("kubectl", args)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Annotate command failed: %v", err)), nil
		}

		return mcp.NewToolResultText(result), nil
	})
}
