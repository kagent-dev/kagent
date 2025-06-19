package helm

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/tools/internal/common"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Helm list releases
func handleHelmListReleases(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	namespace := mcp.ParseString(request, "namespace", "")
	allNamespaces := mcp.ParseString(request, "all_namespaces", "") == "true"
	all := mcp.ParseString(request, "all", "") == "true"
	uninstalled := mcp.ParseString(request, "uninstalled", "") == "true"
	uninstalling := mcp.ParseString(request, "uninstalling", "") == "true"
	failed := mcp.ParseString(request, "failed", "") == "true"
	deployed := mcp.ParseString(request, "deployed", "") == "true"
	pending := mcp.ParseString(request, "pending", "") == "true"
	filter := mcp.ParseString(request, "filter", "")
	output := mcp.ParseString(request, "output", "")

	args := []string{"list"}

	if namespace != "" {
		args = append(args, "-n", namespace)
	}

	if allNamespaces {
		args = append(args, "-A")
	}

	if all {
		args = append(args, "-a")
	}

	if uninstalled {
		args = append(args, "--uninstalled")
	}

	if uninstalling {
		args = append(args, "--uninstalling")
	}

	if failed {
		args = append(args, "--failed")
	}

	if deployed {
		args = append(args, "--deployed")
	}

	if pending {
		args = append(args, "--pending")
	}

	if filter != "" {
		args = append(args, "-f", filter)
	}

	if output != "" {
		args = append(args, "-o", output)
	}

	result, err := common.RunCommand("helm", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Helm list command failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Helm get release
func handleHelmGetRelease(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := mcp.ParseString(request, "name", "")
	namespace := mcp.ParseString(request, "namespace", "")
	resource := mcp.ParseString(request, "resource", "all")

	if name == "" {
		return mcp.NewToolResultError("name parameter is required"), nil
	}

	if namespace == "" {
		return mcp.NewToolResultError("namespace parameter is required"), nil
	}

	args := []string{"get", resource, name, "-n", namespace}

	result, err := common.RunCommand("helm", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Helm get command failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Helm upgrade release
func handleHelmUpgradeRelease(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := mcp.ParseString(request, "name", "")
	chart := mcp.ParseString(request, "chart", "")
	namespace := mcp.ParseString(request, "namespace", "")
	version := mcp.ParseString(request, "version", "")
	values := mcp.ParseString(request, "values", "")
	setValues := mcp.ParseString(request, "set", "")
	install := mcp.ParseString(request, "install", "") == "true"
	dryRun := mcp.ParseString(request, "dry_run", "") == "true"
	wait := mcp.ParseString(request, "wait", "") == "true"

	if name == "" || chart == "" {
		return mcp.NewToolResultError("name and chart parameters are required"), nil
	}

	args := []string{"upgrade", name, chart}

	if namespace != "" {
		args = append(args, "-n", namespace)
	}

	if version != "" {
		args = append(args, "--version", version)
	}

	if values != "" {
		args = append(args, "-f", values)
	}

	if setValues != "" {
		// Split multiple set values by comma
		setValuesList := strings.Split(setValues, ",")
		for _, setValue := range setValuesList {
			args = append(args, "--set", strings.TrimSpace(setValue))
		}
	}

	if install {
		args = append(args, "--install")
	}

	if dryRun {
		args = append(args, "--dry-run")
	}

	if wait {
		args = append(args, "--wait")
	}

	result, err := common.RunCommand("helm", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Helm upgrade command failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Helm uninstall release
func handleHelmUninstall(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := mcp.ParseString(request, "name", "")
	namespace := mcp.ParseString(request, "namespace", "")
	dryRun := mcp.ParseString(request, "dry_run", "") == "true"
	wait := mcp.ParseString(request, "wait", "") == "true"

	if name == "" || namespace == "" {
		return mcp.NewToolResultError("name and namespace parameters are required"), nil
	}

	args := []string{"uninstall", name, "-n", namespace}

	if dryRun {
		args = append(args, "--dry-run")
	}

	if wait {
		args = append(args, "--wait")
	}

	result, err := common.RunCommand("helm", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Helm uninstall command failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Helm repo add
func handleHelmRepoAdd(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := mcp.ParseString(request, "name", "")
	url := mcp.ParseString(request, "url", "")

	if name == "" || url == "" {
		return mcp.NewToolResultError("name and url parameters are required"), nil
	}

	args := []string{"repo", "add", name, url}

	result, err := common.RunCommand("helm", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Helm repo add command failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Helm repo update
func handleHelmRepoUpdate(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := []string{"repo", "update"}

	result, err := common.RunCommand("helm", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Helm repo update command failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Helm install
func handleHelmInstall(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := mcp.ParseString(request, "name", "")
	chart := mcp.ParseString(request, "chart", "")
	namespace := mcp.ParseString(request, "namespace", "")
	version := mcp.ParseString(request, "version", "")
	values := mcp.ParseString(request, "values", "")
	setValues := mcp.ParseString(request, "set", "")
	dryRun := mcp.ParseString(request, "dry_run", "") == "true"
	wait := mcp.ParseString(request, "wait", "") == "true"
	createNamespace := mcp.ParseString(request, "create_namespace", "") == "true"

	if name == "" || chart == "" {
		return mcp.NewToolResultError("name and chart parameters are required"), nil
	}

	args := []string{"install", name, chart}

	if namespace != "" {
		args = append(args, "-n", namespace)
	}

	if version != "" {
		args = append(args, "--version", version)
	}

	if values != "" {
		args = append(args, "-f", values)
	}

	if setValues != "" {
		// Split multiple set values by comma
		setValuesList := strings.Split(setValues, ",")
		for _, setValue := range setValuesList {
			args = append(args, "--set", strings.TrimSpace(setValue))
		}
	}

	if createNamespace {
		args = append(args, "--create-namespace")
	}

	if dryRun {
		args = append(args, "--dry-run")
	}

	if wait {
		args = append(args, "--wait")
	}

	result, err := common.RunCommand("helm", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Helm install command failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Register Helm tools
func RegisterHelmTools(s *server.MCPServer) {
	// Helm list releases
	s.AddTool(mcp.NewTool("helm_list_releases",
		mcp.WithDescription("List all Helm releases in a namespace"),
		mcp.WithString("namespace", mcp.Description("The namespace to list releases from")),
		mcp.WithString("all_namespaces", mcp.Description("List releases from all namespaces (true/false)")),
		mcp.WithString("all", mcp.Description("Show all releases without any filter applied (true/false)")),
		mcp.WithString("uninstalled", mcp.Description("Show uninstalled releases (true/false)")),
		mcp.WithString("uninstalling", mcp.Description("Show uninstalling releases (true/false)")),
		mcp.WithString("failed", mcp.Description("Show failed releases (true/false)")),
		mcp.WithString("deployed", mcp.Description("Show deployed releases (true/false)")),
		mcp.WithString("pending", mcp.Description("Show pending releases (true/false)")),
		mcp.WithString("filter", mcp.Description("Regular expression filter for release names")),
		mcp.WithString("output", mcp.Description("Output format (table, json, yaml)")),
	), handleHelmListReleases)

	// Helm get release
	s.AddTool(mcp.NewTool("helm_get_release",
		mcp.WithDescription("Get extended information about a Helm release"),
		mcp.WithString("name", mcp.Description("The name of the release"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("The namespace of the release"), mcp.Required()),
		mcp.WithString("resource", mcp.Description("The resource to get (all, hooks, manifest, notes, values)")),
	), handleHelmGetRelease)

	// Helm upgrade release
	s.AddTool(mcp.NewTool("helm_upgrade_release",
		mcp.WithDescription("Upgrade or install a Helm release"),
		mcp.WithString("name", mcp.Description("The name of the release"), mcp.Required()),
		mcp.WithString("chart", mcp.Description("The chart to upgrade to"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("The namespace of the release")),
		mcp.WithString("version", mcp.Description("The chart version to upgrade to")),
		mcp.WithString("values", mcp.Description("Path to values file")),
		mcp.WithString("set", mcp.Description("Set values on command line (comma-separated key=value pairs)")),
		mcp.WithString("install", mcp.Description("Install if release does not exist (true/false)")),
		mcp.WithString("dry_run", mcp.Description("Simulate an upgrade (true/false)")),
		mcp.WithString("wait", mcp.Description("Wait for completion (true/false)")),
	), handleHelmUpgradeRelease)

	// Helm uninstall
	s.AddTool(mcp.NewTool("helm_uninstall",
		mcp.WithDescription("Uninstall a Helm release"),
		mcp.WithString("name", mcp.Description("The name of the release"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("The namespace of the release"), mcp.Required()),
		mcp.WithString("dry_run", mcp.Description("Simulate uninstall (true/false)")),
		mcp.WithString("wait", mcp.Description("Wait for completion (true/false)")),
	), handleHelmUninstall)

	// Helm install
	s.AddTool(mcp.NewTool("helm_install",
		mcp.WithDescription("Install a Helm chart"),
		mcp.WithString("name", mcp.Description("The name of the release"), mcp.Required()),
		mcp.WithString("chart", mcp.Description("The chart to install"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("The namespace to install into")),
		mcp.WithString("version", mcp.Description("The chart version to install")),
		mcp.WithString("values", mcp.Description("Path to values file")),
		mcp.WithString("set", mcp.Description("Set values on command line (comma-separated key=value pairs)")),
		mcp.WithString("create_namespace", mcp.Description("Create namespace if it doesn't exist (true/false)")),
		mcp.WithString("dry_run", mcp.Description("Simulate installation (true/false)")),
		mcp.WithString("wait", mcp.Description("Wait for completion (true/false)")),
	), handleHelmInstall)

	// Helm repo add
	s.AddTool(mcp.NewTool("helm_repo_add",
		mcp.WithDescription("Add a Helm chart repository"),
		mcp.WithString("name", mcp.Description("The name of the repository"), mcp.Required()),
		mcp.WithString("url", mcp.Description("The URL of the repository"), mcp.Required()),
	), handleHelmRepoAdd)

	// Helm repo update
	s.AddTool(mcp.NewTool("helm_repo_update",
		mcp.WithDescription("Update local Helm chart repositories"),
	), handleHelmRepoUpdate)
}
