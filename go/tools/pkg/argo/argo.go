package argo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/tools/pkg/utils"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Argo Rollouts tools

func handleVerifyArgoRolloutsControllerInstall(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ns := mcp.ParseString(request, "namespace", "argo-rollouts")
	label := mcp.ParseString(request, "label", "app.kubernetes.io/component=rollouts-controller")

	cmd := []string{"get", "pods", "-n", ns, "-l", label, "-o", "jsonpath={.items[*].status.phase}"}
	output, err := utils.RunCommandWithContext(ctx, "kubectl", cmd)
	if err != nil {
		return mcp.NewToolResultError("Error: " + err.Error()), nil
	}

	output = strings.TrimSpace(output)
	if output == "" {
		return mcp.NewToolResultText("Error: No pods found"), nil
	}

	if strings.HasPrefix(output, "Error") {
		return mcp.NewToolResultText(output), nil
	}

	podStatuses := strings.Fields(output)
	if len(podStatuses) == 0 {
		return mcp.NewToolResultText("Error: No pod statuses returned"), nil
	}

	allRunning := true
	for _, status := range podStatuses {
		if status != "Running" {
			allRunning = false
			break
		}
	}

	if allRunning {
		return mcp.NewToolResultText("All pods are running"), nil
	} else {
		return mcp.NewToolResultText("Error: Not all pods are running (" + strings.Join(podStatuses, " ") + ")"), nil
	}
}

func handleVerifyKubectlPluginInstall(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	output, err := utils.RunCommandWithContext(ctx, "kubectl", []string{"argo", "rollouts", "version"})
	if err != nil {
		return mcp.NewToolResultText("Kubectl Argo Rollouts plugin is not installed: " + err.Error()), nil
	}

	if strings.HasPrefix(output, "Error") {
		return mcp.NewToolResultText("Kubectl Argo Rollouts plugin is not installed: " + output), nil
	}

	return mcp.NewToolResultText(output), nil
}

func handlePromoteRollout(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rolloutName := mcp.ParseString(request, "rollout_name", "")
	ns := mcp.ParseString(request, "namespace", "")
	fullStr := mcp.ParseString(request, "full", "false")
	full := fullStr == "true"

	if rolloutName == "" {
		return mcp.NewToolResultError("rollout_name parameter is required"), nil
	}

	cmd := []string{"argo", "rollouts", "promote"}
	if ns != "" {
		cmd = append(cmd, "-n", ns)
	}
	cmd = append(cmd, rolloutName)
	if full {
		cmd = append(cmd, "--full")
	}

	output, err := utils.RunCommandWithContext(ctx, "kubectl", cmd)
	if err != nil {
		return mcp.NewToolResultError("Error promoting rollout: " + err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func handlePauseRollout(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rolloutName := mcp.ParseString(request, "rollout_name", "")
	ns := mcp.ParseString(request, "namespace", "")

	if rolloutName == "" {
		return mcp.NewToolResultError("rollout_name parameter is required"), nil
	}

	cmd := []string{"argo", "rollouts", "pause"}
	if ns != "" {
		cmd = append(cmd, "-n", ns)
	}
	cmd = append(cmd, rolloutName)

	output, err := utils.RunCommandWithContext(ctx, "kubectl", cmd)
	if err != nil {
		return mcp.NewToolResultError("Error pausing rollout: " + err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func handleSetRolloutImage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rolloutName := mcp.ParseString(request, "rollout_name", "")
	containerImage := mcp.ParseString(request, "container_image", "")
	ns := mcp.ParseString(request, "namespace", "")

	if rolloutName == "" {
		return mcp.NewToolResultError("rollout_name parameter is required"), nil
	}
	if containerImage == "" {
		return mcp.NewToolResultError("container_image parameter is required"), nil
	}

	cmd := []string{"argo", "rollouts", "set", "image", rolloutName, containerImage}
	if ns != "" {
		cmd = append(cmd, "-n", ns)
	}

	output, err := utils.RunCommandWithContext(ctx, "kubectl", cmd)
	if err != nil {
		return mcp.NewToolResultError("Error setting rollout image: " + err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

// Gateway Plugin Status struct
type GatewayPluginStatus struct {
	Installed    bool    `json:"installed"`
	Version      string  `json:"version,omitempty"`
	Architecture string  `json:"architecture,omitempty"`
	DownloadTime float64 `json:"download_time,omitempty"`
	ErrorMessage string  `json:"error_message,omitempty"`
}

func (gps GatewayPluginStatus) String() string {
	data, _ := json.MarshalIndent(gps, "", "  ")
	return string(data)
}

func getSystemArchitecture() (string, error) {
	system := strings.ToLower(runtime.GOOS)
	machine := strings.ToLower(runtime.GOARCH)

	// Map Go architecture to plugin architecture
	archMap := map[string]string{
		"amd64": "amd64",
		"arm64": "arm64",
		"arm":   "arm",
	}

	arch, ok := archMap[machine]
	if !ok {
		arch = machine
	}

	switch system {
	case "windows":
		return fmt.Sprintf("windows-%s.exe", arch), nil
	case "darwin":
		return fmt.Sprintf("darwin-%s", arch), nil
	case "linux":
		return fmt.Sprintf("linux-%s", arch), nil
	default:
		return "", fmt.Errorf("unsupported system: %s", system)
	}
}

func getLatestVersion() string {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/argoproj-labs/rollouts-plugin-trafficrouter-gatewayapi/releases/latest")
	if err != nil {
		return "0.5.0" // Default version
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "0.5.0"
	}

	versionRegex := regexp.MustCompile(`"tag_name":\s*"v([^"]+)"`)
	matches := versionRegex.FindStringSubmatch(string(body))
	if len(matches) > 1 {
		return matches[1]
	}

	return "0.5.0"
}

func configureGatewayPlugin(version, namespace string) GatewayPluginStatus {
	arch, err := getSystemArchitecture()
	if err != nil {
		return GatewayPluginStatus{
			Installed:    false,
			ErrorMessage: fmt.Sprintf("Error determining system architecture: %s", err.Error()),
		}
	}

	if version == "" {
		version = getLatestVersion()
	}

	configMap := fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-config
  namespace: %s
data:
  trafficRouterPlugins: |-
    - name: "argoproj-labs/gatewayAPI"
      location: "https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-gatewayapi/releases/download/v%s/gatewayapi-plugin-%s"
`, namespace, version, arch)

	// Create temporary file
	tmpFile, err := os.CreateTemp("", "argo-gateway-config-*.yaml")
	if err != nil {
		return GatewayPluginStatus{
			Installed:    false,
			ErrorMessage: fmt.Sprintf("Failed to create temp file: %s", err.Error()),
		}
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configMap); err != nil {
		return GatewayPluginStatus{
			Installed:    false,
			ErrorMessage: fmt.Sprintf("Failed to write config map: %s", err.Error()),
		}
	}
	tmpFile.Close()

	// Apply the ConfigMap
	_, err = utils.RunCommandWithContext(context.Background(), "kubectl", []string{"apply", "-f", tmpFile.Name()})
	if err != nil {
		return GatewayPluginStatus{
			Installed:    false,
			ErrorMessage: fmt.Sprintf("Failed to configure Gateway API plugin: %s", err.Error()),
		}
	}

	return GatewayPluginStatus{
		Installed:    true,
		Version:      version,
		Architecture: arch,
	}
}

func handleVerifyGatewayPlugin(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	version := mcp.ParseString(request, "version", "")
	namespace := mcp.ParseString(request, "namespace", "argo-rollouts")
	shouldInstallStr := mcp.ParseString(request, "should_install", "true")
	shouldInstall := shouldInstallStr == "true"

	// Check if ConfigMap exists and is configured
	cmd := []string{"get", "configmap", "argo-rollouts-config", "-n", namespace, "-o", "yaml"}
	output, err := utils.RunCommandWithContext(ctx, "kubectl", cmd)
	if err == nil && strings.Contains(output, "argoproj-labs/gatewayAPI") {
		status := GatewayPluginStatus{
			Installed:    true,
			ErrorMessage: "Gateway API plugin is already configured",
		}
		return mcp.NewToolResultText(status.String()), nil
	}

	if !shouldInstall {
		status := GatewayPluginStatus{
			Installed:    false,
			ErrorMessage: "Gateway API plugin is not configured and installation is disabled",
		}
		return mcp.NewToolResultText(status.String()), nil
	}

	// Configure plugin
	status := configureGatewayPlugin(version, namespace)
	return mcp.NewToolResultText(status.String()), nil
}

func handleCheckPluginLogs(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	namespace := mcp.ParseString(request, "namespace", "argo-rollouts")
	timeoutStr := mcp.ParseString(request, "timeout", "60")

	// Parse timeout (for potential future use)
	_, err := strconv.Atoi(timeoutStr)
	if err != nil {
		// Use default timeout of 60 if parsing fails
	}

	cmd := []string{"logs", "-n", namespace, "-l", "app.kubernetes.io/name=argo-rollouts", "--tail", "100"}
	output, err := utils.RunCommandWithContext(ctx, "kubectl", cmd)
	if err != nil {
		status := GatewayPluginStatus{
			Installed:    false,
			ErrorMessage: err.Error(),
		}
		return mcp.NewToolResultText(status.String()), nil
	}

	// Parse download information
	downloadPattern := regexp.MustCompile(`Downloading plugin argoproj-labs/gatewayAPI from: .*/v([\d.]+)/gatewayapi-plugin-([\w-]+)"`)
	timePattern := regexp.MustCompile(`Download complete, it took ([\d.]+)s`)

	versionMatches := downloadPattern.FindStringSubmatch(output)
	timeMatches := timePattern.FindStringSubmatch(output)

	if len(versionMatches) > 2 && len(timeMatches) > 1 {
		downloadTime, _ := strconv.ParseFloat(timeMatches[1], 64)
		status := GatewayPluginStatus{
			Installed:    true,
			Version:      versionMatches[1],
			Architecture: versionMatches[2],
			DownloadTime: downloadTime,
		}
		return mcp.NewToolResultText(status.String()), nil
	}

	status := GatewayPluginStatus{
		Installed:    false,
		ErrorMessage: "Plugin installation not found in logs",
	}
	return mcp.NewToolResultText(status.String()), nil
}

func RegisterArgoTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("argo_verify_argo_rollouts_controller_install",
		mcp.WithDescription("Verify that the Argo Rollouts controller is installed and running"),
		mcp.WithString("namespace", mcp.Description("The namespace where Argo Rollouts is installed")),
		mcp.WithString("label", mcp.Description("The label of the Argo Rollouts controller pods")),
	), handleVerifyArgoRolloutsControllerInstall)

	s.AddTool(mcp.NewTool("argo_verify_kubectl_plugin_install",
		mcp.WithDescription("Verify that the kubectl Argo Rollouts plugin is installed"),
	), handleVerifyKubectlPluginInstall)

	s.AddTool(mcp.NewTool("argo_promote_rollout",
		mcp.WithDescription("Promote a paused rollout to the next step"),
		mcp.WithString("rollout_name", mcp.Description("The name of the rollout to promote"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("The namespace of the rollout")),
		mcp.WithString("full", mcp.Description("Promote the rollout to the final step")),
	), handlePromoteRollout)

	s.AddTool(mcp.NewTool("argo_pause_rollout",
		mcp.WithDescription("Pause a rollout"),
		mcp.WithString("rollout_name", mcp.Description("The name of the rollout to pause"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("The namespace of the rollout")),
	), handlePauseRollout)

	s.AddTool(mcp.NewTool("argo_set_rollout_image",
		mcp.WithDescription("Set the image of a rollout"),
		mcp.WithString("rollout_name", mcp.Description("The name of the rollout to set the image for"), mcp.Required()),
		mcp.WithString("container_image", mcp.Description("The container image to set for the rollout"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("The namespace of the rollout")),
	), handleSetRolloutImage)

	s.AddTool(mcp.NewTool("argo_verify_gateway_plugin",
		mcp.WithDescription("Verify the installation status of the Argo Rollouts Gateway API plugin"),
		mcp.WithString("version", mcp.Description("The version of the plugin to check")),
		mcp.WithString("namespace", mcp.Description("The namespace for the plugin resources")),
		mcp.WithString("should_install", mcp.Description("Whether to install the plugin if not found")),
	), handleVerifyGatewayPlugin)

	s.AddTool(mcp.NewTool("argo_check_plugin_logs",
		mcp.WithDescription("Check the logs of the Argo Rollouts Gateway API plugin"),
		mcp.WithString("namespace", mcp.Description("The namespace of the plugin resources")),
		mcp.WithString("timeout", mcp.Description("Timeout for log collection in seconds")),
	), handleCheckPluginLogs)
}
