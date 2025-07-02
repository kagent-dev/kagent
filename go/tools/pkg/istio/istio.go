package istio

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/tools/pkg/utils"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Istio proxy status
func handleIstioProxyStatus(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	podName := mcp.ParseString(request, "pod_name", "")
	namespace := mcp.ParseString(request, "namespace", "")

	args := []string{"proxy-status"}

	if namespace != "" {
		args = append(args, "-n", namespace)
	}

	if podName != "" {
		args = append(args, podName)
	}

	result, err := utils.RunCommandWithContext(ctx, "istioctl", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("istioctl proxy-status failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Istio proxy config
func handleIstioProxyConfig(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	podName := mcp.ParseString(request, "pod_name", "")
	namespace := mcp.ParseString(request, "namespace", "")
	configType := mcp.ParseString(request, "config_type", "all")

	if podName == "" {
		return mcp.NewToolResultError("pod_name parameter is required"), nil
	}

	args := []string{"proxy-config", configType}

	if namespace != "" {
		args = append(args, fmt.Sprintf("%s.%s", podName, namespace))
	} else {
		args = append(args, podName)
	}

	result, err := utils.RunCommandWithContext(ctx, "istioctl", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("istioctl proxy-config failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Istio install
func handleIstioInstall(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	profile := mcp.ParseString(request, "profile", "default")

	args := []string{"install", "--set", fmt.Sprintf("profile=%s", profile), "-y"}

	result, err := utils.RunCommandWithContext(ctx, "istioctl", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("istioctl install failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Istio generate manifest
func handleIstioGenerateManifest(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	profile := mcp.ParseString(request, "profile", "default")

	args := []string{"manifest", "generate", "--set", fmt.Sprintf("profile=%s", profile)}

	result, err := utils.RunCommandWithContext(ctx, "istioctl", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("istioctl manifest generate failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Istio analyze
func handleIstioAnalyzeClusterConfiguration(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	namespace := mcp.ParseString(request, "namespace", "")
	allNamespaces := mcp.ParseString(request, "all_namespaces", "") == "true"

	args := []string{"analyze"}

	if allNamespaces {
		args = append(args, "-A")
	} else if namespace != "" {
		args = append(args, "-n", namespace)
	}

	result, err := utils.RunCommandWithContext(ctx, "istioctl", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("istioctl analyze failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Istio version
func handleIstioVersion(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	short := mcp.ParseString(request, "short", "") == "true"

	args := []string{"version"}

	if short {
		args = append(args, "--short")
	}

	result, err := utils.RunCommandWithContext(ctx, "istioctl", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("istioctl version failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Istio remote clusters
func handleIstioRemoteClusters(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := []string{"remote-clusters"}

	result, err := utils.RunCommandWithContext(ctx, "istioctl", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("istioctl remote-clusters failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Waypoint list
func handleWaypointList(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	namespace := mcp.ParseString(request, "namespace", "")
	allNamespaces := mcp.ParseString(request, "all_namespaces", "") == "true"

	args := []string{"waypoint", "list"}

	if allNamespaces {
		args = append(args, "-A")
	} else if namespace != "" {
		args = append(args, "-n", namespace)
	}

	result, err := utils.RunCommandWithContext(ctx, "istioctl", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("istioctl waypoint list failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Waypoint generate
func handleWaypointGenerate(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	namespace := mcp.ParseString(request, "namespace", "")
	name := mcp.ParseString(request, "name", "waypoint")
	trafficType := mcp.ParseString(request, "traffic_type", "all")

	if namespace == "" {
		return mcp.NewToolResultError("namespace parameter is required"), nil
	}

	args := []string{"waypoint", "generate"}

	if name != "" {
		args = append(args, name)
	}

	args = append(args, "-n", namespace)

	if trafficType != "" {
		args = append(args, "--for", trafficType)
	}

	result, err := utils.RunCommandWithContext(ctx, "istioctl", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("istioctl waypoint generate failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Waypoint apply
func handleWaypointApply(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	namespace := mcp.ParseString(request, "namespace", "")
	enrollNamespace := mcp.ParseString(request, "enroll_namespace", "") == "true"

	if namespace == "" {
		return mcp.NewToolResultError("namespace parameter is required"), nil
	}

	args := []string{"waypoint", "apply", "-n", namespace}

	if enrollNamespace {
		args = append(args, "--enroll-namespace")
	}

	result, err := utils.RunCommandWithContext(ctx, "istioctl", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("istioctl waypoint apply failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Waypoint delete
func handleWaypointDelete(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	namespace := mcp.ParseString(request, "namespace", "")
	names := mcp.ParseString(request, "names", "")
	all := mcp.ParseString(request, "all", "") == "true"

	if namespace == "" {
		return mcp.NewToolResultError("namespace parameter is required"), nil
	}

	args := []string{"waypoint", "delete"}

	if all {
		args = append(args, "--all")
	} else if names != "" {
		namesList := strings.Split(names, ",")
		for _, name := range namesList {
			args = append(args, strings.TrimSpace(name))
		}
	}

	args = append(args, "-n", namespace)

	result, err := utils.RunCommandWithContext(ctx, "istioctl", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("istioctl waypoint delete failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Waypoint status
func handleWaypointStatus(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	namespace := mcp.ParseString(request, "namespace", "")
	name := mcp.ParseString(request, "name", "")

	if namespace == "" {
		return mcp.NewToolResultError("namespace parameter is required"), nil
	}

	args := []string{"waypoint", "status"}

	if name != "" {
		args = append(args, name)
	}

	args = append(args, "-n", namespace)

	result, err := utils.RunCommandWithContext(ctx, "istioctl", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("istioctl waypoint status failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Ztunnel config
func handleZtunnelConfig(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	namespace := mcp.ParseString(request, "namespace", "")
	configType := mcp.ParseString(request, "config_type", "all")

	args := []string{"ztunnel-config", configType}

	if namespace != "" {
		args = append(args, "-n", namespace)
	}

	result, err := utils.RunCommandWithContext(ctx, "istioctl", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("istioctl ztunnel-config failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Register Istio tools
func RegisterIstioTools(s *server.MCPServer) {
	// Istio proxy status
	s.AddTool(mcp.NewTool("istio_proxy_status",
		mcp.WithDescription("Get Envoy proxy status for pods, retrieves last sent and acknowledged xDS sync from Istiod to each Envoy in the mesh"),
		mcp.WithString("pod_name", mcp.Description("Name of the pod to get proxy status for")),
		mcp.WithString("namespace", mcp.Description("Namespace of the pod")),
	), handleIstioProxyStatus)

	// Istio proxy config
	s.AddTool(mcp.NewTool("istio_proxy_config",
		mcp.WithDescription("Get specific proxy configuration for a single pod"),
		mcp.WithString("pod_name", mcp.Description("Name of the pod to get proxy configuration for"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace of the pod")),
		mcp.WithString("config_type", mcp.Description("Type of configuration (all, bootstrap, cluster, ecds, listener, log, route, secret)")),
	), handleIstioProxyConfig)

	// Istio install
	s.AddTool(mcp.NewTool("istio_install_istio",
		mcp.WithDescription("Install Istio with a specified configuration profile"),
		mcp.WithString("profile", mcp.Description("Istio configuration profile (ambient, default, demo, minimal, empty)")),
	), handleIstioInstall)

	// Istio generate manifest
	s.AddTool(mcp.NewTool("istio_generate_manifest",
		mcp.WithDescription("Generate an Istio install manifest"),
		mcp.WithString("profile", mcp.Description("Istio configuration profile (ambient, default, demo, minimal, empty)")),
	), handleIstioGenerateManifest)

	// Istio analyze
	s.AddTool(mcp.NewTool("istio_analyze_cluster_configuration",
		mcp.WithDescription("Analyze live cluster configuration for potential issues"),
		mcp.WithString("namespace", mcp.Description("Namespace to analyze")),
		mcp.WithString("all_namespaces", mcp.Description("Analyze all namespaces (true/false)")),
	), handleIstioAnalyzeClusterConfiguration)

	// Istio version
	s.AddTool(mcp.NewTool("istio_version",
		mcp.WithDescription("Get Istio CLI client version, control plane and data plane versions"),
		mcp.WithString("short", mcp.Description("Show short version format (true/false)")),
	), handleIstioVersion)

	// Istio remote clusters
	s.AddTool(mcp.NewTool("istio_remote_clusters",
		mcp.WithDescription("List remote clusters each istiod instance is connected to"),
	), handleIstioRemoteClusters)

	// Waypoint list
	s.AddTool(mcp.NewTool("istio_list_waypoints",
		mcp.WithDescription("List managed waypoint configurations in the cluster"),
		mcp.WithString("namespace", mcp.Description("Namespace to list waypoints for")),
		mcp.WithString("all_namespaces", mcp.Description("List waypoints for all namespaces (true/false)")),
	), handleWaypointList)

	// Waypoint generate
	s.AddTool(mcp.NewTool("istio_generate_waypoint",
		mcp.WithDescription("Generate a waypoint configuration as YAML"),
		mcp.WithString("namespace", mcp.Description("Namespace to generate the waypoint for"), mcp.Required()),
		mcp.WithString("name", mcp.Description("Name of the waypoint to generate")),
		mcp.WithString("traffic_type", mcp.Description("Traffic type for the waypoint (all, inbound, outbound)")),
	), handleWaypointGenerate)

	// Waypoint apply
	s.AddTool(mcp.NewTool("istio_apply_waypoint",
		mcp.WithDescription("Apply a waypoint configuration to a cluster"),
		mcp.WithString("namespace", mcp.Description("Namespace to apply the waypoint to"), mcp.Required()),
		mcp.WithString("enroll_namespace", mcp.Description("Label the namespace with the waypoint name (true/false)")),
	), handleWaypointApply)

	// Waypoint delete
	s.AddTool(mcp.NewTool("istio_delete_waypoint",
		mcp.WithDescription("Delete waypoint configurations from a cluster"),
		mcp.WithString("namespace", mcp.Description("Namespace to delete waypoints from"), mcp.Required()),
		mcp.WithString("names", mcp.Description("Comma-separated list of waypoint names to delete")),
		mcp.WithString("all", mcp.Description("Delete all waypoints in the namespace (true/false)")),
	), handleWaypointDelete)

	// Waypoint status
	s.AddTool(mcp.NewTool("istio_waypoint_status",
		mcp.WithDescription("Get status of a waypoint"),
		mcp.WithString("namespace", mcp.Description("Namespace of the waypoint"), mcp.Required()),
		mcp.WithString("name", mcp.Description("Name of the waypoint to get status for")),
	), handleWaypointStatus)

	// Ztunnel config
	s.AddTool(mcp.NewTool("istio_ztunnel_config",
		mcp.WithDescription("Get ztunnel configuration"),
		mcp.WithString("namespace", mcp.Description("Namespace of the pod")),
		mcp.WithString("config_type", mcp.Description("Type of configuration (all, bootstrap, cluster, ecds, listener, log, route, secret)")),
	), handleZtunnelConfig)
}
