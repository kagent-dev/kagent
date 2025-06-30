package istio

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/tools/pkg/utils"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// Test Istio Proxy Status
func TestHandleIstioProxyStatus(t *testing.T) {
	t.Run("basic proxy status", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `NAME     CDS      LDS      EDS      RDS      ISTIOD                      VERSION
app-1    SYNCED   SYNCED   SYNCED   SYNCED   istiod-68d5d5b5fc-7vf6n     1.18.0
app-2    SYNCED   SYNCED   SYNCED   SYNCED   istiod-68d5d5b5fc-7vf6n     1.18.0`

		mock.AddCommandString("istioctl", []string{"proxy-status"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		result, err := handleIstioProxyStatus(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		// Verify the expected output
		content := getResultText(result)
		assert.Contains(t, content, "app-1")
		assert.Contains(t, content, "SYNCED")

		// Verify the correct command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"proxy-status"}, callLog[0].Args)
	})

	t.Run("proxy status with namespace", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `NAME     CDS      LDS      EDS      RDS      ISTIOD                      VERSION
app-1    SYNCED   SYNCED   SYNCED   SYNCED   istiod-68d5d5b5fc-7vf6n     1.18.0`

		mock.AddCommandString("istioctl", []string{"proxy-status", "-n", "production"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"namespace": "production",
		}

		result, err := handleIstioProxyStatus(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)

		// Verify the correct command was called with namespace
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"proxy-status", "-n", "production"}, callLog[0].Args)
	})

	t.Run("proxy status with pod name and namespace", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `NAME     CDS      LDS      EDS      RDS      ISTIOD                      VERSION
app-1    SYNCED   SYNCED   SYNCED   SYNCED   istiod-68d5d5b5fc-7vf6n     1.18.0`

		mock.AddCommandString("istioctl", []string{"proxy-status", "-n", "production", "app-1"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"namespace": "production",
			"pod_name":  "app-1",
		}

		result, err := handleIstioProxyStatus(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)

		// Verify the correct command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"proxy-status", "-n", "production", "app-1"}, callLog[0].Args)
	})

	t.Run("istioctl command failure", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		mock.AddCommandString("istioctl", []string{"proxy-status"}, "", assert.AnError)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		result, err := handleIstioProxyStatus(ctx, request)

		assert.NoError(t, err) // MCP handlers should not return Go errors
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "istioctl proxy-status failed")
	})
}

// Test Istio Proxy Config
func TestHandleIstioProxyConfig(t *testing.T) {
	t.Run("proxy config all", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `CLUSTER NAME     DIRECTION     TYPE           DESTINATION RULE
outbound|80||kubernetes.default.svc.cluster.local     outbound      EDS
inbound|80||     inbound       EDS`

		mock.AddCommandString("istioctl", []string{"proxy-config", "all", "app-1"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"pod_name": "app-1",
		}

		result, err := handleIstioProxyConfig(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, getResultText(result), "CLUSTER NAME")

		// Verify the correct command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"proxy-config", "all", "app-1"}, callLog[0].Args)
	})

	t.Run("proxy config with namespace and config type", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `CLUSTER NAME     DIRECTION     TYPE           DESTINATION RULE
outbound|80||kubernetes.default.svc.cluster.local     outbound      EDS`

		mock.AddCommandString("istioctl", []string{"proxy-config", "cluster", "app-1.production"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"pod_name":    "app-1",
			"namespace":   "production",
			"config_type": "cluster",
		}

		result, err := handleIstioProxyConfig(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)

		// Verify the correct command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"proxy-config", "cluster", "app-1.production"}, callLog[0].Args)
	})

	t.Run("missing required parameters", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			// Missing pod_name
		}

		result, err := handleIstioProxyConfig(ctx, request)
		assert.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "pod_name parameter is required")

		// Verify no commands were executed
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})
}

// Test Istio Install
func TestHandleIstioInstall(t *testing.T) {
	t.Run("basic install", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `✔ Istio core installed
✔ Istiod installed
✔ Ingress gateways installed
✔ Installation complete`

		mock.AddCommandString("istioctl", []string{"install", "--set", "profile=default", "-y"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		result, err := handleIstioInstall(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, getResultText(result), "Installation complete")

		// Verify the correct command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"install", "--set", "profile=default", "-y"}, callLog[0].Args)
	})

	t.Run("install with profile", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `✔ Istio core installed
✔ Installation complete`

		mock.AddCommandString("istioctl", []string{"install", "--set", "profile=minimal", "-y"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"profile": "minimal",
		}

		result, err := handleIstioInstall(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)

		// Verify the correct command was called with profile
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"install", "--set", "profile=minimal", "-y"}, callLog[0].Args)
	})
}

// Test Istio Analyze
func TestHandleIstioAnalyzeClusterConfiguration(t *testing.T) {
	t.Run("basic analyze", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `✔ No validation issues found when analyzing namespace: default.`

		mock.AddCommandString("istioctl", []string{"analyze"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		result, err := handleIstioAnalyzeClusterConfiguration(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, getResultText(result), "No validation issues found")

		// Verify the correct command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"analyze"}, callLog[0].Args)
	})

	t.Run("analyze with namespace", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `✔ No validation issues found when analyzing namespace: production.`

		mock.AddCommandString("istioctl", []string{"analyze", "-n", "production"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"namespace": "production",
		}

		result, err := handleIstioAnalyzeClusterConfiguration(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)

		// Verify the correct command was called with namespace
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"analyze", "-n", "production"}, callLog[0].Args)
	})

	t.Run("analyze all namespaces", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `✔ No validation issues found when analyzing all namespaces.`

		mock.AddCommandString("istioctl", []string{"analyze", "-A"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"all_namespaces": "true",
		}

		result, err := handleIstioAnalyzeClusterConfiguration(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)

		// Verify the correct command was called with -A flag
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"analyze", "-A"}, callLog[0].Args)
	})
}

// Test Istio Version
func TestHandleIstioVersion(t *testing.T) {
	t.Run("version detailed output", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `client version: 1.18.0
control plane version: 1.18.0
data plane version: 1.18.0 (2 proxies)`

		mock.AddCommandString("istioctl", []string{"version"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		result, err := handleIstioVersion(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, getResultText(result), "client version: 1.18.0")

		// Verify the correct command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"version"}, callLog[0].Args)
	})

	t.Run("version short output", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `1.18.0`

		mock.AddCommandString("istioctl", []string{"version", "--short"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"short": "true",
		}

		result, err := handleIstioVersion(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, getResultText(result), "1.18.0")

		// Verify the correct command was called with --short flag
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"version", "--short"}, callLog[0].Args)
	})
}

// Test Waypoint List
func TestHandleWaypointList(t *testing.T) {
	t.Run("list waypoints", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `NAMESPACE   NAME        TRAFFIC TYPE
default     waypoint    ALL
production  waypoint    INBOUND`

		mock.AddCommandString("istioctl", []string{"waypoint", "list"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		result, err := handleWaypointList(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, getResultText(result), "NAMESPACE")
		assert.Contains(t, getResultText(result), "waypoint")

		// Verify the correct command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"waypoint", "list"}, callLog[0].Args)
	})

	t.Run("list waypoints in namespace", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `NAMESPACE   NAME        TRAFFIC TYPE
production  waypoint    INBOUND`

		mock.AddCommandString("istioctl", []string{"waypoint", "list", "-n", "production"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"namespace": "production",
		}

		result, err := handleWaypointList(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)

		// Verify the correct command was called with namespace
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"waypoint", "list", "-n", "production"}, callLog[0].Args)
	})
}

// Test Waypoint Generate
func TestHandleWaypointGenerate(t *testing.T) {
	t.Run("generate waypoint", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  name: waypoint
  namespace: production
spec:
  gatewayClassName: istio-waypoint`

		mock.AddCommandString("istioctl", []string{"waypoint", "generate", "waypoint", "-n", "production", "--for", "all"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"namespace": "production",
		}

		result, err := handleWaypointGenerate(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, getResultText(result), "apiVersion: gateway.networking.k8s.io/v1beta1")

		// Verify the correct command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"waypoint", "generate", "waypoint", "-n", "production", "--for", "all"}, callLog[0].Args)
	})

	t.Run("missing required parameters", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			// Missing namespace
		}

		result, err := handleWaypointGenerate(ctx, request)
		assert.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "namespace parameter is required")

		// Verify no commands were executed
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})
}

// Test Waypoint Apply
func TestHandleWaypointApply(t *testing.T) {
	t.Run("basic waypoint apply", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `waypoint/waypoint applied`

		mock.AddCommandString("istioctl", []string{"waypoint", "apply", "-n", "default"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"namespace": "default",
		}

		result, err := handleWaypointApply(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)
		assert.Contains(t, getResultText(result), "applied")

		// Verify the correct command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"waypoint", "apply", "-n", "default"}, callLog[0].Args)
	})

	t.Run("waypoint apply with enroll namespace", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `waypoint/waypoint applied
namespace/default labeled with istio.io/use-waypoint=waypoint`

		mock.AddCommandString("istioctl", []string{"waypoint", "apply", "-n", "default", "--enroll-namespace"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"namespace":        "default",
			"enroll_namespace": "true",
		}

		result, err := handleWaypointApply(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, getResultText(result), "applied")

		// Verify the correct command was called with --enroll-namespace flag
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"waypoint", "apply", "-n", "default", "--enroll-namespace"}, callLog[0].Args)
	})

	t.Run("missing namespace parameter", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			// Missing namespace
		}

		result, err := handleWaypointApply(ctx, request)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "namespace parameter is required")

		// Verify no commands were executed
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})

	t.Run("istioctl command failure", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		mock.AddCommandString("istioctl", []string{"waypoint", "apply", "-n", "default"}, "", assert.AnError)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"namespace": "default",
		}

		result, err := handleWaypointApply(ctx, request)

		assert.NoError(t, err) // MCP handlers should not return Go errors
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "istioctl waypoint apply failed")
	})
}

// Test Waypoint Delete
func TestHandleWaypointDelete(t *testing.T) {
	t.Run("delete all waypoints", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `waypoint/waypoint deleted`

		mock.AddCommandString("istioctl", []string{"waypoint", "delete", "--all", "-n", "default"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"namespace": "default",
			"all":       "true",
		}

		result, err := handleWaypointDelete(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)
		assert.Contains(t, getResultText(result), "deleted")

		// Verify the correct command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"waypoint", "delete", "--all", "-n", "default"}, callLog[0].Args)
	})

	t.Run("delete specific waypoints", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `waypoint/waypoint1 deleted
waypoint/waypoint2 deleted`

		mock.AddCommandString("istioctl", []string{"waypoint", "delete", "waypoint1", "waypoint2", "-n", "default"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"namespace": "default",
			"names":     "waypoint1,waypoint2",
		}

		result, err := handleWaypointDelete(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, getResultText(result), "deleted")

		// Verify the correct command was called with specific names
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"waypoint", "delete", "waypoint1", "waypoint2", "-n", "default"}, callLog[0].Args)
	})

	t.Run("missing namespace parameter", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			// Missing namespace
		}

		result, err := handleWaypointDelete(ctx, request)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "namespace parameter is required")

		// Verify no commands were executed
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})

	t.Run("istioctl command failure", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		mock.AddCommandString("istioctl", []string{"waypoint", "delete", "--all", "-n", "default"}, "", assert.AnError)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"namespace": "default",
			"all":       "true",
		}

		result, err := handleWaypointDelete(ctx, request)

		assert.NoError(t, err) // MCP handlers should not return Go errors
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "istioctl waypoint delete failed")
	})
}

// Test Waypoint Status
func TestHandleWaypointStatus(t *testing.T) {
	t.Run("waypoint status", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `waypoint/waypoint is deployed and ready`

		mock.AddCommandString("istioctl", []string{"waypoint", "status", "-n", "default"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"namespace": "default",
		}

		result, err := handleWaypointStatus(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)
		assert.Contains(t, getResultText(result), "waypoint")

		// Verify the correct command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"waypoint", "status", "-n", "default"}, callLog[0].Args)
	})

	t.Run("waypoint status with specific name", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `waypoint/test-waypoint is deployed and ready`

		mock.AddCommandString("istioctl", []string{"waypoint", "status", "test-waypoint", "-n", "default"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"namespace": "default",
			"name":      "test-waypoint",
		}

		result, err := handleWaypointStatus(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, getResultText(result), "test-waypoint")

		// Verify the correct command was called with specific name
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"waypoint", "status", "test-waypoint", "-n", "default"}, callLog[0].Args)
	})

	t.Run("missing namespace parameter", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			// Missing namespace
		}

		result, err := handleWaypointStatus(ctx, request)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "namespace parameter is required")

		// Verify no commands were executed
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})

	t.Run("istioctl command failure", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		mock.AddCommandString("istioctl", []string{"waypoint", "status", "-n", "default"}, "", assert.AnError)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"namespace": "default",
		}

		result, err := handleWaypointStatus(ctx, request)

		assert.NoError(t, err) // MCP handlers should not return Go errors
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "istioctl waypoint status failed")
	})
}

// Test Ztunnel Config
func TestHandleZtunnelConfig(t *testing.T) {
	t.Run("default ztunnel config", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `CLUSTER_NAME     CLUSTER_TYPE     ENDPOINTS
cluster1         EDS              10.0.0.1:15010
cluster2         STATIC           10.0.0.2:15010`

		mock.AddCommandString("istioctl", []string{"ztunnel-config", "all"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		result, err := handleZtunnelConfig(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)
		assert.Contains(t, getResultText(result), "CLUSTER_NAME")

		// Verify the correct command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"ztunnel-config", "all"}, callLog[0].Args)
	})

	t.Run("ztunnel config with namespace", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `CLUSTER_NAME     CLUSTER_TYPE     ENDPOINTS
cluster1         EDS              10.0.0.1:15010`

		mock.AddCommandString("istioctl", []string{"ztunnel-config", "all", "-n", "istio-system"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"namespace": "istio-system",
		}

		result, err := handleZtunnelConfig(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)

		// Verify the correct command was called with namespace
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"ztunnel-config", "all", "-n", "istio-system"}, callLog[0].Args)
	})

	t.Run("ztunnel config with specific type", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `CLUSTER_NAME     CLUSTER_TYPE     ENDPOINTS
cluster1         EDS              10.0.0.1:15010`

		mock.AddCommandString("istioctl", []string{"ztunnel-config", "cluster"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"config_type": "cluster",
		}

		result, err := handleZtunnelConfig(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)

		// Verify the correct command was called with specific config type
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"ztunnel-config", "cluster"}, callLog[0].Args)
	})

	t.Run("ztunnel config with namespace and config type", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `LISTENER_NAME     ADDRESS          PORT     TYPE
listener1         0.0.0.0          15006    TCP`

		mock.AddCommandString("istioctl", []string{"ztunnel-config", "listener", "-n", "istio-system"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"namespace":   "istio-system",
			"config_type": "listener",
		}

		result, err := handleZtunnelConfig(ctx, request)

		assert.NoError(t, err)
		assert.False(t, result.IsError)

		// Verify the correct command was called with both namespace and config type
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "istioctl", callLog[0].Command)
		assert.Equal(t, []string{"ztunnel-config", "listener", "-n", "istio-system"}, callLog[0].Args)
	})

	t.Run("istioctl command failure", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		mock.AddCommandString("istioctl", []string{"ztunnel-config", "all"}, "", assert.AnError)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		request := mcp.CallToolRequest{}
		result, err := handleZtunnelConfig(ctx, request)

		assert.NoError(t, err) // MCP handlers should not return Go errors
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "istioctl ztunnel-config failed")
	})
}
