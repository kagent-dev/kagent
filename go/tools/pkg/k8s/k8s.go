package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"

	"k8s.io/client-go/tools/clientcmd"

	"github.com/kagent-dev/kagent/go/tools/pkg/logger"
	"github.com/kagent-dev/kagent/go/tools/pkg/utils"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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

// K8sTool struct to hold the client
type K8sTool struct {
	client *K8sClient
}

func NewK8sTool() (*K8sTool, error) {
	client, err := NewK8sClient()
	if err != nil {
		return nil, err
	}
	return &K8sTool{client: client}, nil
}

// Enhanced kubectl get with native K8s client
func (k *K8sTool) handleKubectlGetEnhanced(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resourceType := mcp.ParseString(request, "resource_type", "")
	resourceName := mcp.ParseString(request, "resource_name", "")
	namespace := mcp.ParseString(request, "namespace", "")
	allNamespaces := mcp.ParseString(request, "all_namespaces", "") == "true"
	output := mcp.ParseString(request, "output", "json")

	if resourceType == "" {
		return mcp.NewToolResultError("resource_type parameter is required"), nil
	}

	switch resourceType {
	case "pods", "pod":
		return k.getPodsNative(ctx, resourceName, namespace, allNamespaces, output)
	case "services", "service", "svc":
		return k.getServicesNative(ctx, resourceName, namespace, allNamespaces, output)
	case "deployments", "deployment", "deploy":
		return k.getDeploymentsNative(ctx, resourceName, namespace, allNamespaces, output)
	case "configmaps", "configmap", "cm":
		return k.getConfigMapsNative(ctx, resourceName, namespace, allNamespaces, output)
	default:
		// Fallback to kubectl for unsupported resource types
		return k.handleKubectlGetTool(ctx, request)
	}
}

func (k *K8sTool) getPodsNative(ctx context.Context, name, namespace string, allNamespaces bool, output string) (*mcp.CallToolResult, error) {
	var pods *corev1.PodList
	var err error

	if name != "" {
		pod, err := k.client.clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get pod: %v", err)), nil
		}
		pods = &corev1.PodList{Items: []corev1.Pod{*pod}}
	} else if allNamespaces {
		pods, err = k.client.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	} else {
		pods, err = k.client.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	}

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list pods: %v", err)), nil
	}

	return formatResourceOutput(pods, output)
}

func (k *K8sTool) getServicesNative(ctx context.Context, name, namespace string, allNamespaces bool, output string) (*mcp.CallToolResult, error) {
	var services *corev1.ServiceList
	var err error

	if name != "" {
		service, err := k.client.clientset.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get service: %v", err)), nil
		}
		services = &corev1.ServiceList{Items: []corev1.Service{*service}}
	} else if allNamespaces {
		services, err = k.client.clientset.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	} else {
		services, err = k.client.clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	}

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list services: %v", err)), nil
	}

	return formatResourceOutput(services, output)
}

func (k *K8sTool) getDeploymentsNative(ctx context.Context, name, namespace string, allNamespaces bool, output string) (*mcp.CallToolResult, error) {
	var deployments *v1.DeploymentList
	var err error

	if name != "" {
		deployment, err := k.client.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get deployment: %v", err)), nil
		}
		deployments = &v1.DeploymentList{Items: []v1.Deployment{*deployment}}
	} else if allNamespaces {
		deployments, err = k.client.clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	} else {
		deployments, err = k.client.clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	}

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list deployments: %v", err)), nil
	}

	return formatResourceOutput(deployments, output)
}

func (k *K8sTool) getConfigMapsNative(ctx context.Context, name, namespace string, allNamespaces bool, output string) (*mcp.CallToolResult, error) {
	var configMaps *corev1.ConfigMapList
	var err error

	if name != "" {
		configMap, err := k.client.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get configmap: %v", err)), nil
		}
		configMaps = &corev1.ConfigMapList{Items: []corev1.ConfigMap{*configMap}}
	} else if allNamespaces {
		configMaps, err = k.client.clientset.CoreV1().ConfigMaps("").List(ctx, metav1.ListOptions{})
	} else {
		configMaps, err = k.client.clientset.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
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
func (k *K8sTool) handleKubectlLogsEnhanced(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	podName := mcp.ParseString(request, "pod_name", "")
	namespace := mcp.ParseString(request, "namespace", "default")
	container := mcp.ParseString(request, "container", "")
	tailLines := mcp.ParseInt(request, "tail_lines", 50)

	if podName == "" {
		return mcp.NewToolResultError("pod_name parameter is required"), nil
	}

	lines := int64(tailLines)
	logOptions := &corev1.PodLogOptions{
		TailLines: &lines,
	}

	if container != "" {
		logOptions.Container = container
	}

	logs, err := k.client.clientset.CoreV1().Pods(namespace).GetLogs(podName, logOptions).DoRaw(ctx)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get pod logs: %v", err)), nil
	}

	return mcp.NewToolResultText(string(logs)), nil
}

// Scale deployment using native client
func (k *K8sTool) handleScaleDeployment(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	deploymentName := mcp.ParseString(request, "name", "")
	namespace := mcp.ParseString(request, "namespace", "default")
	replicas := mcp.ParseInt(request, "replicas", 1)

	if deploymentName == "" {
		return mcp.NewToolResultError("name parameter is required"), nil
	}

	deployment, err := k.client.clientset.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get deployment: %v", err)), nil
	}

	replicasInt32 := int32(replicas)
	deployment.Spec.Replicas = &replicasInt32

	_, err = k.client.clientset.AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to scale deployment: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Deployment %s scaled to %d replicas", deploymentName, replicas)), nil
}

// Patch resource using native client
func (k *K8sTool) handlePatchResource(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resourceType := mcp.ParseString(request, "resource_type", "")
	resourceName := mcp.ParseString(request, "resource_name", "")
	patch := mcp.ParseString(request, "patch", "")
	namespace := mcp.ParseString(request, "namespace", "default")

	if resourceType == "" || resourceName == "" || patch == "" {
		return mcp.NewToolResultError("resource_type, resource_name, and patch parameters are required"), nil
	}

	_, err := k.client.clientset.CoreV1().Pods(namespace).Patch(ctx, resourceName, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to patch resource: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Resource %s/%s patched successfully", resourceType, resourceName)), nil
}

// Apply manifest from content
func (k *K8sTool) handleApplyManifest(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	manifest := mcp.ParseString(request, "manifest", "")

	if manifest == "" {
		return mcp.NewToolResultError("manifest parameter is required"), nil
	}

	// This handler still uses kubectl apply, which is not ideal for native Go implementation.
	// For a pure Go approach, we would parse the manifest and use the appropriate client to create/update resources.
	// This is a complex task and for now we will keep the kubectl fallback.
	tmpFile, err := os.CreateTemp("", "manifest-*.yaml")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to create temp file: %v", err)), nil
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(manifest); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to write to temp file: %v", err)), nil
	}
	tmpFile.Close()

	return k.runKubectlCommand(ctx, []string{"apply", "-f", tmpFile.Name()})
}

// Delete resource using native client
func (k *K8sTool) handleDeleteResource(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resourceType := mcp.ParseString(request, "resource_type", "")
	resourceName := mcp.ParseString(request, "resource_name", "")
	namespace := mcp.ParseString(request, "namespace", "default")

	if resourceType == "" || resourceName == "" {
		return mcp.NewToolResultError("resource_type and resource_name parameters are required"), nil
	}

	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}

	var err error
	switch resourceType {
	case "pods", "pod":
		err = k.client.clientset.CoreV1().Pods(namespace).Delete(ctx, resourceName, deleteOptions)
	case "services", "service", "svc":
		err = k.client.clientset.CoreV1().Services(namespace).Delete(ctx, resourceName, deleteOptions)
	case "deployments", "deployment", "deploy":
		err = k.client.clientset.AppsV1().Deployments(namespace).Delete(ctx, resourceName, deleteOptions)
	case "configmaps", "configmap", "cm":
		err = k.client.clientset.CoreV1().ConfigMaps(namespace).Delete(ctx, resourceName, deleteOptions)
	default:
		return mcp.NewToolResultError(fmt.Sprintf("Unsupported resource type for deletion: %s", resourceType)), nil
	}

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to delete resource: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Resource %s/%s deleted successfully", resourceType, resourceName)), nil
}

// Check service connectivity
func (k *K8sTool) handleCheckServiceConnectivity(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	serviceName := mcp.ParseString(request, "service_name", "")
	namespace := mcp.ParseString(request, "namespace", "default")

	if serviceName == "" {
		return mcp.NewToolResultError("service_name parameter is required"), nil
	}

	// This is a complex operation to perform natively, involving creating a temporary pod.
	// We'll keep the kubectl approach for this tool for now.
	podName := fmt.Sprintf("curl-test-%d", rand.Intn(10000))
	defer k.runKubectlCommand(ctx, []string{"delete", "pod", podName, "-n", namespace, "--ignore-not-found"})

	_, err := k.runKubectlCommand(ctx, []string{"run", podName, "--image=curlimages/curl", "-n", namespace, "--restart=Never", "--", "sleep", "3600"})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to create curl pod: %v", err)), nil
	}

	_, err = k.runKubectlCommand(ctx, []string{"wait", "--for=condition=ready", "pod/" + podName, "-n", namespace, "--timeout=60s"})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to wait for curl pod: %v", err)), nil
	}

	return k.runKubectlCommand(ctx, []string{"exec", podName, "-n", namespace, "--", "curl", "-s", serviceName})
}

// Get cluster events using native client
func (k *K8sTool) handleGetEvents(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	namespace := mcp.ParseString(request, "namespace", "")

	events, err := k.client.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get events: %v", err)), nil
	}
	return formatResourceOutput(events, "json")
}

// Execute command in pod using native client
func (k *K8sTool) handleExecCommand(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	podName := mcp.ParseString(request, "pod_name", "")
	namespace := mcp.ParseString(request, "namespace", "default")
	command := mcp.ParseString(request, "command", "")

	if podName == "" || command == "" {
		return mcp.NewToolResultError("pod_name and command parameters are required"), nil
	}

	// This handler uses kubectl exec.
	return k.runKubectlCommand(ctx, []string{"exec", podName, "-n", namespace, "--", command})
}

// Fallback to kubectl command for get operations
func (k *K8sTool) handleKubectlGetTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resourceType := mcp.ParseString(request, "resource_type", "")
	resourceName := mcp.ParseString(request, "resource_name", "")
	namespace := mcp.ParseString(request, "namespace", "")
	output := mcp.ParseString(request, "output", "")

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

	if output != "" {
		args = append(args, "-o", output)
	} else {
		args = append(args, "-o", "json")
	}

	return k.runKubectlCommand(ctx, args)
}

// Fallback to kubectl command for describe operations
func (k *K8sTool) handleKubectlDescribeTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resourceType := mcp.ParseString(request, "resource_type", "")
	resourceName := mcp.ParseString(request, "resource_name", "")
	namespace := mcp.ParseString(request, "namespace", "")

	if resourceType == "" || resourceName == "" {
		return mcp.NewToolResultError("resource_type and resource_name parameters are required"), nil
	}

	args := []string{"describe", resourceType, resourceName}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}

	return k.runKubectlCommand(ctx, args)
}

func (k *K8sTool) runKubectlCommand(ctx context.Context, args []string) (*mcp.CallToolResult, error) {
	result, err := utils.RunCommandWithContext(ctx, "kubectl", args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(result), nil
}

func (k *K8sTool) handleGetAvailableAPIResources(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	serverResources, err := k.client.clientset.Discovery().ServerPreferredResources()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get available API resources: %v", err)), nil
	}

	// We can format this into a more readable string or return the JSON
	jsonData, err := json.MarshalIndent(serverResources, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal JSON: %v", err)), nil
	}

	return mcp.NewToolResultText(string(jsonData)), nil
}

func (k *K8sTool) handleRollout(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	action := mcp.ParseString(request, "action", "")
	resourceType := mcp.ParseString(request, "resource_type", "")
	resourceName := mcp.ParseString(request, "resource_name", "")
	namespace := mcp.ParseString(request, "namespace", "")

	if action == "" || resourceType == "" || resourceName == "" {
		return mcp.NewToolResultError("action, resource_type, and resource_name parameters are required"), nil
	}

	args := []string{"rollout", action, fmt.Sprintf("%s/%s", resourceType, resourceName)}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}

	return k.runKubectlCommand(ctx, args)
}

func (k *K8sTool) handleGetClusterConfiguration(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return k.runKubectlCommand(ctx, []string{"config", "view"})
}

func (k *K8sTool) handleRemoveAnnotation(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resourceType := mcp.ParseString(request, "resource_type", "")
	resourceName := mcp.ParseString(request, "resource_name", "")
	annotationKey := mcp.ParseString(request, "annotation_key", "")
	namespace := mcp.ParseString(request, "namespace", "")

	if resourceType == "" || resourceName == "" || annotationKey == "" {
		return mcp.NewToolResultError("resource_type, resource_name, and annotation_key parameters are required"), nil
	}

	args := []string{"annotate", resourceType, resourceName, annotationKey + "-"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}

	return k.runKubectlCommand(ctx, args)
}

func (k *K8sTool) handleRemoveLabel(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resourceType := mcp.ParseString(request, "resource_type", "")
	resourceName := mcp.ParseString(request, "resource_name", "")
	labelKey := mcp.ParseString(request, "label_key", "")
	namespace := mcp.ParseString(request, "namespace", "")

	if resourceType == "" || resourceName == "" || labelKey == "" {
		return mcp.NewToolResultError("resource_type, resource_name, and label_key parameters are required"), nil
	}

	args := []string{"label", resourceType, resourceName, labelKey + "-"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}

	return k.runKubectlCommand(ctx, args)
}

func (k *K8sTool) handleAnnotateResource(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resourceType := mcp.ParseString(request, "resource_type", "")
	resourceName := mcp.ParseString(request, "resource_name", "")
	annotations := mcp.ParseString(request, "annotations", "")
	namespace := mcp.ParseString(request, "namespace", "")

	if resourceType == "" || resourceName == "" || annotations == "" {
		return mcp.NewToolResultError("resource_type, resource_name, and annotations parameters are required"), nil
	}

	args := []string{"annotate", resourceType, resourceName}
	args = append(args, strings.Fields(annotations)...)

	if namespace != "" {
		args = append(args, "-n", namespace)
	}

	return k.runKubectlCommand(ctx, args)
}

func (k *K8sTool) handleLabelResource(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resourceType := mcp.ParseString(request, "resource_type", "")
	resourceName := mcp.ParseString(request, "resource_name", "")
	labels := mcp.ParseString(request, "labels", "")
	namespace := mcp.ParseString(request, "namespace", "")

	if resourceType == "" || resourceName == "" || labels == "" {
		return mcp.NewToolResultError("resource_type, resource_name, and labels parameters are required"), nil
	}

	args := []string{"label", resourceType, resourceName}
	args = append(args, strings.Fields(labels)...)

	if namespace != "" {
		args = append(args, "-n", namespace)
	}

	return k.runKubectlCommand(ctx, args)
}

func (k *K8sTool) handleCreateResourceFromURL(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	url := mcp.ParseString(request, "url", "")
	namespace := mcp.ParseString(request, "namespace", "")

	if url == "" {
		return mcp.NewToolResultError("url parameter is required"), nil
	}

	args := []string{"create", "-f", url}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}

	return k.runKubectlCommand(ctx, args)
}

func (k *K8sTool) handleGenerateResource(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resourceType := mcp.ParseString(request, "resource_type", "")
	resourceDescription := mcp.ParseString(request, "resource_description", "")

	if resourceType == "" || resourceDescription == "" {
		return mcp.NewToolResultError("resource_type and resource_description parameters are required"), nil
	}

	// Generate basic YAML templates based on resource type and description
	template := generateResourceTemplate(resourceType, resourceDescription)
	if template == "" {
		return mcp.NewToolResultError(fmt.Sprintf("Unsupported resource type: %s", resourceType)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("# Generated YAML for %s\n# Description: %s\n%s", resourceType, resourceDescription, template)), nil
}

// generateResourceTemplate provides basic YAML templates for common Kubernetes resources
func generateResourceTemplate(resourceType, description string) string {
	switch strings.ToLower(resourceType) {
	case "deployment":
		return `apiVersion: apps/v1
kind: Deployment
metadata:
  name: CHANGEME-deployment
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: CHANGEME-app
  template:
    metadata:
      labels:
        app: CHANGEME-app
    spec:
      containers:
      - name: CHANGEME-container
        image: CHANGEME-image:latest
        ports:
        - containerPort: 80`

	case "service":
		return `apiVersion: v1
kind: Service
metadata:
  name: CHANGEME-service
  namespace: default
spec:
  selector:
    app: CHANGEME-app
  ports:
  - protocol: TCP
    port: 80
    targetPort: 80
  type: ClusterIP`

	case "configmap":
		return `apiVersion: v1
kind: ConfigMap
metadata:
  name: CHANGEME-config
  namespace: default
data:
  config.yaml: |
    # Add your configuration here`

	case "secret":
		return `apiVersion: v1
kind: Secret
metadata:
  name: CHANGEME-secret
  namespace: default
type: Opaque
data:
  # Base64 encoded values
  key: dmFsdWU=`

	case "ingress":
		return `apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: CHANGEME-ingress
  namespace: default
spec:
  rules:
  - host: CHANGEME-host.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: CHANGEME-service
            port:
              number: 80`

	default:
		return ""
	}
}

func RegisterK8sTools(s *server.MCPServer) {
	k8sTool, err := NewK8sTool()
	if err != nil {
		// Log the error and proceed without native tool implementations
		logger.Get().Info("Failed to initialize Kubernetes client, falling back to kubectl commands",
			"level", "warn", "error", err.Error())
		// Here you could register the pure-kubectl versions of the tools as a fallback
		return
	}
	s.AddTool(mcp.NewTool("k8s_get_resources",
		mcp.WithDescription("Get Kubernetes resources using kubectl with enhanced native client support"),
		mcp.WithString("resource_type", mcp.Description("Type of resource (pod, service, deployment, etc.)"), mcp.Required()),
		mcp.WithString("resource_name", mcp.Description("Name of specific resource (optional)")),
		mcp.WithString("namespace", mcp.Description("Namespace to query (optional)")),
		mcp.WithString("all_namespaces", mcp.Description("Query all namespaces (true/false)")),
		mcp.WithString("output", mcp.Description("Output format (json, yaml, wide, etc.)")),
	), k8sTool.handleKubectlGetEnhanced)

	s.AddTool(mcp.NewTool("k8s_get_pod_logs",
		mcp.WithDescription("Get logs from a Kubernetes pod with enhanced native client support"),
		mcp.WithString("pod_name", mcp.Description("Name of the pod"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace of the pod (default: default)")),
		mcp.WithString("container", mcp.Description("Container name (for multi-container pods)")),
		mcp.WithNumber("tail_lines", mcp.Description("Number of lines to show from the end (default: 50)")),
	), k8sTool.handleKubectlLogsEnhanced)

	s.AddTool(mcp.NewTool("k8s_scale",
		mcp.WithDescription("Scale a Kubernetes deployment using native client"),
		mcp.WithString("name", mcp.Description("Name of the deployment"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace of the deployment (default: default)")),
		mcp.WithNumber("replicas", mcp.Description("Number of replicas"), mcp.Required()),
	), k8sTool.handleScaleDeployment)

	s.AddTool(mcp.NewTool("k8s_patch_resource",
		mcp.WithDescription("Patch a Kubernetes resource using strategic merge patch"),
		mcp.WithString("resource_type", mcp.Description("Type of resource (deployment, service, etc.)"), mcp.Required()),
		mcp.WithString("resource_name", mcp.Description("Name of the resource"), mcp.Required()),
		mcp.WithString("patch", mcp.Description("JSON patch to apply"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace of the resource (default: default)")),
	), k8sTool.handlePatchResource)

	s.AddTool(mcp.NewTool("k8s_apply_manifest",
		mcp.WithDescription("Apply a YAML manifest to the Kubernetes cluster"),
		mcp.WithString("manifest", mcp.Description("YAML manifest content"), mcp.Required()),
	), k8sTool.handleApplyManifest)

	s.AddTool(mcp.NewTool("k8s_delete_resource",
		mcp.WithDescription("Delete a Kubernetes resource using native client"),
		mcp.WithString("resource_type", mcp.Description("Type of resource (pod, service, deployment, etc.)"), mcp.Required()),
		mcp.WithString("resource_name", mcp.Description("Name of the resource"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace of the resource (default: default)")),
	), k8sTool.handleDeleteResource)

	s.AddTool(mcp.NewTool("k8s_check_service_connectivity",
		mcp.WithDescription("Check connectivity to a service using a temporary curl pod"),
		mcp.WithString("service_name", mcp.Description("Service name to test (e.g., my-service.my-namespace.svc.cluster.local:80)"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace to run the check from (default: default)")),
	), k8sTool.handleCheckServiceConnectivity)

	s.AddTool(mcp.NewTool("k8s_get_events",
		mcp.WithDescription("Get Kubernetes cluster events using native client"),
		mcp.WithString("namespace", mcp.Description("Namespace to query events from (optional, default: all namespaces)")),
	), k8sTool.handleGetEvents)

	s.AddTool(mcp.NewTool("k8s_execute_command",
		mcp.WithDescription("Execute a command inside a Kubernetes pod"),
		mcp.WithString("pod_name", mcp.Description("Name of the pod"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace of the pod (default: default)")),
		mcp.WithString("command", mcp.Description("Command to execute"), mcp.Required()),
	), k8sTool.handleExecCommand)

	s.AddTool(mcp.NewTool("k8s_get_available_api_resources",
		mcp.WithDescription("Get all available API resources from the Kubernetes cluster"),
	), k8sTool.handleGetAvailableAPIResources)

	s.AddTool(mcp.NewTool("k8s_get_cluster_configuration",
		mcp.WithDescription("Get the current kubectl cluster configuration"),
	), k8sTool.handleGetClusterConfiguration)

	s.AddTool(mcp.NewTool("k8s_rollout",
		mcp.WithDescription("Perform rollout operations on Kubernetes resources (history, pause, restart, resume, status, undo)"),
		mcp.WithString("action", mcp.Description("The rollout action to perform"), mcp.Required()),
		mcp.WithString("resource_type", mcp.Description("The type of resource to rollout (e.g., deployment)"), mcp.Required()),
		mcp.WithString("resource_name", mcp.Description("The name of the resource to rollout"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("The namespace of the resource")),
	), k8sTool.handleRollout)

	s.AddTool(mcp.NewTool("k8s_label_resource",
		mcp.WithDescription("Add or update labels on a Kubernetes resource"),
		mcp.WithString("resource_type", mcp.Description("The type of resource"), mcp.Required()),
		mcp.WithString("resource_name", mcp.Description("The name of the resource"), mcp.Required()),
		mcp.WithString("labels", mcp.Description("Space-separated key=value pairs for labels"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("The namespace of the resource")),
	), k8sTool.handleLabelResource)

	s.AddTool(mcp.NewTool("k8s_annotate_resource",
		mcp.WithDescription("Add or update annotations on a Kubernetes resource"),
		mcp.WithString("resource_type", mcp.Description("The type of resource"), mcp.Required()),
		mcp.WithString("resource_name", mcp.Description("The name of the resource"), mcp.Required()),
		mcp.WithString("annotations", mcp.Description("Space-separated key=value pairs for annotations"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("The namespace of the resource")),
	), k8sTool.handleAnnotateResource)

	s.AddTool(mcp.NewTool("k8s_remove_annotation",
		mcp.WithDescription("Remove an annotation from a Kubernetes resource"),
		mcp.WithString("resource_type", mcp.Description("The type of resource"), mcp.Required()),
		mcp.WithString("resource_name", mcp.Description("The name of the resource"), mcp.Required()),
		mcp.WithString("annotation_key", mcp.Description("The key of the annotation to remove"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("The namespace of the resource")),
	), k8sTool.handleRemoveAnnotation)

	s.AddTool(mcp.NewTool("k8s_remove_label",
		mcp.WithDescription("Remove a label from a Kubernetes resource"),
		mcp.WithString("resource_type", mcp.Description("The type of resource"), mcp.Required()),
		mcp.WithString("resource_name", mcp.Description("The name of the resource"), mcp.Required()),
		mcp.WithString("label_key", mcp.Description("The key of the label to remove"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("The namespace of the resource")),
	), k8sTool.handleRemoveLabel)

	s.AddTool(mcp.NewTool("k8s_create_resource",
		mcp.WithDescription("Create a Kubernetes resource from YAML content"),
		mcp.WithString("yaml_content", mcp.Description("YAML content of the resource"), mcp.Required()),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		yamlContent := mcp.ParseString(request, "yaml_content", "")

		if yamlContent == "" {
			return mcp.NewToolResultError("yaml_content is required"), nil
		}

		// Create temporary file
		tmpFile, err := os.CreateTemp("", "k8s-resource-*.yaml")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to create temp file: %v", err)), nil
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.WriteString(yamlContent); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to write to temp file: %v", err)), nil
		}
		tmpFile.Close()

		result, err := utils.RunCommandWithContext(ctx, "kubectl", []string{"create", "-f", tmpFile.Name()})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Create command failed: %v", err)), nil
		}

		return mcp.NewToolResultText(result), nil
	})

	s.AddTool(mcp.NewTool("k8s_create_resource_from_url",
		mcp.WithDescription("Create a Kubernetes resource from a URL pointing to a YAML manifest"),
		mcp.WithString("url", mcp.Description("The URL of the manifest"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("The namespace to create the resource in")),
	), k8sTool.handleCreateResourceFromURL)

	s.AddTool(mcp.NewTool("k8s_get_resource_yaml",
		mcp.WithDescription("Get the YAML representation of a Kubernetes resource"),
		mcp.WithString("resource_type", mcp.Description("Type of resource"), mcp.Required()),
		mcp.WithString("resource_name", mcp.Description("Name of the resource"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace of the resource (optional)")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		resourceType := mcp.ParseString(request, "resource_type", "")
		resourceName := mcp.ParseString(request, "resource_name", "")
		namespace := mcp.ParseString(request, "namespace", "")

		if resourceType == "" || resourceName == "" {
			return mcp.NewToolResultError("resource_type and resource_name are required"), nil
		}

		args := []string{"get", resourceType, resourceName, "-o", "yaml"}
		if namespace != "" {
			args = append(args, "-n", namespace)
		}

		result, err := utils.RunCommandWithContext(ctx, "kubectl", args)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Get YAML command failed: %v", err)), nil
		}

		return mcp.NewToolResultText(result), nil
	})

	s.AddTool(mcp.NewTool("k8s_describe_resource",
		mcp.WithDescription("Describe a Kubernetes resource in detail"),
		mcp.WithString("resource_type", mcp.Description("Type of resource (deployment, service, pod, node, etc.)"), mcp.Required()),
		mcp.WithString("resource_name", mcp.Description("Name of the resource"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace of the resource (optional)")),
	), k8sTool.handleKubectlDescribeTool)

	s.AddTool(mcp.NewTool("k8s_generate_resource_tool",
		mcp.WithDescription("Generate a Kubernetes resource YAML from a description"),
		mcp.WithString("resource_description", mcp.Description("Detailed description of the resource to generate"), mcp.Required()),
		mcp.WithString("resource_type", mcp.Description("Type of resource to generate (deployment, service, configmap, etc.)"), mcp.Required()),
	), k8sTool.handleGenerateResource)
}
