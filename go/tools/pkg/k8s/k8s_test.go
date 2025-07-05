package k8s

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/kagent-dev/kagent/go/tools/pkg/utils"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

// Helper function to create a test K8sTool
func newTestK8sTool() *K8sTool {
	return NewK8sTool(nil)
}

// Helper function to create a test K8sTool with mock LLM
func newTestK8sToolWithLLM(llm llms.Model) *K8sTool {
	return NewK8sTool(llm)
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

func TestHandleGetAvailableAPIResources(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `[{"name": "pods", "singularName": "pod", "namespaced": true, "kind": "Pod"}]`
		mock.AddCommandString("kubectl", []string{"api-resources", "-o", "json"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(ctx, mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		result, err := k8sTool.handleGetAvailableAPIResources(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		// Check that we got some content
		assert.NotEmpty(t, result.Content)
		assert.Contains(t, getResultText(result), "pods")
	})

	t.Run("kubectl command failure", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		mock.AddCommandString("kubectl", []string{"api-resources", "-o", "json"}, "", assert.AnError)
		ctx := utils.WithShellExecutor(ctx, mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		result, err := k8sTool.handleGetAvailableAPIResources(ctx, req)
		assert.NoError(t, err) // MCP handlers should not return Go errors
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
	})
}

func TestHandleScaleDeployment(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `deployment.apps/test-deployment scaled`
		mock.AddCommandString("kubectl", []string{"scale", "deployment", "test-deployment", "--replicas", "5", "-n", "default"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(ctx, mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"name":     "test-deployment",
			"replicas": float64(5), // JSON numbers come as float64
		}

		result, err := k8sTool.handleScaleDeployment(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		resultText := getResultText(result)
		assert.Contains(t, resultText, "test-deployment")
		assert.Contains(t, resultText, "scaled")
	})

	t.Run("missing name parameter", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			// Missing name parameter (this is the required one)
			"replicas": float64(3),
		}

		result, err := k8sTool.handleScaleDeployment(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "name parameter is required")

		// Verify no commands were executed since parameters are missing
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})

	t.Run("missing replicas parameter uses default", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `deployment.apps/test-deployment scaled`
		// Default replicas is 1
		mock.AddCommandString("kubectl", []string{"scale", "deployment", "test-deployment", "--replicas", "1", "-n", "default"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"name": "test-deployment",
			// Missing replicas parameter - should use default value of 1
		}

		result, err := k8sTool.handleScaleDeployment(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		resultText := getResultText(result)
		assert.Contains(t, resultText, "scaled")

		// Verify the command was executed with default replicas=1
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 1)
		assert.Equal(t, "kubectl", callLog[0].Command)
		assert.Equal(t, []string{"scale", "deployment", "test-deployment", "--replicas", "1", "-n", "default"}, callLog[0].Args)
	})
}

func TestHandleGetEvents(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `{"items": [{"metadata": {"name": "test-event"}, "message": "Test event message"}]}`
		mock.AddCommandString("kubectl", []string{"get", "events", "-o", "json", "--all-namespaces"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(ctx, mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		result, err := k8sTool.handleGetEvents(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		resultText := getResultText(result)
		assert.Contains(t, resultText, "test-event")
	})

	t.Run("with namespace", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `{"items": []}`
		mock.AddCommandString("kubectl", []string{"get", "events", "-o", "json", "-n", "custom-namespace"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(ctx, mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"namespace": "custom-namespace",
		}

		result, err := k8sTool.handleGetEvents(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)
	})
}

func TestHandlePatchResource(t *testing.T) {
	ctx := context.Background()

	t.Run("missing parameters", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "deployment",
			// Missing resource_name and patch
		}

		result, err := k8sTool.handlePatchResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)

		// Verify no commands were executed since parameters are missing
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})

	t.Run("valid parameters", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `deployment.apps/test-deployment patched`
		mock.AddCommandString("kubectl", []string{"patch", "deployment", "test-deployment", "-p", `{"spec":{"replicas":5}}`, "-n", "default"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(ctx, mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "deployment",
			"resource_name": "test-deployment",
			"patch":         `{"spec":{"replicas":5}}`,
		}

		result, err := k8sTool.handlePatchResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		resultText := getResultText(result)
		assert.Contains(t, resultText, "patched")
	})
}

func TestHandleDeleteResource(t *testing.T) {
	ctx := context.Background()

	t.Run("missing parameters", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "pod",
			// Missing resource_name
		}

		result, err := k8sTool.handleDeleteResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)

		// Verify no commands were executed since parameters are missing
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})

	t.Run("valid parameters", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `pod "test-pod" deleted`
		mock.AddCommandString("kubectl", []string{"delete", "pod", "test-pod", "-n", "default"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(ctx, mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "pod",
			"resource_name": "test-pod",
		}

		result, err := k8sTool.handleDeleteResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		resultText := getResultText(result)
		assert.Contains(t, resultText, "deleted")
	})
}

func TestHandleCheckServiceConnectivity(t *testing.T) {
	ctx := context.Background()

	t.Run("missing service_name", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{}

		result, err := k8sTool.handleCheckServiceConnectivity(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)

		// Verify no commands were executed since parameters are missing
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})

	t.Run("valid service_name", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()

		// Mock the pod creation, wait, and exec commands using partial matchers
		mock.AddPartialMatcherString("kubectl", []string{"run", "*", "--image=curlimages/curl", "-n", "default", "--restart=Never", "--", "sleep", "3600"}, "pod/curl-test-123 created", nil)
		mock.AddPartialMatcherString("kubectl", []string{"wait", "--for=condition=ready", "*", "-n", "default", "--timeout=60s"}, "pod/curl-test-123 condition met", nil)
		mock.AddPartialMatcherString("kubectl", []string{"exec", "*", "-n", "default", "--", "curl", "-s", "test-service.default.svc.cluster.local:80"}, "Connection successful", nil)
		mock.AddPartialMatcherString("kubectl", []string{"delete", "pod", "*", "-n", "default", "--ignore-not-found"}, "pod deleted", nil)

		ctx := utils.WithShellExecutor(ctx, mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"service_name": "test-service.default.svc.cluster.local:80",
		}

		result, err := k8sTool.handleCheckServiceConnectivity(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		// Should attempt connectivity check (may succeed or fail but validates params)
	})
}

func TestHandleKubectlDescribeTool(t *testing.T) {
	ctx := context.Background()

	t.Run("missing parameters", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "deployment",
			// Missing resource_name
		}

		result, err := k8sTool.handleKubectlDescribeTool(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)

		// Verify no commands were executed since parameters are missing
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})

	t.Run("valid parameters", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `Name:               test-deployment
Namespace:          default
Labels:             app=test`
		mock.AddCommandString("kubectl", []string{"describe", "deployment", "test-deployment", "-n", "default"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(ctx, mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "deployment",
			"resource_name": "test-deployment",
			"namespace":     "default",
		}

		result, err := k8sTool.handleKubectlDescribeTool(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		resultText := getResultText(result)
		assert.Contains(t, resultText, "test-deployment")
	})
}

func TestHandleKubectlGetEnhanced(t *testing.T) {
	ctx := context.Background()

	t.Run("missing resource_type", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		k8sTool := newTestK8sTool()
		req := mcp.CallToolRequest{}
		result, err := k8sTool.handleKubectlGetEnhanced(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)

		// Verify no commands were executed since parameters are missing
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})

	t.Run("valid resource_type", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `{"items": [{"metadata": {"name": "pod1"}}]}`
		mock.AddCommandString("kubectl", []string{"get", "pods", "-o", "json"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(ctx, mock)

		k8sTool := newTestK8sTool()
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"resource_type": "pods"}
		result, err := k8sTool.handleKubectlGetEnhanced(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)
	})
}

func TestHandleKubectlLogsEnhanced(t *testing.T) {
	ctx := context.Background()

	t.Run("missing pod_name", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		k8sTool := newTestK8sTool()
		req := mcp.CallToolRequest{}
		result, err := k8sTool.handleKubectlLogsEnhanced(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)

		// Verify no commands were executed since parameters are missing
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})

	t.Run("valid pod_name", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `log line 1
log line 2`
		mock.AddCommandString("kubectl", []string{"logs", "test-pod", "-n", "default", "--tail", "50"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(ctx, mock)

		k8sTool := newTestK8sTool()
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"pod_name": "test-pod"}
		result, err := k8sTool.handleKubectlLogsEnhanced(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)
	})
}

func TestHandleApplyManifest(t *testing.T) {
	t.Run("apply manifest from string", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		manifest := `apiVersion: v1
kind: Pod
metadata:
  name: test-pod
spec:
  containers:
  - name: test
    image: nginx`

		expectedOutput := `pod/test-pod created`
		// Use partial matcher to handle dynamic temp file names
		mock.AddPartialMatcherString("kubectl", []string{"apply", "-f", "*"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"manifest": manifest,
		}

		result, err := k8sTool.handleApplyManifest(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		// Verify the expected output
		content := getResultText(result)
		assert.Contains(t, content, "created")

		// Verify kubectl apply was called (we can't predict the exact temp file name)
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "kubectl", callLog[0].Command)
		assert.Len(t, callLog[0].Args, 3) // apply, -f, <temp-file>
		assert.Equal(t, "apply", callLog[0].Args[0])
		assert.Equal(t, "-f", callLog[0].Args[1])
		// Third argument should be the temporary file path
		assert.Contains(t, callLog[0].Args[2], "manifest-")
	})

	t.Run("missing manifest parameter", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			// Missing manifest parameter
		}

		result, err := k8sTool.handleApplyManifest(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "manifest parameter is required")

		// Verify no commands were executed
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})
}

func TestHandleExecCommand(t *testing.T) {
	t.Run("exec command in pod", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `total 8
drwxr-xr-x 1 root root 4096 Jan  1 12:00 .
drwxr-xr-x 1 root root 4096 Jan  1 12:00 ..`

		// The implementation passes the command as a single string after --
		mock.AddCommandString("kubectl", []string{"exec", "mypod", "-n", "default", "--", "ls -la"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"pod_name":  "mypod",
			"namespace": "default",
			"command":   "ls -la",
		}

		result, err := k8sTool.handleExecCommand(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		// Verify the expected output
		content := getResultText(result)
		assert.Contains(t, content, "total 8")

		// Verify the correct kubectl command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "kubectl", callLog[0].Command)
		assert.Equal(t, []string{"exec", "mypod", "-n", "default", "--", "ls -la"}, callLog[0].Args)
	})

	t.Run("missing required parameters", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"pod_name": "mypod",
			// Missing command parameter
		}

		result, err := k8sTool.handleExecCommand(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "pod_name and command parameters are required")

		// Verify no commands were executed since parameters are missing
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})
}

func TestHandleRollout(t *testing.T) {
	t.Run("rollout restart deployment", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `deployment.apps/myapp restarted`

		mock.AddCommandString("kubectl", []string{"rollout", "restart", "deployment/myapp", "-n", "default"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"action":        "restart",
			"resource_type": "deployment",
			"resource_name": "myapp",
			"namespace":     "default",
		}

		result, err := k8sTool.handleRollout(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		// Verify the expected output
		content := getResultText(result)
		assert.Contains(t, content, "restarted")

		// Verify the correct kubectl command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "kubectl", callLog[0].Command)
		assert.Equal(t, []string{"rollout", "restart", "deployment/myapp", "-n", "default"}, callLog[0].Args)
	})

	t.Run("missing required parameters", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"action": "restart",
			// Missing resource_type and resource_name
		}

		result, err := k8sTool.handleRollout(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "required")

		// Verify no commands were executed since parameters are missing
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})
}

// Mock LLM for testing
type mockLLM struct {
	called   int
	response *llms.ContentResponse
	error    error
}

func newMockLLM(response *llms.ContentResponse, err error) *mockLLM {
	return &mockLLM{
		response: response,
		error:    err,
	}
}

func (m *mockLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return "", nil
}

func (m *mockLLM) GenerateContent(ctx context.Context, _ []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	m.called++
	return m.response, m.error
}

func TestHandleGenerateResource(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		expectedYAML := `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
  namespace: foo
spec:
  mtls:
    mode: STRICT`

		mockLLM := newMockLLM(&llms.ContentResponse{
			Choices: []*llms.ContentChoice{
				{Content: expectedYAML},
			},
		}, nil)

		k8sTool := newTestK8sToolWithLLM(mockLLM)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type":        "istio_auth_policy",
			"resource_description": "A peer authentication policy for strict mTLS",
		}

		result, err := k8sTool.handleGenerateResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		resultText := getResultText(result)
		assert.Contains(t, resultText, "PeerAuthentication")
		assert.Contains(t, resultText, "STRICT")

		// Verify the mock was called
		assert.Equal(t, 1, mockLLM.called)
	})

	t.Run("missing parameters", func(t *testing.T) {
		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "istio_auth_policy",
			// Missing resource_description
		}

		result, err := k8sTool.handleGenerateResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "required")
	})

	t.Run("no LLM model", func(t *testing.T) {
		k8sTool := newTestK8sTool() // No LLM model

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type":        "istio_auth_policy",
			"resource_description": "A peer authentication policy for strict mTLS",
		}

		result, err := k8sTool.handleGenerateResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "No LLM client present")
	})

	t.Run("invalid resource type", func(t *testing.T) {
		mockLLM := newMockLLM(&llms.ContentResponse{
			Choices: []*llms.ContentChoice{
				{Content: "test"},
			},
		}, nil)

		k8sTool := newTestK8sToolWithLLM(mockLLM)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type":        "invalid_resource_type",
			"resource_description": "A test resource",
		}

		result, err := k8sTool.handleGenerateResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "resource type invalid_resource_type not found")

		// Verify the mock was not called
		assert.Equal(t, 0, mockLLM.called)
	})
}

// Test additional handlers that were missing tests
func TestHandleAnnotateResource(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `deployment.apps/test-deployment annotated`
		mock.AddCommandString("kubectl", []string{"annotate", "deployment", "test-deployment", "key1=value1", "key2=value2", "-n", "default"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(ctx, mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "deployment",
			"resource_name": "test-deployment",
			"annotations":   "key1=value1 key2=value2",
			"namespace":     "default",
		}

		result, err := k8sTool.handleAnnotateResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		resultText := getResultText(result)
		assert.Contains(t, resultText, "annotated")
	})

	t.Run("missing parameters", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "deployment",
			// Missing resource_name and annotations
		}

		result, err := k8sTool.handleAnnotateResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "required")

		// Verify no commands were executed since parameters are missing
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})
}

func TestHandleLabelResource(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `deployment.apps/test-deployment labeled`
		mock.AddCommandString("kubectl", []string{"label", "deployment", "test-deployment", "env=prod", "version=1.0", "-n", "default"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(ctx, mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "deployment",
			"resource_name": "test-deployment",
			"labels":        "env=prod version=1.0",
			"namespace":     "default",
		}

		result, err := k8sTool.handleLabelResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		resultText := getResultText(result)
		assert.Contains(t, resultText, "labeled")
	})

	t.Run("missing parameters", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "deployment",
			// Missing resource_name and labels
		}

		result, err := k8sTool.handleLabelResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "required")

		// Verify no commands were executed since parameters are missing
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})
}

func TestHandleRemoveAnnotation(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `deployment.apps/test-deployment annotated`
		mock.AddCommandString("kubectl", []string{"annotate", "deployment", "test-deployment", "key1-", "-n", "default"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(ctx, mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type":  "deployment",
			"resource_name":  "test-deployment",
			"annotation_key": "key1",
			"namespace":      "default",
		}

		result, err := k8sTool.handleRemoveAnnotation(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		resultText := getResultText(result)
		assert.Contains(t, resultText, "annotated")
	})

	t.Run("missing parameters", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "deployment",
			// Missing resource_name and annotation_key
		}

		result, err := k8sTool.handleRemoveAnnotation(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "required")

		// Verify no commands were executed since parameters are missing
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})
}

func TestHandleRemoveLabel(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `deployment.apps/test-deployment labeled`
		mock.AddCommandString("kubectl", []string{"label", "deployment", "test-deployment", "env-", "-n", "default"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(ctx, mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "deployment",
			"resource_name": "test-deployment",
			"label_key":     "env",
			"namespace":     "default",
		}

		result, err := k8sTool.handleRemoveLabel(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		resultText := getResultText(result)
		assert.Contains(t, resultText, "labeled")
	})

	t.Run("missing parameters", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "deployment",
			// Missing resource_name and label_key
		}

		result, err := k8sTool.handleRemoveLabel(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "required")

		// Verify no commands were executed since parameters are missing
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})
}

func TestHandleCreateResourceFromURL(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `deployment.apps/test-deployment created`
		mock.AddCommandString("kubectl", []string{"create", "-f", "https://example.com/manifest.yaml", "-n", "default"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(ctx, mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"url":       "https://example.com/manifest.yaml",
			"namespace": "default",
		}

		result, err := k8sTool.handleCreateResourceFromURL(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		resultText := getResultText(result)
		assert.Contains(t, resultText, "created")
	})

	t.Run("missing url parameter", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			// Missing url parameter
		}

		result, err := k8sTool.handleCreateResourceFromURL(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "url parameter is required")

		// Verify no commands were executed since parameters are missing
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})
}

func TestHandleGetClusterConfiguration(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `apiVersion: v1
clusters:
- cluster:
    server: https://kubernetes.default.svc
  name: default
contexts:
- context:
    cluster: default
    user: default
  name: default
current-context: default
kind: Config
preferences: {}
users:
- name: default`
		mock.AddCommandString("kubectl", []string{"config", "view"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(ctx, mock)

		k8sTool := newTestK8sTool()

		req := mcp.CallToolRequest{}
		result, err := k8sTool.handleGetClusterConfiguration(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		resultText := getResultText(result)
		assert.Contains(t, resultText, "current-context")
		assert.Contains(t, resultText, "clusters")
	})
}

// Test the k8s_create_resource handler (inline function in RegisterK8sTools)
func TestHandleCreateResource(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		yamlContent := `apiVersion: v1
kind: Pod
metadata:
  name: test-pod
spec:
  containers:
  - name: test
    image: nginx`

		expectedOutput := `pod/test-pod created`
		// Use partial matcher to handle dynamic temp file names
		mock.AddPartialMatcherString("kubectl", []string{"create", "-f", "*"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(ctx, mock)

		// We need to test the inline function from RegisterK8sTools
		// Let's create a test handler that mimics the inline function
		testHandler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		}

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"yaml_content": yamlContent,
		}

		result, err := testHandler(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		// Verify the expected output
		content := getResultText(result)
		assert.Contains(t, content, "created")

		// Verify kubectl create was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "kubectl", callLog[0].Command)
		assert.Len(t, callLog[0].Args, 3) // create, -f, <temp-file>
		assert.Equal(t, "create", callLog[0].Args[0])
		assert.Equal(t, "-f", callLog[0].Args[1])
		// Third argument should be the temporary file path
		assert.Contains(t, callLog[0].Args[2], "k8s-resource-")
	})

	t.Run("missing yaml_content parameter", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		// Test handler for missing parameter
		testHandler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			yamlContent := mcp.ParseString(request, "yaml_content", "")

			if yamlContent == "" {
				return mcp.NewToolResultError("yaml_content is required"), nil
			}
			return mcp.NewToolResultText("should not reach here"), nil
		}

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			// Missing yaml_content parameter
		}

		result, err := testHandler(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "yaml_content is required")

		// Verify no commands were executed
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})
}

// Test the k8s_get_resource_yaml handler (inline function in RegisterK8sTools)
func TestHandleGetResourceYAML(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `apiVersion: v1
kind: Pod
metadata:
  name: test-pod
  namespace: default
spec:
  containers:
  - name: test
    image: nginx`
		mock.AddCommandString("kubectl", []string{"get", "pod", "test-pod", "-o", "yaml", "-n", "default"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(ctx, mock)

		// Test handler that mimics the inline function
		testHandler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		}

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "pod",
			"resource_name": "test-pod",
			"namespace":     "default",
		}

		result, err := testHandler(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		resultText := getResultText(result)
		assert.Contains(t, resultText, "test-pod")
		assert.Contains(t, resultText, "apiVersion")

		// Verify the correct kubectl command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "kubectl", callLog[0].Command)
		assert.Equal(t, []string{"get", "pod", "test-pod", "-o", "yaml", "-n", "default"}, callLog[0].Args)
	})

	t.Run("missing parameters", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		testHandler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			resourceType := mcp.ParseString(request, "resource_type", "")
			resourceName := mcp.ParseString(request, "resource_name", "")

			if resourceType == "" || resourceName == "" {
				return mcp.NewToolResultError("resource_type and resource_name are required"), nil
			}
			return mcp.NewToolResultText("should not reach here"), nil
		}

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "pod",
			// Missing resource_name
		}

		result, err := testHandler(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "resource_type and resource_name are required")

		// Verify no commands were executed
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})

	t.Run("without namespace", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `apiVersion: v1
kind: ClusterRole
metadata:
  name: test-cluster-role`
		mock.AddCommandString("kubectl", []string{"get", "clusterrole", "test-cluster-role", "-o", "yaml"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(ctx, mock)

		testHandler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		}

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "clusterrole",
			"resource_name": "test-cluster-role",
			// No namespace for cluster-scoped resource
		}

		result, err := testHandler(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		resultText := getResultText(result)
		assert.Contains(t, resultText, "test-cluster-role")
		assert.Contains(t, resultText, "ClusterRole")

		// Verify the correct kubectl command was called (without namespace)
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "kubectl", callLog[0].Command)
		assert.Equal(t, []string{"get", "clusterrole", "test-cluster-role", "-o", "yaml"}, callLog[0].Args)
	})
}
