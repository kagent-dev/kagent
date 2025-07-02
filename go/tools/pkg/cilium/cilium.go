package cilium

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/tools/pkg/utils"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func runCiliumCliWithContext(ctx context.Context, args ...string) (string, error) {
	return utils.RunCommandWithContext(ctx, "cilium", args)
}

func handleCiliumStatusAndVersion(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	status, err := runCiliumCliWithContext(ctx, "status")
	if err != nil {
		return mcp.NewToolResultError("Error getting Cilium status: " + err.Error()), nil
	}

	version, err := runCiliumCliWithContext(ctx, "version")
	if err != nil {
		return mcp.NewToolResultError("Error getting Cilium version: " + err.Error()), nil
	}

	result := status + "\n" + version
	return mcp.NewToolResultText(result), nil
}

func handleUpgradeCilium(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	clusterName := mcp.ParseString(request, "cluster_name", "")
	datapathMode := mcp.ParseString(request, "datapath_mode", "")

	args := []string{"upgrade"}
	if clusterName != "" {
		args = append(args, "--cluster-name", clusterName)
	}
	if datapathMode != "" {
		args = append(args, "--datapath-mode", datapathMode)
	}

	output, err := runCiliumCliWithContext(ctx, args...)
	if err != nil {
		return mcp.NewToolResultError("Error upgrading Cilium: " + err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func handleInstallCilium(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	clusterName := mcp.ParseString(request, "cluster_name", "")
	clusterID := mcp.ParseString(request, "cluster_id", "")
	datapathMode := mcp.ParseString(request, "datapath_mode", "")

	args := []string{"install"}
	if clusterName != "" {
		args = append(args, "--set", "cluster.name="+clusterName)
	}
	if clusterID != "" {
		args = append(args, "--set", "cluster.id="+clusterID)
	}
	if datapathMode != "" {
		args = append(args, "--datapath-mode", datapathMode)
	}

	output, err := runCiliumCliWithContext(ctx, args...)
	if err != nil {
		return mcp.NewToolResultError("Error installing Cilium: " + err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func handleUninstallCilium(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	output, err := runCiliumCliWithContext(ctx, "uninstall")
	if err != nil {
		return mcp.NewToolResultError("Error uninstalling Cilium: " + err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func handleConnectToRemoteCluster(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	clusterName := mcp.ParseString(request, "cluster_name", "")
	context := mcp.ParseString(request, "context", "")

	if clusterName == "" {
		return mcp.NewToolResultError("cluster_name parameter is required"), nil
	}

	args := []string{"clustermesh", "connect", "--destination-cluster", clusterName}
	if context != "" {
		args = append(args, "--destination-context", context)
	}

	output, err := runCiliumCliWithContext(ctx, args...)
	if err != nil {
		return mcp.NewToolResultError("Error connecting to remote cluster: " + err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func handleDisconnectRemoteCluster(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	clusterName := mcp.ParseString(request, "cluster_name", "")

	if clusterName == "" {
		return mcp.NewToolResultError("cluster_name parameter is required"), nil
	}

	args := []string{"clustermesh", "disconnect", "--destination-cluster", clusterName}

	output, err := runCiliumCliWithContext(ctx, args...)
	if err != nil {
		return mcp.NewToolResultError("Error disconnecting from remote cluster: " + err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func handleListBGPPeers(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	output, err := runCiliumCliWithContext(ctx, "bgp", "peers")
	if err != nil {
		return mcp.NewToolResultError("Error listing BGP peers: " + err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func handleListBGPRoutes(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	output, err := runCiliumCliWithContext(ctx, "bgp", "routes")
	if err != nil {
		return mcp.NewToolResultError("Error listing BGP routes: " + err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func handleShowClusterMeshStatus(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	output, err := runCiliumCliWithContext(ctx, "clustermesh", "status")
	if err != nil {
		return mcp.NewToolResultError("Error getting cluster mesh status: " + err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func handleShowFeaturesStatus(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	output, err := runCiliumCliWithContext(ctx, "features", "status")
	if err != nil {
		return mcp.NewToolResultError("Error getting features status: " + err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func handleToggleHubble(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	enableStr := mcp.ParseString(request, "enable", "true")
	enable := enableStr == "true"

	var action string
	if enable {
		action = "enable"
	} else {
		action = "disable"
	}

	output, err := runCiliumCliWithContext(ctx, "hubble", action)
	if err != nil {
		return mcp.NewToolResultError("Error toggling Hubble: " + err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func handleToggleClusterMesh(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	enableStr := mcp.ParseString(request, "enable", "true")
	enable := enableStr == "true"

	var action string
	if enable {
		action = "enable"
	} else {
		action = "disable"
	}

	output, err := runCiliumCliWithContext(ctx, "clustermesh", action)
	if err != nil {
		return mcp.NewToolResultError("Error toggling cluster mesh: " + err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func RegisterCiliumTools(s *server.MCPServer) {
	// Register debug tools
	RegisterCiliumDbgTools(s)

	// Register main Cilium tools
	s.AddTool(mcp.NewTool("cilium_status_and_version",
		mcp.WithDescription("Get the status and version of Cilium installation"),
	), handleCiliumStatusAndVersion)

	s.AddTool(mcp.NewTool("cilium_upgrade_cilium",
		mcp.WithDescription("Upgrade Cilium on the cluster"),
		mcp.WithString("cluster_name", mcp.Description("The name of the cluster to upgrade Cilium on")),
		mcp.WithString("datapath_mode", mcp.Description("The datapath mode to use for Cilium (tunnel, native, aws-eni, gke, azure, aks-byocni)")),
	), handleUpgradeCilium)

	s.AddTool(mcp.NewTool("cilium_install_cilium",
		mcp.WithDescription("Install Cilium on the cluster"),
		mcp.WithString("cluster_name", mcp.Description("The name of the cluster to install Cilium on")),
		mcp.WithString("cluster_id", mcp.Description("The ID of the cluster to install Cilium on")),
		mcp.WithString("datapath_mode", mcp.Description("The datapath mode to use for Cilium (tunnel, native, aws-eni, gke, azure, aks-byocni)")),
	), handleInstallCilium)

	s.AddTool(mcp.NewTool("cilium_uninstall_cilium",
		mcp.WithDescription("Uninstall Cilium from the cluster"),
	), handleUninstallCilium)

	s.AddTool(mcp.NewTool("cilium_connect_to_remote_cluster",
		mcp.WithDescription("Connect to a remote cluster for cluster mesh"),
		mcp.WithString("cluster_name", mcp.Description("The name of the destination cluster"), mcp.Required()),
		mcp.WithString("context", mcp.Description("The kubectl context for the destination cluster")),
	), handleConnectToRemoteCluster)

	s.AddTool(mcp.NewTool("cilium_disconnect_remote_cluster",
		mcp.WithDescription("Disconnect from a remote cluster"),
		mcp.WithString("cluster_name", mcp.Description("The name of the destination cluster"), mcp.Required()),
	), handleDisconnectRemoteCluster)

	s.AddTool(mcp.NewTool("cilium_list_bgp_peers",
		mcp.WithDescription("List BGP peers"),
	), handleListBGPPeers)

	s.AddTool(mcp.NewTool("cilium_list_bgp_routes",
		mcp.WithDescription("List BGP routes"),
	), handleListBGPRoutes)

	s.AddTool(mcp.NewTool("cilium_show_cluster_mesh_status",
		mcp.WithDescription("Show cluster mesh status"),
	), handleShowClusterMeshStatus)

	s.AddTool(mcp.NewTool("cilium_show_features_status",
		mcp.WithDescription("Show Cilium features status"),
	), handleShowFeaturesStatus)

	s.AddTool(mcp.NewTool("cilium_toggle_hubble",
		mcp.WithDescription("Enable or disable Hubble"),
		mcp.WithString("enable", mcp.Description("Set to 'true' to enable, 'false' to disable")),
	), handleToggleHubble)

	s.AddTool(mcp.NewTool("cilium_toggle_cluster_mesh",
		mcp.WithDescription("Enable or disable cluster mesh"),
		mcp.WithString("enable", mcp.Description("Set to 'true' to enable, 'false' to disable")),
	), handleToggleClusterMesh)

	// Add tools that are also needed by cilium-manager agent
	s.AddTool(mcp.NewTool("cilium_get_daemon_status",
		mcp.WithDescription("Get the status of the Cilium daemon for the cluster"),
		mcp.WithString("show_all_addresses", mcp.Description("Whether to show all addresses")),
		mcp.WithString("show_all_clusters", mcp.Description("Whether to show all clusters")),
		mcp.WithString("show_all_controllers", mcp.Description("Whether to show all controllers")),
		mcp.WithString("show_health", mcp.Description("Whether to show health")),
		mcp.WithString("show_all_nodes", mcp.Description("Whether to show all nodes")),
		mcp.WithString("show_all_redirects", mcp.Description("Whether to show all redirects")),
		mcp.WithString("brief", mcp.Description("Whether to show a brief status")),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the daemon status for")),
	), handleGetDaemonStatus)

	s.AddTool(mcp.NewTool("cilium_get_endpoints_list",
		mcp.WithDescription("Get the list of all endpoints in the cluster"),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the endpoints list for")),
	), handleGetEndpointsList)

	s.AddTool(mcp.NewTool("cilium_get_endpoint_details",
		mcp.WithDescription("List the details of an endpoint in the cluster"),
		mcp.WithString("endpoint_id", mcp.Description("The ID of the endpoint to get details for")),
		mcp.WithString("labels", mcp.Description("The labels of the endpoint to get details for")),
		mcp.WithString("output_format", mcp.Description("The output format of the endpoint details (json, yaml, jsonpath)")),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the endpoint details for")),
	), handleGetEndpointDetails)

	s.AddTool(mcp.NewTool("cilium_show_configuration_options",
		mcp.WithDescription("Show Cilium configuration options"),
		mcp.WithString("list_all", mcp.Description("Whether to list all configuration options")),
		mcp.WithString("list_read_only", mcp.Description("Whether to list read-only configuration options")),
		mcp.WithString("list_options", mcp.Description("Whether to list options")),
		mcp.WithString("node_name", mcp.Description("The name of the node to show the configuration options for")),
	), handleShowConfigurationOptions)

	s.AddTool(mcp.NewTool("cilium_toggle_configuration_option",
		mcp.WithDescription("Toggle a Cilium configuration option"),
		mcp.WithString("option", mcp.Description("The option to toggle"), mcp.Required()),
		mcp.WithString("value", mcp.Description("The value to set the option to (true/false)"), mcp.Required()),
		mcp.WithString("node_name", mcp.Description("The name of the node to toggle the configuration option for")),
	), handleToggleConfigurationOption)

	s.AddTool(mcp.NewTool("cilium_list_services",
		mcp.WithDescription("List services for the cluster"),
		mcp.WithString("show_cluster_mesh_affinity", mcp.Description("Whether to show cluster mesh affinity")),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the services for")),
	), handleListServices)

	s.AddTool(mcp.NewTool("cilium_get_service_information",
		mcp.WithDescription("Get information about a service in the cluster"),
		mcp.WithString("service_id", mcp.Description("The ID of the service to get information about"), mcp.Required()),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the service information for")),
	), handleGetServiceInformation)

	s.AddTool(mcp.NewTool("cilium_update_service",
		mcp.WithDescription("Update a service in the cluster"),
		mcp.WithString("backend_weights", mcp.Description("The backend weights to update the service with")),
		mcp.WithString("backends", mcp.Description("The backends to update the service with"), mcp.Required()),
		mcp.WithString("frontend", mcp.Description("The frontend to update the service with"), mcp.Required()),
		mcp.WithString("id", mcp.Description("The ID of the service to update"), mcp.Required()),
		mcp.WithString("k8s_cluster_internal", mcp.Description("Whether to update the k8s cluster internal flag")),
		mcp.WithString("k8s_ext_traffic_policy", mcp.Description("The k8s ext traffic policy to update the service with")),
		mcp.WithString("k8s_external", mcp.Description("Whether to update the k8s external flag")),
		mcp.WithString("k8s_host_port", mcp.Description("Whether to update the k8s host port flag")),
		mcp.WithString("k8s_int_traffic_policy", mcp.Description("The k8s int traffic policy to update the service with")),
		mcp.WithString("k8s_load_balancer", mcp.Description("Whether to update the k8s load balancer flag")),
		mcp.WithString("k8s_node_port", mcp.Description("Whether to update the k8s node port flag")),
		mcp.WithString("local_redirect", mcp.Description("Whether to update the local redirect flag")),
		mcp.WithString("protocol", mcp.Description("The protocol to update the service with")),
		mcp.WithString("states", mcp.Description("The states to update the service with")),
		mcp.WithString("node_name", mcp.Description("The name of the node to update the service on")),
	), handleUpdateService)
}

// -- Debug Tools --

func getCiliumPodName(nodeName string) (string, error) {
	return getCiliumPodNameWithContext(context.Background(), nodeName)
}

func getCiliumPodNameWithContext(ctx context.Context, nodeName string) (string, error) {
	args := []string{"get", "pod", "-l", "k8s-app=cilium", "-o", "name", "-n", "kube-system"}
	if nodeName != "" {
		args = append(args, "--field-selector", "spec.nodeName="+nodeName)
	}
	podName, err := utils.RunCommandWithContext(ctx, "kubectl", args)
	if err != nil {
		return "", fmt.Errorf("failed to get cilium pod name: %v", err)
	}
	if podName == "" {
		return "", fmt.Errorf("no cilium pod found")
	}
	return strings.TrimSpace(podName), nil
}

func runCiliumDbgCommand(command, nodeName string) (string, error) {
	return runCiliumDbgCommandWithContext(context.Background(), command, nodeName)
}

func runCiliumDbgCommandWithContext(ctx context.Context, command, nodeName string) (string, error) {
	podName, err := getCiliumPodNameWithContext(ctx, nodeName)
	if err != nil {
		return "", err
	}
	cmdParts := strings.Fields(command)
	args := []string{"exec", "-it", podName, "-n", "kube-system", "--", "cilium-dbg"}
	args = append(args, cmdParts...)
	return utils.RunCommandWithContext(ctx, "kubectl", args)
}

func handleGetEndpointDetails(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	endpointID := mcp.ParseString(request, "endpoint_id", "")
	labels := mcp.ParseString(request, "labels", "")
	outputFormat := mcp.ParseString(request, "output_format", "json")
	nodeName := mcp.ParseString(request, "node_name", "")

	var cmd string
	if labels != "" {
		cmd = fmt.Sprintf("endpoint get -l %s -o %s", labels, outputFormat)
	} else if endpointID != "" {
		cmd = fmt.Sprintf("endpoint get %s -o %s", endpointID, outputFormat)
	} else {
		return mcp.NewToolResultError("either endpoint_id or labels must be provided"), nil
	}

	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get endpoint details: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleGetEndpointLogs(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	endpointID := mcp.ParseString(request, "endpoint_id", "")
	nodeName := mcp.ParseString(request, "node_name", "")

	if endpointID == "" {
		return mcp.NewToolResultError("endpoint_id parameter is required"), nil
	}

	cmd := fmt.Sprintf("endpoint logs %s", endpointID)
	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get endpoint logs: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleGetEndpointHealth(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	endpointID := mcp.ParseString(request, "endpoint_id", "")
	nodeName := mcp.ParseString(request, "node_name", "")

	if endpointID == "" {
		return mcp.NewToolResultError("endpoint_id parameter is required"), nil
	}

	cmd := fmt.Sprintf("endpoint health %s", endpointID)
	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get endpoint health: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleManageEndpointLabels(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	endpointID := mcp.ParseString(request, "endpoint_id", "")
	labels := mcp.ParseString(request, "labels", "")
	action := mcp.ParseString(request, "action", "add") // Default to add
	nodeName := mcp.ParseString(request, "node_name", "")

	if endpointID == "" || labels == "" {
		return mcp.NewToolResultError("endpoint_id and labels parameters are required"), nil
	}

	cmd := fmt.Sprintf("endpoint labels %s --%s %s", endpointID, action, labels)
	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to manage endpoint labels: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleManageEndpointConfiguration(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	endpointID := mcp.ParseString(request, "endpoint_id", "")
	config := mcp.ParseString(request, "config", "")
	nodeName := mcp.ParseString(request, "node_name", "")

	if endpointID == "" {
		return mcp.NewToolResultError("endpoint_id parameter is required"), nil
	}
	if config == "" {
		return mcp.NewToolResultError("config parameter is required"), nil
	}

	command := fmt.Sprintf("endpoint config %s %s", endpointID, config)
	output, err := runCiliumDbgCommand(command, nodeName)
	if err != nil {
		return mcp.NewToolResultError("Error managing endpoint configuration: " + err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func handleDisconnectEndpoint(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	endpointID := mcp.ParseString(request, "endpoint_id", "")
	nodeName := mcp.ParseString(request, "node_name", "")

	if endpointID == "" {
		return mcp.NewToolResultError("endpoint_id parameter is required"), nil
	}

	cmd := fmt.Sprintf("endpoint disconnect %s", endpointID)
	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to disconnect endpoint: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleGetEndpointsList(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName := mcp.ParseString(request, "node_name", "")

	output, err := runCiliumDbgCommand("endpoint list", nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get endpoints list: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleListIdentities(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName := mcp.ParseString(request, "node_name", "")

	output, err := runCiliumDbgCommand("identity list", nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list identities: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleGetIdentityDetails(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	identityID := mcp.ParseString(request, "identity_id", "")
	nodeName := mcp.ParseString(request, "node_name", "")

	if identityID == "" {
		return mcp.NewToolResultError("identity_id parameter is required"), nil
	}

	cmd := fmt.Sprintf("identity get %s", identityID)
	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get identity details: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleShowConfigurationOptions(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	listAll := mcp.ParseString(request, "list_all", "") == "true"
	listReadOnly := mcp.ParseString(request, "list_read_only", "") == "true"
	listOptions := mcp.ParseString(request, "list_options", "") == "true"
	nodeName := mcp.ParseString(request, "node_name", "")

	var cmd string
	if listAll {
		cmd = "endpoint config --all"
	} else if listReadOnly {
		cmd = "endpoint config -r"
	} else if listOptions {
		cmd = "endpoint config --list-options"
	} else {
		cmd = "endpoint config"
	}

	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to show configuration options: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleToggleConfigurationOption(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	option := mcp.ParseString(request, "option", "")
	value := mcp.ParseString(request, "value", "true") == "true"
	nodeName := mcp.ParseString(request, "node_name", "")

	if option == "" {
		return mcp.NewToolResultError("option parameter is required"), nil
	}

	valueStr := "enable"
	if !value {
		valueStr = "disable"
	}

	cmd := fmt.Sprintf("endpoint config %s=%s", option, valueStr)
	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to toggle configuration option: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleRequestDebuggingInformation(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName := mcp.ParseString(request, "node_name", "")

	output, err := runCiliumDbgCommand("debuginfo", nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to request debugging information: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleDisplayEncryptionState(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName := mcp.ParseString(request, "node_name", "")

	output, err := runCiliumDbgCommand("encrypt status", nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to display encryption state: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleFlushIPsecState(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName := mcp.ParseString(request, "node_name", "")

	output, err := runCiliumDbgCommand("encrypt flush -f", nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to flush IPsec state: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleListEnvoyConfig(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resourceName := mcp.ParseString(request, "resource_name", "")
	nodeName := mcp.ParseString(request, "node_name", "")

	if resourceName == "" {
		return mcp.NewToolResultError("resource_name parameter is required"), nil
	}

	cmd := fmt.Sprintf("envoy admin %s", resourceName)
	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list Envoy config: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleFQDNCache(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	command := mcp.ParseString(request, "command", "list")
	nodeName := mcp.ParseString(request, "node_name", "")

	var cmd string
	if command == "clean" {
		cmd = "fqdn cache clean -f"
	} else {
		cmd = fmt.Sprintf("fqdn cache %s", command)
	}

	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to manage FQDN cache: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleShowDNSNames(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName := mcp.ParseString(request, "node_name", "")

	output, err := runCiliumDbgCommand("dns names", nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to show DNS names: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleListIPAddresses(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName := mcp.ParseString(request, "node_name", "")

	output, err := runCiliumDbgCommand("ip list", nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list IP addresses: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleShowIPCacheInformation(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cidr := mcp.ParseString(request, "cidr", "")
	labels := mcp.ParseString(request, "labels", "")
	nodeName := mcp.ParseString(request, "node_name", "")

	var cmd string
	if labels != "" {
		cmd = fmt.Sprintf("ip get --labels %s", labels)
	} else if cidr != "" {
		cmd = fmt.Sprintf("ip get %s", cidr)
	} else {
		return mcp.NewToolResultError("either cidr or labels must be provided"), nil
	}

	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to show IP cache information: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleDeleteKeyFromKVStore(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	key := mcp.ParseString(request, "key", "")
	nodeName := mcp.ParseString(request, "node_name", "")

	if key == "" {
		return mcp.NewToolResultError("key parameter is required"), nil
	}

	cmd := fmt.Sprintf("kvstore delete %s", key)
	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to delete key from kvstore: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleGetKVStoreKey(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	key := mcp.ParseString(request, "key", "")
	nodeName := mcp.ParseString(request, "node_name", "")

	if key == "" {
		return mcp.NewToolResultError("key parameter is required"), nil
	}

	cmd := fmt.Sprintf("kvstore get %s", key)
	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get key from kvstore: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleSetKVStoreKey(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	key := mcp.ParseString(request, "key", "")
	value := mcp.ParseString(request, "value", "")
	nodeName := mcp.ParseString(request, "node_name", "")

	if key == "" || value == "" {
		return mcp.NewToolResultError("key and value parameters are required"), nil
	}

	cmd := fmt.Sprintf("kvstore set %s=%s", key, value)
	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to set key in kvstore: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleShowLoadInformation(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName := mcp.ParseString(request, "node_name", "")

	output, err := runCiliumDbgCommand("loadinfo", nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to show load information: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleListLocalRedirectPolicies(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName := mcp.ParseString(request, "node_name", "")

	output, err := runCiliumDbgCommand("lrp list", nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list local redirect policies: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleListBPFMapEvents(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	mapName := mcp.ParseString(request, "map_name", "")
	nodeName := mcp.ParseString(request, "node_name", "")

	if mapName == "" {
		return mcp.NewToolResultError("map_name parameter is required"), nil
	}

	cmd := fmt.Sprintf("bpf map events %s", mapName)
	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list BPF map events: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleGetBPFMap(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	mapName := mcp.ParseString(request, "map_name", "")
	nodeName := mcp.ParseString(request, "node_name", "")

	if mapName == "" {
		return mcp.NewToolResultError("map_name parameter is required"), nil
	}

	cmd := fmt.Sprintf("bpf map get %s", mapName)
	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get BPF map: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleListBPFMaps(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName := mcp.ParseString(request, "node_name", "")

	output, err := runCiliumDbgCommand("bpf map list", nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list BPF maps: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleListMetrics(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	matchPattern := mcp.ParseString(request, "match_pattern", "")
	nodeName := mcp.ParseString(request, "node_name", "")

	var cmd string
	if matchPattern != "" {
		cmd = fmt.Sprintf("metrics list --pattern %s", matchPattern)
	} else {
		cmd = "metrics list"
	}

	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list metrics: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleListClusterNodes(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName := mcp.ParseString(request, "node_name", "")

	output, err := runCiliumDbgCommand("nodes list", nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list cluster nodes: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleListNodeIds(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName := mcp.ParseString(request, "node_name", "")

	output, err := runCiliumDbgCommand("nodeid list", nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list node IDs: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleDisplayPolicyNodeInformation(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	labels := mcp.ParseString(request, "labels", "")
	nodeName := mcp.ParseString(request, "node_name", "")

	var cmd string
	if labels != "" {
		cmd = fmt.Sprintf("policy get %s", labels)
	} else {
		cmd = "policy get"
	}

	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to display policy node information: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleDeletePolicyRules(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	labels := mcp.ParseString(request, "labels", "")
	all := mcp.ParseString(request, "all", "") == "true"
	nodeName := mcp.ParseString(request, "node_name", "")

	var cmd string
	if all {
		cmd = "policy delete --all"
	} else if labels != "" {
		cmd = fmt.Sprintf("policy delete %s", labels)
	} else {
		return mcp.NewToolResultError("either labels or all=true must be provided"), nil
	}

	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to delete policy rules: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleDisplaySelectors(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName := mcp.ParseString(request, "node_name", "")

	output, err := runCiliumDbgCommand("policy selectors", nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to display selectors: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleListXDPCIDRFilters(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName := mcp.ParseString(request, "node_name", "")

	output, err := runCiliumDbgCommand("prefilter list", nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list XDP CIDR filters: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleUpdateXDPCIDRFilters(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cidrPrefixes := mcp.ParseString(request, "cidr_prefixes", "")
	revision := mcp.ParseString(request, "revision", "")
	nodeName := mcp.ParseString(request, "node_name", "")

	if cidrPrefixes == "" {
		return mcp.NewToolResultError("cidr_prefixes parameter is required"), nil
	}

	var cmd string
	if revision != "" {
		cmd = fmt.Sprintf("prefilter update --cidr %s --revision %s", cidrPrefixes, revision)
	} else {
		cmd = fmt.Sprintf("prefilter update --cidr %s", cidrPrefixes)
	}

	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to update XDP CIDR filters: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleDeleteXDPCIDRFilters(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cidrPrefixes := mcp.ParseString(request, "cidr_prefixes", "")
	revision := mcp.ParseString(request, "revision", "")
	nodeName := mcp.ParseString(request, "node_name", "")

	if cidrPrefixes == "" {
		return mcp.NewToolResultError("cidr_prefixes parameter is required"), nil
	}

	var cmd string
	if revision != "" {
		cmd = fmt.Sprintf("prefilter delete --cidr %s --revision %s", cidrPrefixes, revision)
	} else {
		cmd = fmt.Sprintf("prefilter delete --cidr %s", cidrPrefixes)
	}

	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to delete XDP CIDR filters: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleValidateCiliumNetworkPolicies(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	enableK8s := mcp.ParseString(request, "enable_k8s", "") == "true"
	enableK8sAPIDiscovery := mcp.ParseString(request, "enable_k8s_api_discovery", "") == "true"
	nodeName := mcp.ParseString(request, "node_name", "")

	cmd := "preflight validate-cnp"
	if enableK8s {
		cmd += " --enable-k8s"
	}
	if enableK8sAPIDiscovery {
		cmd += " --enable-k8s-api-discovery"
	}

	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to validate Cilium network policies: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleListPCAPRecorders(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName := mcp.ParseString(request, "node_name", "")

	output, err := runCiliumDbgCommand("recorder list", nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list PCAP recorders: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleGetPCAPRecorder(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	recorderID := mcp.ParseString(request, "recorder_id", "")
	nodeName := mcp.ParseString(request, "node_name", "")

	if recorderID == "" {
		return mcp.NewToolResultError("recorder_id parameter is required"), nil
	}

	cmd := fmt.Sprintf("recorder get %s", recorderID)
	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get PCAP recorder: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleDeletePCAPRecorder(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	recorderID := mcp.ParseString(request, "recorder_id", "")
	nodeName := mcp.ParseString(request, "node_name", "")

	if recorderID == "" {
		return mcp.NewToolResultError("recorder_id parameter is required"), nil
	}

	cmd := fmt.Sprintf("recorder delete %s", recorderID)
	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to delete PCAP recorder: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleUpdatePCAPRecorder(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	recorderID := mcp.ParseString(request, "recorder_id", "")
	filters := mcp.ParseString(request, "filters", "")
	caplen := mcp.ParseString(request, "caplen", "0")
	id := mcp.ParseString(request, "id", "0")
	nodeName := mcp.ParseString(request, "node_name", "")

	if recorderID == "" || filters == "" {
		return mcp.NewToolResultError("recorder_id and filters parameters are required"), nil
	}

	cmd := fmt.Sprintf("recorder update %s --filters %s --caplen %s --id %s", recorderID, filters, caplen, id)
	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to update PCAP recorder: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleListServices(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	showClusterMeshAffinity := mcp.ParseString(request, "show_cluster_mesh_affinity", "") == "true"
	nodeName := mcp.ParseString(request, "node_name", "")

	var cmd string
	if showClusterMeshAffinity {
		cmd = "service list --clustermesh-affinity"
	} else {
		cmd = "service list"
	}

	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list services: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleGetServiceInformation(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	serviceID := mcp.ParseString(request, "service_id", "")
	nodeName := mcp.ParseString(request, "node_name", "")

	if serviceID == "" {
		return mcp.NewToolResultError("service_id parameter is required"), nil
	}

	cmd := fmt.Sprintf("service get %s", serviceID)
	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get service information: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleDeleteService(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	serviceID := mcp.ParseString(request, "service_id", "")
	all := mcp.ParseString(request, "all", "") == "true"
	nodeName := mcp.ParseString(request, "node_name", "")

	var cmd string
	if all {
		cmd = "service delete --all"
	} else if serviceID != "" {
		cmd = fmt.Sprintf("service delete %s", serviceID)
	} else {
		return mcp.NewToolResultError("either service_id or all=true must be provided"), nil
	}

	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to delete service: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleUpdateService(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	backendWeights := mcp.ParseString(request, "backend_weights", "")
	backends := mcp.ParseString(request, "backends", "")
	frontend := mcp.ParseString(request, "frontend", "")
	id := mcp.ParseString(request, "id", "")
	k8sClusterInternal := mcp.ParseString(request, "k8s_cluster_internal", "") == "true"
	k8sExtTrafficPolicy := mcp.ParseString(request, "k8s_ext_traffic_policy", "Cluster")
	k8sExternal := mcp.ParseString(request, "k8s_external", "") == "true"
	k8sHostPort := mcp.ParseString(request, "k8s_host_port", "") == "true"
	k8sIntTrafficPolicy := mcp.ParseString(request, "k8s_int_traffic_policy", "Cluster")
	k8sLoadBalancer := mcp.ParseString(request, "k8s_load_balancer", "") == "true"
	k8sNodePort := mcp.ParseString(request, "k8s_node_port", "") == "true"
	localRedirect := mcp.ParseString(request, "local_redirect", "") == "true"
	protocol := mcp.ParseString(request, "protocol", "TCP")
	states := mcp.ParseString(request, "states", "active")
	nodeName := mcp.ParseString(request, "node_name", "")

	if backends == "" || frontend == "" || id == "" {
		return mcp.NewToolResultError("backends, frontend, and id parameters are required"), nil
	}

	cmd := fmt.Sprintf("service update %s --backends %s --frontend %s --protocol %s --states %s",
		id, backends, frontend, protocol, states)

	if backendWeights != "" {
		cmd += fmt.Sprintf(" --backend-weights %s", backendWeights)
	}
	if k8sClusterInternal {
		cmd += " --k8s-cluster-internal"
	}
	if k8sExtTrafficPolicy != "Cluster" {
		cmd += fmt.Sprintf(" --k8s-ext-traffic-policy %s", k8sExtTrafficPolicy)
	}
	if k8sExternal {
		cmd += " --k8s-external"
	}
	if k8sHostPort {
		cmd += " --k8s-host-port"
	}
	if k8sIntTrafficPolicy != "Cluster" {
		cmd += fmt.Sprintf(" --k8s-int-traffic-policy %s", k8sIntTrafficPolicy)
	}
	if k8sLoadBalancer {
		cmd += " --k8s-load-balancer"
	}
	if k8sNodePort {
		cmd += " --k8s-node-port"
	}
	if localRedirect {
		cmd += " --local-redirect"
	}

	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to update service: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func handleGetDaemonStatus(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	showAllAddresses := mcp.ParseString(request, "show_all_addresses", "") == "true"
	showAllClusters := mcp.ParseString(request, "show_all_clusters", "") == "true"
	showAllControllers := mcp.ParseString(request, "show_all_controllers", "") == "true"
	showHealth := mcp.ParseString(request, "show_health", "") == "true"
	showAllNodes := mcp.ParseString(request, "show_all_nodes", "") == "true"
	showAllRedirects := mcp.ParseString(request, "show_all_redirects", "") == "true"
	brief := mcp.ParseString(request, "brief", "") == "true"
	nodeName := mcp.ParseString(request, "node_name", "")

	cmd := "status"
	if showAllAddresses {
		cmd += " --all-addresses"
	}
	if showAllClusters {
		cmd += " --all-clusters"
	}
	if showAllControllers {
		cmd += " --all-controllers"
	}
	if showHealth {
		cmd += " --health"
	}
	if showAllNodes {
		cmd += " --all-nodes"
	}
	if showAllRedirects {
		cmd += " --all-redirects"
	}
	if brief {
		cmd += " --brief"
	}

	output, err := runCiliumDbgCommand(cmd, nodeName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get daemon status: %v", err)), nil
	}
	return mcp.NewToolResultText(output), nil
}

func RegisterCiliumDbgTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("cilium_get_endpoint_details",
		mcp.WithDescription("List the details of an endpoint in the cluster"),
		mcp.WithString("endpoint_id", mcp.Description("The ID of the endpoint to get details for")),
		mcp.WithString("labels", mcp.Description("The labels of the endpoint to get details for")),
		mcp.WithString("output_format", mcp.Description("The output format of the endpoint details (json, yaml, jsonpath)")),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the endpoint details for")),
	), handleGetEndpointDetails)

	s.AddTool(mcp.NewTool("cilium_get_endpoint_logs",
		mcp.WithDescription("Get the logs of an endpoint in the cluster"),
		mcp.WithString("endpoint_id", mcp.Description("The ID of the endpoint to get logs for"), mcp.Required()),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the endpoint logs for")),
	), handleGetEndpointLogs)

	s.AddTool(mcp.NewTool("cilium_get_endpoint_health",
		mcp.WithDescription("Get the health of an endpoint in the cluster"),
		mcp.WithString("endpoint_id", mcp.Description("The ID of the endpoint to get health for"), mcp.Required()),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the endpoint health for")),
	), handleGetEndpointHealth)

	s.AddTool(mcp.NewTool("cilium_manage_endpoint_labels",
		mcp.WithDescription("Manage the labels (add or delete) of an endpoint in the cluster"),
		mcp.WithString("endpoint_id", mcp.Description("The ID of the endpoint to manage labels for"), mcp.Required()),
		mcp.WithString("labels", mcp.Description("Space-separated labels to manage (e.g., 'key1=value1 key2=value2')"), mcp.Required()),
		mcp.WithString("action", mcp.Description("The action to perform on the labels (add or delete)"), mcp.Required()),
		mcp.WithString("node_name", mcp.Description("The name of the node to manage the endpoint labels on")),
	), handleManageEndpointLabels)

	s.AddTool(mcp.NewTool("cilium_manage_endpoint_config",
		mcp.WithDescription("Manage the configuration of an endpoint in the cluster"),
		mcp.WithString("endpoint_id", mcp.Description("The ID of the endpoint to manage configuration for"), mcp.Required()),
		mcp.WithString("config", mcp.Description("The configuration to manage for the endpoint provided as a space-separated list of key-value pairs (e.g. 'DropNotification=false TraceNotification=false')"), mcp.Required()),
		mcp.WithString("node_name", mcp.Description("The name of the node to manage the endpoint configuration on")),
	), handleManageEndpointConfiguration)

	s.AddTool(mcp.NewTool("cilium_disconnect_endpoint",
		mcp.WithDescription("Disconnect an endpoint from the network"),
		mcp.WithString("endpoint_id", mcp.Description("The ID of the endpoint to disconnect"), mcp.Required()),
		mcp.WithString("node_name", mcp.Description("The name of the node to disconnect the endpoint from")),
	), handleDisconnectEndpoint)

	s.AddTool(mcp.NewTool("cilium_list_identities",
		mcp.WithDescription("List all identities in the cluster"),
		mcp.WithString("node_name", mcp.Description("The name of the node to list the identities for")),
	), handleListIdentities)

	s.AddTool(mcp.NewTool("cilium_get_identity_details",
		mcp.WithDescription("Get the details of an identity in the cluster"),
		mcp.WithString("identity_id", mcp.Description("The ID of the identity to get details for"), mcp.Required()),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the identity details for")),
	), handleGetIdentityDetails)

	s.AddTool(mcp.NewTool("cilium_request_debugging_information",
		mcp.WithDescription("Request debugging information for the cluster"),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the debugging information for")),
	), handleRequestDebuggingInformation)

	s.AddTool(mcp.NewTool("cilium_display_encryption_state",
		mcp.WithDescription("Display the encryption state for the cluster"),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the encryption state for")),
	), handleDisplayEncryptionState)

	s.AddTool(mcp.NewTool("cilium_flush_ipsec_state",
		mcp.WithDescription("Flush the IPsec state for the cluster"),
		mcp.WithString("node_name", mcp.Description("The name of the node to flush the IPsec state for")),
	), handleFlushIPsecState)

	s.AddTool(mcp.NewTool("cilium_list_envoy_config",
		mcp.WithDescription("List the Envoy configuration for a resource in the cluster"),
		mcp.WithString("resource_name", mcp.Description("The name of the resource to get the Envoy configuration for"), mcp.Required()),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the Envoy configuration for")),
	), handleListEnvoyConfig)

	s.AddTool(mcp.NewTool("cilium_fqdn_cache",
		mcp.WithDescription("Manage the FQDN cache for the cluster"),
		mcp.WithString("command", mcp.Description("The command to perform on the FQDN cache (list, clean, or a specific command)"), mcp.Required()),
		mcp.WithString("node_name", mcp.Description("The name of the node to manage the FQDN cache for")),
	), handleFQDNCache)

	s.AddTool(mcp.NewTool("cilium_show_dns_names",
		mcp.WithDescription("Show the DNS names for the cluster"),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the DNS names for")),
	), handleShowDNSNames)

	s.AddTool(mcp.NewTool("cilium_list_ip_addresses",
		mcp.WithDescription("List the IP addresses for the cluster"),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the IP addresses for")),
	), handleListIPAddresses)

	s.AddTool(mcp.NewTool("cilium_show_ip_cache_information",
		mcp.WithDescription("Show the IP cache information for the cluster"),
		mcp.WithString("cidr", mcp.Description("The CIDR of the IP to get cache information for")),
		mcp.WithString("labels", mcp.Description("The labels of the IP to get cache information for")),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the IP cache information for")),
	), handleShowIPCacheInformation)

	s.AddTool(mcp.NewTool("cilium_delete_key_from_kv_store",
		mcp.WithDescription("Delete a key from the kvstore for the cluster"),
		mcp.WithString("key", mcp.Description("The key to delete from the kvstore"), mcp.Required()),
		mcp.WithString("node_name", mcp.Description("The name of the node to delete the key from")),
	), handleDeleteKeyFromKVStore)

	s.AddTool(mcp.NewTool("cilium_get_kv_store_key",
		mcp.WithDescription("Get a key from the kvstore for the cluster"),
		mcp.WithString("key", mcp.Description("The key to get from the kvstore"), mcp.Required()),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the key from")),
	), handleGetKVStoreKey)

	s.AddTool(mcp.NewTool("cilium_set_kv_store_key",
		mcp.WithDescription("Set a key in the kvstore for the cluster"),
		mcp.WithString("key", mcp.Description("The key to set in the kvstore"), mcp.Required()),
		mcp.WithString("value", mcp.Description("The value to set in the kvstore"), mcp.Required()),
		mcp.WithString("node_name", mcp.Description("The name of the node to set the key in")),
	), handleSetKVStoreKey)

	s.AddTool(mcp.NewTool("cilium_show_load_information",
		mcp.WithDescription("Show load information for the cluster"),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the load information for")),
	), handleShowLoadInformation)

	s.AddTool(mcp.NewTool("cilium_list_local_redirect_policies",
		mcp.WithDescription("List local redirect policies for the cluster"),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the local redirect policies for")),
	), handleListLocalRedirectPolicies)

	s.AddTool(mcp.NewTool("cilium_list_bpf_map_events",
		mcp.WithDescription("List BPF map events for the cluster"),
		mcp.WithString("map_name", mcp.Description("The name of the BPF map to get events for"), mcp.Required()),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the BPF map events for")),
	), handleListBPFMapEvents)

	s.AddTool(mcp.NewTool("cilium_get_bpf_map",
		mcp.WithDescription("Get BPF map for the cluster"),
		mcp.WithString("map_name", mcp.Description("The name of the BPF map to get"), mcp.Required()),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the BPF map for")),
	), handleGetBPFMap)

	s.AddTool(mcp.NewTool("cilium_list_bpf_maps",
		mcp.WithDescription("List BPF maps for the cluster"),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the BPF maps for")),
	), handleListBPFMaps)

	s.AddTool(mcp.NewTool("cilium_list_metrics",
		mcp.WithDescription("List metrics for the cluster"),
		mcp.WithString("match_pattern", mcp.Description("The match pattern to filter metrics by")),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the metrics for")),
	), handleListMetrics)

	s.AddTool(mcp.NewTool("cilium_list_cluster_nodes",
		mcp.WithDescription("List cluster nodes for the cluster"),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the cluster nodes for")),
	), handleListClusterNodes)

	s.AddTool(mcp.NewTool("cilium_list_node_ids",
		mcp.WithDescription("List node IDs for the cluster"),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the node IDs for")),
	), handleListNodeIds)

	s.AddTool(mcp.NewTool("cilium_display_policy_node_information",
		mcp.WithDescription("Display policy node information for the cluster"),
		mcp.WithString("labels", mcp.Description("The labels to get policy node information for")),
		mcp.WithString("node_name", mcp.Description("The name of the node to get policy node information for")),
	), handleDisplayPolicyNodeInformation)

	s.AddTool(mcp.NewTool("cilium_delete_policy_rules",
		mcp.WithDescription("Delete policy rules for the cluster"),
		mcp.WithString("labels", mcp.Description("The labels to delete policy rules for")),
		mcp.WithString("all", mcp.Description("Whether to delete all policy rules")),
		mcp.WithString("node_name", mcp.Description("The name of the node to delete policy rules for")),
	), handleDeletePolicyRules)

	s.AddTool(mcp.NewTool("cilium_display_selectors",
		mcp.WithDescription("Display selectors for the cluster"),
		mcp.WithString("node_name", mcp.Description("The name of the node to get selectors for")),
	), handleDisplaySelectors)

	s.AddTool(mcp.NewTool("cilium_list_xdp_cidr_filters",
		mcp.WithDescription("List XDP CIDR filters for the cluster"),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the XDP CIDR filters for")),
	), handleListXDPCIDRFilters)

	s.AddTool(mcp.NewTool("cilium_update_xdp_cidr_filters",
		mcp.WithDescription("Update XDP CIDR filters for the cluster"),
		mcp.WithString("cidr_prefixes", mcp.Description("The CIDR prefixes to update the XDP filters for"), mcp.Required()),
		mcp.WithString("revision", mcp.Description("The revision of the XDP filters to update")),
		mcp.WithString("node_name", mcp.Description("The name of the node to update the XDP filters for")),
	), handleUpdateXDPCIDRFilters)

	s.AddTool(mcp.NewTool("cilium_delete_xdp_cidr_filters",
		mcp.WithDescription("Delete XDP CIDR filters for the cluster"),
		mcp.WithString("cidr_prefixes", mcp.Description("The CIDR prefixes to delete the XDP filters for"), mcp.Required()),
		mcp.WithString("revision", mcp.Description("The revision of the XDP filters to delete")),
		mcp.WithString("node_name", mcp.Description("The name of the node to delete the XDP filters for")),
	), handleDeleteXDPCIDRFilters)

	s.AddTool(mcp.NewTool("cilium_validate_cilium_network_policies",
		mcp.WithDescription("Validate Cilium network policies for the cluster"),
		mcp.WithString("enable_k8s", mcp.Description("Whether to enable k8s API discovery")),
		mcp.WithString("enable_k8s_api_discovery", mcp.Description("Whether to enable k8s API discovery")),
		mcp.WithString("node_name", mcp.Description("The name of the node to validate the Cilium network policies for")),
	), handleValidateCiliumNetworkPolicies)

	s.AddTool(mcp.NewTool("cilium_list_pcap_recorders",
		mcp.WithDescription("List PCAP recorders for the cluster"),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the PCAP recorders for")),
	), handleListPCAPRecorders)

	s.AddTool(mcp.NewTool("cilium_get_pcap_recorder",
		mcp.WithDescription("Get a PCAP recorder for the cluster"),
		mcp.WithString("recorder_id", mcp.Description("The ID of the PCAP recorder to get"), mcp.Required()),
		mcp.WithString("node_name", mcp.Description("The name of the node to get the PCAP recorder for")),
	), handleGetPCAPRecorder)

	s.AddTool(mcp.NewTool("cilium_delete_pcap_recorder",
		mcp.WithDescription("Delete a PCAP recorder for the cluster"),
		mcp.WithString("recorder_id", mcp.Description("The ID of the PCAP recorder to delete"), mcp.Required()),
		mcp.WithString("node_name", mcp.Description("The name of the node to delete the PCAP recorder from")),
	), handleDeletePCAPRecorder)

	s.AddTool(mcp.NewTool("cilium_update_pcap_recorder",
		mcp.WithDescription("Update a PCAP recorder for the cluster"),
		mcp.WithString("recorder_id", mcp.Description("The ID of the PCAP recorder to update"), mcp.Required()),
		mcp.WithString("filters", mcp.Description("The filters to update the PCAP recorder with"), mcp.Required()),
		mcp.WithString("caplen", mcp.Description("The caplen to update the PCAP recorder with")),
		mcp.WithString("id", mcp.Description("The id to update the PCAP recorder with")),
		mcp.WithString("node_name", mcp.Description("The name of the node to update the PCAP recorder on")),
	), handleUpdatePCAPRecorder)
}
