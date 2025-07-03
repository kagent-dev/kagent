package k8s

import (
	"context"
	_ "embed"
	"fmt"
	"maps"
	"math/rand"
	"os"
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/tools/pkg/logger"
	"github.com/kagent-dev/kagent/go/tools/pkg/utils"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

// K8sTool struct to hold the LLM model
type K8sTool struct {
	kubeconfig string
	llmModel   llms.Model
}

func NewK8sTool(llmModel llms.Model) *K8sTool {
	return &K8sTool{llmModel: llmModel}
}

func NewK8sToolWithConfig(kubeconfig string, llmModel llms.Model) *K8sTool {
	return &K8sTool{kubeconfig: kubeconfig, llmModel: llmModel}
}

// Enhanced kubectl get
func (k *K8sTool) handleKubectlGetEnhanced(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resourceType := mcp.ParseString(request, "resource_type", "")
	resourceName := mcp.ParseString(request, "resource_name", "")
	namespace := mcp.ParseString(request, "namespace", "")
	allNamespaces := mcp.ParseString(request, "all_namespaces", "") == "true"
	output := mcp.ParseString(request, "output", "json")

	if resourceType == "" {
		return mcp.NewToolResultError("resource_type parameter is required"), nil
	}

	args := []string{"get", resourceType}

	if resourceName != "" {
		args = append(args, resourceName)
	}

	if allNamespaces {
		args = append(args, "--all-namespaces")
	} else if namespace != "" {
		args = append(args, "-n", namespace)
	}

	if output != "" {
		args = append(args, "-o", output)
	} else {
		args = append(args, "-o", "json")
	}

	return k.runKubectlCommand(ctx, args)
}

// Get pod logs
func (k *K8sTool) handleKubectlLogsEnhanced(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	podName := mcp.ParseString(request, "pod_name", "")
	namespace := mcp.ParseString(request, "namespace", "default")
	container := mcp.ParseString(request, "container", "")
	tailLines := mcp.ParseInt(request, "tail_lines", 50)

	if podName == "" {
		return mcp.NewToolResultError("pod_name parameter is required"), nil
	}

	args := []string{"logs", podName, "-n", namespace}

	if container != "" {
		args = append(args, "-c", container)
	}

	if tailLines > 0 {
		args = append(args, "--tail", fmt.Sprintf("%d", tailLines))
	}

	return k.runKubectlCommand(ctx, args)
}

// Scale deployment
func (k *K8sTool) handleScaleDeployment(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	deploymentName := mcp.ParseString(request, "name", "")
	namespace := mcp.ParseString(request, "namespace", "default")
	replicas := mcp.ParseInt(request, "replicas", 1)

	if deploymentName == "" {
		return mcp.NewToolResultError("name parameter is required"), nil
	}

	args := []string{"scale", "deployment", deploymentName, "--replicas", fmt.Sprintf("%d", replicas), "-n", namespace}

	return k.runKubectlCommand(ctx, args)
}

// Patch resource
func (k *K8sTool) handlePatchResource(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resourceType := mcp.ParseString(request, "resource_type", "")
	resourceName := mcp.ParseString(request, "resource_name", "")
	patch := mcp.ParseString(request, "patch", "")
	namespace := mcp.ParseString(request, "namespace", "default")

	if resourceType == "" || resourceName == "" || patch == "" {
		return mcp.NewToolResultError("resource_type, resource_name, and patch parameters are required"), nil
	}

	args := []string{"patch", resourceType, resourceName, "-p", patch, "-n", namespace}

	return k.runKubectlCommand(ctx, args)
}

// Apply manifest from content
func (k *K8sTool) handleApplyManifest(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	manifest := mcp.ParseString(request, "manifest", "")

	if manifest == "" {
		return mcp.NewToolResultError("manifest parameter is required"), nil
	}

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

// Delete resource
func (k *K8sTool) handleDeleteResource(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resourceType := mcp.ParseString(request, "resource_type", "")
	resourceName := mcp.ParseString(request, "resource_name", "")
	namespace := mcp.ParseString(request, "namespace", "default")

	if resourceType == "" || resourceName == "" {
		return mcp.NewToolResultError("resource_type and resource_name parameters are required"), nil
	}

	args := []string{"delete", resourceType, resourceName, "-n", namespace}

	return k.runKubectlCommand(ctx, args)
}

// Check service connectivity
func (k *K8sTool) handleCheckServiceConnectivity(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	serviceName := mcp.ParseString(request, "service_name", "")
	namespace := mcp.ParseString(request, "namespace", "default")

	if serviceName == "" {
		return mcp.NewToolResultError("service_name parameter is required"), nil
	}

	// Create a temporary curl pod for connectivity check
	podName := fmt.Sprintf("curl-test-%d", rand.Intn(10000))
	defer k.runKubectlCommand(ctx, []string{"delete", "pod", podName, "-n", namespace, "--ignore-not-found"})

	// Create the curl pod
	_, err := k.runKubectlCommand(ctx, []string{"run", podName, "--image=curlimages/curl", "-n", namespace, "--restart=Never", "--", "sleep", "3600"})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to create curl pod: %v", err)), nil
	}

	// Wait for pod to be ready
	_, err = k.runKubectlCommand(ctx, []string{"wait", "--for=condition=ready", "pod/" + podName, "-n", namespace, "--timeout=60s"})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to wait for curl pod: %v", err)), nil
	}

	// Execute curl command
	return k.runKubectlCommand(ctx, []string{"exec", podName, "-n", namespace, "--", "curl", "-s", serviceName})
}

// Get cluster events
func (k *K8sTool) handleGetEvents(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	namespace := mcp.ParseString(request, "namespace", "")

	args := []string{"get", "events", "-o", "json"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	} else {
		args = append(args, "--all-namespaces")
	}

	return k.runKubectlCommand(ctx, args)
}

// Execute command in pod
func (k *K8sTool) handleExecCommand(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	podName := mcp.ParseString(request, "pod_name", "")
	namespace := mcp.ParseString(request, "namespace", "default")
	command := mcp.ParseString(request, "command", "")

	if podName == "" || command == "" {
		return mcp.NewToolResultError("pod_name and command parameters are required"), nil
	}

	args := []string{"exec", podName, "-n", namespace, "--", command}

	return k.runKubectlCommand(ctx, args)
}

// Get available API resources
func (k *K8sTool) handleGetAvailableAPIResources(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return k.runKubectlCommand(ctx, []string{"api-resources", "-o", "json"})
}

// Kubectl get tool (fallback)
func (k *K8sTool) handleKubectlGetTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return k.handleKubectlGetEnhanced(ctx, request)
}

// Kubectl describe tool
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

// Rollout operations
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

// Get cluster configuration
func (k *K8sTool) handleGetClusterConfiguration(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return k.runKubectlCommand(ctx, []string{"config", "view"})
}

// Remove annotation
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

// Remove label
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

// Annotate resource
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

// Label resource
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

// Create resource from URL
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

// Resource generation embeddings
var (
	//go:embed resources/istio/peer_auth.md
	istioAuthPolicy string

	//go:embed resources/istio/virtual_service.md
	istioVirtualService string

	//go:embed resources/gw_api/reference_grant.md
	gatewayApiReferenceGrant string

	//go:embed resources/gw_api/gateway.md
	gatewayApiGateway string

	//go:embed resources/gw_api/http_route.md
	gatewayApiHttpRoute string

	//go:embed resources/gw_api/gateway_class.md
	gatewayApiGatewayClass string

	//go:embed resources/gw_api/grpc_route.md
	gatewayApiGrpcRoute string

	//go:embed resources/argo/rollout.md
	argoRollout string

	//go:embed resources/argo/analysis_template.md
	argoAnalaysisTempalte string

	resourceMap = map[string]string{
		"istio_auth_policy":           istioAuthPolicy,
		"istio_virtual_service":       istioVirtualService,
		"gateway_api_reference_grant": gatewayApiReferenceGrant,
		"gateway_api_gateway":         gatewayApiGateway,
		"gateway_api_http_route":      gatewayApiHttpRoute,
		"gateway_api_gateway_class":   gatewayApiGatewayClass,
		"gateway_api_grpc_route":      gatewayApiGrpcRoute,
		"argo_rollout":                argoRollout,
		"argo_analysis_template":      argoAnalaysisTempalte,
	}

	resourceTypes = maps.Keys(resourceMap)
)

// Generate resource using LLM
func (k *K8sTool) handleGenerateResource(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resourceType := mcp.ParseString(request, "resource_type", "")
	resourceDescription := mcp.ParseString(request, "resource_description", "")

	if resourceType == "" || resourceDescription == "" {
		return mcp.NewToolResultError("resource_type and resource_description parameters are required"), nil
	}

	systemPrompt, ok := resourceMap[resourceType]
	if !ok {
		return mcp.NewToolResultError(fmt.Sprintf("resource type %s not found", resourceType)), nil
	}

	// Use the injected LLM model if available, otherwise create a new OpenAI instance
	if k.llmModel == nil {
		return mcp.NewToolResultError("No LLM client present, can't generate resource"), nil
	}
	llm := k.llmModel

	contents := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeSystem,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: systemPrompt},
			},
		},
		{
			Role: llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: resourceDescription},
			},
		},
	}

	resp, err := llm.GenerateContent(ctx, contents, llms.WithModel("gpt-4o-mini"))
	if err != nil {
		return mcp.NewToolResultError("failed to generate content: " + err.Error()), nil
	}

	choices := resp.Choices
	if len(choices) < 1 {
		return mcp.NewToolResultError("empty response from model"), nil
	}
	c1 := choices[0]
	return mcp.NewToolResultText(c1.Content), nil
}

// Helper function to run kubectl commands
func (k *K8sTool) runKubectlCommand(ctx context.Context, args []string) (*mcp.CallToolResult, error) {
	if k.kubeconfig != "" {
		args = append([]string{"--kubeconfig", k.kubeconfig}, args...)
	}
	result, err := utils.RunCommandWithContext(ctx, "kubectl", args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(result), nil
}

// RegisterK8sTools registers all k8s tools with the MCP server
func RegisterK8sTools(s *server.MCPServer, kubeconfig string) {
	var llm llms.Model
	if openAiClient, err := openai.New(); err == nil {
		llm = openAiClient
	} else {
		logger.Get().Error(err, "Failed to initialize OpenAI LLM, k8s_generate_resource tool will not be available")
	}

	k8sTool := NewK8sToolWithConfig(kubeconfig, llm)

	s.AddTool(mcp.NewTool("k8s_get_resources",
		mcp.WithDescription("Get Kubernetes resources using kubectl"),
		mcp.WithString("resource_type", mcp.Description("Type of resource (pod, service, deployment, etc.)"), mcp.Required()),
		mcp.WithString("resource_name", mcp.Description("Name of specific resource (optional)")),
		mcp.WithString("namespace", mcp.Description("Namespace to query (optional)")),
		mcp.WithString("all_namespaces", mcp.Description("Query all namespaces (true/false)")),
		mcp.WithString("output", mcp.Description("Output format (json, yaml, wide, etc.)")),
	), k8sTool.handleKubectlGetEnhanced)

	s.AddTool(mcp.NewTool("k8s_get_pod_logs",
		mcp.WithDescription("Get logs from a Kubernetes pod"),
		mcp.WithString("pod_name", mcp.Description("Name of the pod"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace of the pod (default: default)")),
		mcp.WithString("container", mcp.Description("Container name (for multi-container pods)")),
		mcp.WithNumber("tail_lines", mcp.Description("Number of lines to show from the end (default: 50)")),
	), k8sTool.handleKubectlLogsEnhanced)

	s.AddTool(mcp.NewTool("k8s_scale",
		mcp.WithDescription("Scale a Kubernetes deployment"),
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
		mcp.WithDescription("Delete a Kubernetes resource"),
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
		mcp.WithDescription("Get Kubernetes cluster events"),
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

	s.AddTool(mcp.NewTool("k8s_generate_resource",
		mcp.WithDescription("Generate a Kubernetes resource YAML from a description"),
		mcp.WithString("resource_description", mcp.Description("Detailed description of the resource to generate"), mcp.Required()),
		mcp.WithString("resource_type", mcp.Description(fmt.Sprintf("Type of resource to generate (%s)", strings.Join(slices.Collect(resourceTypes), ", "))), mcp.Required()),
	), k8sTool.handleGenerateResource)
}
