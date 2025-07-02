package helm

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/tools/pkg/utils"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Helm List Releases
func TestHandleHelmListReleases(t *testing.T) {
	t.Run("basic list releases", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `NAME    NAMESPACE   REVISION    UPDATED                                 STATUS      CHART           APP VERSION
app1    default     1           2023-01-01 12:00:00.000000000 +0000 UTC    deployed    myapp-1.0.0     1.0.0
app2    kube-system 2           2023-01-02 12:00:00.000000000 +0000 UTC    deployed    system-2.0.0    2.0.0`

		mock.AddCommandString("helm", []string{"list"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		result, err := handleHelmListReleases(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		// Verify the expected output
		content := getResultText(result)
		assert.Contains(t, content, "app1")
		assert.Contains(t, content, "app2")

		// Verify the correct command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "helm", callLog[0].Command)
		assert.Equal(t, []string{"list"}, callLog[0].Args)
	})

	t.Run("list releases with namespace", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		mock.AddCommandString("helm", []string{"list", "-n", "production"}, "production releases", nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"namespace": "production",
		}

		result, err := handleHelmListReleases(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)

		// Verify the correct command was called with namespace
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "helm", callLog[0].Command)
		assert.Equal(t, []string{"list", "-n", "production"}, callLog[0].Args)
	})

	t.Run("list releases with all namespaces", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		mock.AddCommandString("helm", []string{"list", "-A"}, "all namespaces releases", nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"all_namespaces": "true",
		}

		result, err := handleHelmListReleases(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)

		// Verify the correct command was called with -A flag
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "helm", callLog[0].Command)
		assert.Equal(t, []string{"list", "-A"}, callLog[0].Args)
	})

	t.Run("list releases with multiple flags", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		mock.AddCommandString("helm", []string{"list", "-A", "-a", "--failed", "-o", "json"}, `[{"name":"failed-app","status":"failed"}]`, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"all_namespaces": "true",
			"all":            "true",
			"failed":         "true",
			"output":         "json",
		}

		result, err := handleHelmListReleases(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)

		// Verify the correct command was called with multiple flags
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "helm", callLog[0].Command)
		assert.Equal(t, []string{"list", "-A", "-a", "--failed", "-o", "json"}, callLog[0].Args)
	})

	t.Run("helm command failure", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		mock.AddCommandString("helm", []string{"list"}, "", assert.AnError)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		result, err := handleHelmListReleases(ctx, request)

		assert.NoError(t, err) // MCP handlers should not return Go errors
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "Helm list command failed")
	})
}

// Test Helm Get Release
func TestHandleHelmGetRelease(t *testing.T) {
	t.Run("get release all resources", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `REVISION: 1
RELEASED: Mon Jan 01 12:00:00 UTC 2023
CHART: myapp-1.0.0
VALUES:
replicaCount: 3`

		mock.AddCommandString("helm", []string{"get", "all", "myapp", "-n", "default"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"name":      "myapp",
			"namespace": "default",
		}

		result, err := handleHelmGetRelease(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, getResultText(result), "REVISION: 1")

		// Verify the correct command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "helm", callLog[0].Command)
		assert.Equal(t, []string{"get", "all", "myapp", "-n", "default"}, callLog[0].Args)
	})

	t.Run("get release values only", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		mock.AddCommandString("helm", []string{"get", "values", "myapp", "-n", "default"}, "replicaCount: 3", nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"name":      "myapp",
			"namespace": "default",
			"resource":  "values",
		}

		result, err := handleHelmGetRelease(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)

		// Verify the correct command was called with values resource
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "helm", callLog[0].Command)
		assert.Equal(t, []string{"get", "values", "myapp", "-n", "default"}, callLog[0].Args)
	})

	t.Run("missing required parameters", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		// Test missing name
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"namespace": "default",
		}

		result, err := handleHelmGetRelease(ctx, request)
		assert.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "name parameter is required")

		// Test missing namespace
		request.Params.Arguments = map[string]interface{}{
			"name": "myapp",
		}

		result, err = handleHelmGetRelease(ctx, request)
		assert.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "namespace parameter is required")

		// Verify no commands were executed
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})
}

// Test Helm Upgrade Release
func TestHandleHelmUpgradeRelease(t *testing.T) {
	t.Run("basic upgrade", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `Release "myapp" has been upgraded. Happy Helming!
NAME: myapp
LAST DEPLOYED: Mon Jan 01 12:00:00 UTC 2023
NAMESPACE: default
STATUS: deployed
REVISION: 2`

		mock.AddCommandString("helm", []string{"upgrade", "myapp", "stable/myapp"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"name":  "myapp",
			"chart": "stable/myapp",
		}

		result, err := handleHelmUpgradeRelease(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, getResultText(result), "has been upgraded")

		// Verify the correct command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "helm", callLog[0].Command)
		assert.Equal(t, []string{"upgrade", "myapp", "stable/myapp"}, callLog[0].Args)
	})

	t.Run("upgrade with all options", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedArgs := []string{
			"upgrade", "myapp", "stable/myapp",
			"-n", "production",
			"--version", "1.2.0",
			"-f", "values.yaml",
			"--set", "replicas=5",
			"--set", "image.tag=v1.2.0",
			"--install",
			"--dry-run",
			"--wait",
		}
		mock.AddCommandString("helm", expectedArgs, "dry run output", nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"name":      "myapp",
			"chart":     "stable/myapp",
			"namespace": "production",
			"version":   "1.2.0",
			"values":    "values.yaml",
			"set":       "replicas=5,image.tag=v1.2.0",
			"install":   "true",
			"dry_run":   "true",
			"wait":      "true",
		}

		result, err := handleHelmUpgradeRelease(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)

		// Verify the correct command was called with all options
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "helm", callLog[0].Command)
		assert.Equal(t, expectedArgs, callLog[0].Args)
	})

	t.Run("missing required parameters", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"name": "myapp",
			// Missing chart
		}

		result, err := handleHelmUpgradeRelease(ctx, request)
		assert.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "name and chart parameters are required")

		// Verify no commands were executed
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})
}

// Test Helm Uninstall
func TestHandleHelmUninstall(t *testing.T) {
	t.Run("basic uninstall", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `release "myapp" uninstalled`

		mock.AddCommandString("helm", []string{"uninstall", "myapp", "-n", "default"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"name":      "myapp",
			"namespace": "default",
		}

		result, err := handleHelmUninstall(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, getResultText(result), "uninstalled")

		// Verify the correct command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "helm", callLog[0].Command)
		assert.Equal(t, []string{"uninstall", "myapp", "-n", "default"}, callLog[0].Args)
	})

	t.Run("uninstall with options", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedArgs := []string{"uninstall", "myapp", "-n", "production", "--dry-run", "--wait"}
		mock.AddCommandString("helm", expectedArgs, "dry run uninstall", nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"name":      "myapp",
			"namespace": "production",
			"dry_run":   "true",
			"wait":      "true",
		}

		result, err := handleHelmUninstall(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)

		// Verify the correct command was called with options
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "helm", callLog[0].Command)
		assert.Equal(t, expectedArgs, callLog[0].Args)
	})
}

// Test Helm Repo Add
func TestHandleHelmRepoAdd(t *testing.T) {
	t.Run("add repository", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `"stable" has been added to your repositories`

		mock.AddCommandString("helm", []string{"repo", "add", "stable", "https://charts.helm.sh/stable"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"name": "stable",
			"url":  "https://charts.helm.sh/stable",
		}

		result, err := handleHelmRepoAdd(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, getResultText(result), "has been added")

		// Verify the correct command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "helm", callLog[0].Command)
		assert.Equal(t, []string{"repo", "add", "stable", "https://charts.helm.sh/stable"}, callLog[0].Args)
	})

	t.Run("missing required parameters", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"name": "stable",
			// Missing url
		}

		result, err := handleHelmRepoAdd(ctx, request)
		assert.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "name and url parameters are required")

		// Verify no commands were executed
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})
}

// Test Helm Repo Update
func TestHandleHelmRepoUpdate(t *testing.T) {
	t.Run("update repositories", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `Hang tight while we grab the latest from your chart repositories...
...Successfully got an update from the "stable" chart repository
Update Complete. ⎈Happy Helming!⎈`

		mock.AddCommandString("helm", []string{"repo", "update"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		result, err := handleHelmRepoUpdate(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, getResultText(result), "Update Complete")

		// Verify the correct command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "helm", callLog[0].Command)
		assert.Equal(t, []string{"repo", "update"}, callLog[0].Args)
	})
}

// Helper function to extract text content from MCP result
func getResultText(result *mcp.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	if textContent, ok := result.Content[0].(mcp.TextContent); ok {
		return textContent.Text
	}
	return ""
}
