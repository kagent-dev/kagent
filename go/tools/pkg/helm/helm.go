package helm

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/tools/pkg/utils"
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

	result, err := utils.RunCommandWithContext(ctx, "helm", args)
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

	result, err := utils.RunCommandWithContext(ctx, "helm", args)
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

	result, err := utils.RunCommandWithContext(ctx, "helm", args)
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

	result, err := utils.RunCommandWithContext(ctx, "helm", args)
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

	result, err := utils.RunCommandWithContext(ctx, "helm", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Helm repo add command failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Helm repo update
func handleHelmRepoUpdate(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := []string{"repo", "update"}

	result, err := utils.RunCommandWithContext(ctx, "helm", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Helm repo update command failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Register Helm tools
func RegisterHelmTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("helm_list_releases",
		mcp.WithDescription("List Helm releases in a namespace"),
		mcp.WithString("namespace", mcp.Description("The namespace to list releases from")),
		mcp.WithString("all_namespaces", mcp.Description("List releases from all namespaces")),
		mcp.WithString("all", mcp.Description("Show all releases without any filter applied")),
		mcp.WithString("uninstalled", mcp.Description("List uninstalled releases")),
		mcp.WithString("uninstalling", mcp.Description("List uninstalling releases")),
		mcp.WithString("failed", mcp.Description("List failed releases")),
		mcp.WithString("deployed", mcp.Description("List deployed releases")),
		mcp.WithString("pending", mcp.Description("List pending releases")),
		mcp.WithString("filter", mcp.Description("A regular expression to filter releases by")),
		mcp.WithString("output", mcp.Description("The output format (e.g., 'json', 'yaml', 'table')")),
	), handleHelmListReleases)

	s.AddTool(mcp.NewTool("helm_get_release",
		mcp.WithDescription("Get extended information about a Helm release"),
		mcp.WithString("name", mcp.Description("The name of the release"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("The namespace of the release"), mcp.Required()),
		mcp.WithString("resource", mcp.Description("The resource to get (all, hooks, manifest, notes, values)")),
	), handleHelmGetRelease)

	s.AddTool(mcp.NewTool("helm_upgrade",
		mcp.WithDescription("Upgrade or install a Helm release"),
		mcp.WithString("name", mcp.Description("The name of the release"), mcp.Required()),
		mcp.WithString("chart", mcp.Description("The chart to install or upgrade to"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("The namespace of the release")),
		mcp.WithString("version", mcp.Description("The version of the chart to upgrade to")),
		mcp.WithString("values", mcp.Description("Path to a values file")),
		mcp.WithString("set", mcp.Description("Set values on the command line (e.g., 'key1=val1,key2=val2')")),
		mcp.WithString("install", mcp.Description("Run an install if the release is not present")),
		mcp.WithString("dry_run", mcp.Description("Simulate an upgrade")),
		mcp.WithString("wait", mcp.Description("Wait for the upgrade to complete")),
	), handleHelmUpgradeRelease)

	s.AddTool(mcp.NewTool("helm_uninstall",
		mcp.WithDescription("Uninstall a Helm release"),
		mcp.WithString("name", mcp.Description("The name of the release to uninstall"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("The namespace of the release"), mcp.Required()),
		mcp.WithString("dry_run", mcp.Description("Simulate an uninstall")),
		mcp.WithString("wait", mcp.Description("Wait for the uninstall to complete")),
	), handleHelmUninstall)

	s.AddTool(mcp.NewTool("helm_repo_add",
		mcp.WithDescription("Add a Helm repository"),
		mcp.WithString("name", mcp.Description("The name of the repository"), mcp.Required()),
		mcp.WithString("url", mcp.Description("The URL of the repository"), mcp.Required()),
	), handleHelmRepoAdd)

	s.AddTool(mcp.NewTool("helm_repo_update",
		mcp.WithDescription("Update information of available charts locally from chart repositories"),
	), handleHelmRepoUpdate)
}
