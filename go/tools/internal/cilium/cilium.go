package cilium

import (
	"context"

	"github.com/kagent-dev/kagent/go/tools/internal/common"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Cilium tools using cilium CLI

func runCiliumCli(args ...string) (string, error) {
	return common.RunCommand("cilium", args)
}

func handleCiliumStatusAndVersion(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	status, err := runCiliumCli("status")
	if err != nil {
		return mcp.NewToolResultError("Error getting Cilium status: " + err.Error()), nil
	}

	version, err := runCiliumCli("version")
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

	output, err := runCiliumCli(args...)
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

	output, err := runCiliumCli(args...)
	if err != nil {
		return mcp.NewToolResultError("Error installing Cilium: " + err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func handleUninstallCilium(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	output, err := runCiliumCli("uninstall")
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

	output, err := runCiliumCli(args...)
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

	output, err := runCiliumCli(args...)
	if err != nil {
		return mcp.NewToolResultError("Error disconnecting from remote cluster: " + err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func handleListBGPPeers(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	output, err := runCiliumCli("bgp", "peers")
	if err != nil {
		return mcp.NewToolResultError("Error listing BGP peers: " + err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func handleListBGPRoutes(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	output, err := runCiliumCli("bgp", "routes")
	if err != nil {
		return mcp.NewToolResultError("Error listing BGP routes: " + err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func handleShowClusterMeshStatus(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	output, err := runCiliumCli("clustermesh", "status")
	if err != nil {
		return mcp.NewToolResultError("Error getting cluster mesh status: " + err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func handleShowFeaturesStatus(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	output, err := runCiliumCli("features", "status")
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

	output, err := runCiliumCli("hubble", action)
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

	output, err := runCiliumCli("clustermesh", action)
	if err != nil {
		return mcp.NewToolResultError("Error toggling cluster mesh: " + err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func RegisterCiliumTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("cilium_status_and_version",
		mcp.WithDescription("Get the status and version of Cilium installation"),
	), handleCiliumStatusAndVersion)

	s.AddTool(mcp.NewTool("upgrade_cilium",
		mcp.WithDescription("Upgrade Cilium on the cluster"),
		mcp.WithString("cluster_name", mcp.Description("The name of the cluster to upgrade Cilium on")),
		mcp.WithString("datapath_mode", mcp.Description("The datapath mode to use for Cilium (tunnel, native, aws-eni, gke, azure, aks-byocni)")),
	), handleUpgradeCilium)

	s.AddTool(mcp.NewTool("install_cilium",
		mcp.WithDescription("Install Cilium on the cluster"),
		mcp.WithString("cluster_name", mcp.Description("The name of the cluster to install Cilium on")),
		mcp.WithString("cluster_id", mcp.Description("The ID of the cluster to install Cilium on")),
		mcp.WithString("datapath_mode", mcp.Description("The datapath mode to use for Cilium (tunnel, native, aws-eni, gke, azure, aks-byocni)")),
	), handleInstallCilium)

	s.AddTool(mcp.NewTool("uninstall_cilium",
		mcp.WithDescription("Uninstall Cilium from the cluster"),
	), handleUninstallCilium)

	s.AddTool(mcp.NewTool("connect_to_remote_cluster",
		mcp.WithDescription("Connect to a remote cluster for cluster mesh"),
		mcp.WithString("cluster_name", mcp.Description("The name of the destination cluster"), mcp.Required()),
		mcp.WithString("context", mcp.Description("The kubectl context for the destination cluster")),
	), handleConnectToRemoteCluster)

	s.AddTool(mcp.NewTool("disconnect_remote_cluster",
		mcp.WithDescription("Disconnect from a remote cluster"),
		mcp.WithString("cluster_name", mcp.Description("The name of the destination cluster"), mcp.Required()),
	), handleDisconnectRemoteCluster)

	s.AddTool(mcp.NewTool("list_bgp_peers",
		mcp.WithDescription("List BGP peers"),
	), handleListBGPPeers)

	s.AddTool(mcp.NewTool("list_bgp_routes",
		mcp.WithDescription("List BGP routes"),
	), handleListBGPRoutes)

	s.AddTool(mcp.NewTool("show_cluster_mesh_status",
		mcp.WithDescription("Show cluster mesh status"),
	), handleShowClusterMeshStatus)

	s.AddTool(mcp.NewTool("show_features_status",
		mcp.WithDescription("Show Cilium features status"),
	), handleShowFeaturesStatus)

	s.AddTool(mcp.NewTool("toggle_hubble",
		mcp.WithDescription("Toggle Hubble"),
		mcp.WithString("enable", mcp.Description("Enable or disable Hubble (true/false)")),
	), handleToggleHubble)

	s.AddTool(mcp.NewTool("toggle_cluster_mesh",
		mcp.WithDescription("Toggle cluster mesh"),
		mcp.WithString("enable", mcp.Description("Enable or disable cluster mesh (true/false)")),
	), handleToggleClusterMesh)
}
