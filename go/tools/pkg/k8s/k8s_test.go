package k8s

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/tools/pkg/utils"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"
)

// Helper function to create a test K8sTool with fake client
func newTestK8sTool(clientset kubernetes.Interface) *K8sTool {
	return &K8sTool{
		client: &K8sClient{
			clientset: clientset,
			config:    &rest.Config{},
		},
	}
}

// Helper function to create a test K8sTool with fake client and mock LLM
func newTestK8sToolWithLLM(clientset kubernetes.Interface, llm llms.Model) *K8sTool {
	return &K8sTool{
		client: &K8sClient{
			clientset: clientset,
			config:    &rest.Config{},
		},
		llmModel: llm,
	}
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

func TestNewK8sClient(t *testing.T) {
	// Test that NewK8sClient handles errors gracefully
	// This will likely fail in test environment without kubeconfig, which is expected
	_, err := NewK8sClient()
	// We don't fail the test if client creation fails, as it's expected in test env
	if err != nil {
		t.Logf("NewK8sClient failed as expected in test environment: %v", err)
	}
}

func TestFormatResourceOutput(t *testing.T) {
	testData := map[string]interface{}{
		"test":   "data",
		"number": 42,
	}

	// Test JSON output format
	result, err := formatResourceOutput(testData, "json")
	if err != nil {
		t.Fatalf("formatResourceOutput failed: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatal("Expected non-empty content")
	}

	// Test empty output format (defaults to JSON)
	result, err = formatResourceOutput(testData, "")
	if err != nil {
		t.Fatalf("formatResourceOutput with empty format failed: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatal("Expected non-empty content")
	}
}

func TestHandleGetAvailableAPIResources(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()

		// Mock the discovery client
		clientset.Fake.PrependReactor("get", "*", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
			return true, &corev1.PodList{}, nil
		})

		k8sTool := newTestK8sTool(clientset)

		req := mcp.CallToolRequest{}
		result, err := k8sTool.handleGetAvailableAPIResources(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		// Check that we got some content
		assert.NotEmpty(t, result.Content)
	})

	t.Run("error handling", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		clientset.Fake.PrependReactor("*", "*", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
			return true, nil, assert.AnError
		})

		k8sTool := newTestK8sTool(clientset)

		req := mcp.CallToolRequest{}
		result, err := k8sTool.handleGetAvailableAPIResources(ctx, req)
		assert.NoError(t, err) // MCP handlers should not return Go errors
		assert.NotNil(t, result)
		// Should handle the error gracefully
	})
}

func TestHandleScaleDeployment(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		deployment := &v1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-deployment",
				Namespace: "default",
			},
			Spec: v1.DeploymentSpec{
				Replicas: ptr.To(int32(3)),
			},
		}
		clientset := fake.NewSimpleClientset(deployment)

		k8sTool := newTestK8sTool(clientset)

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
	})

	t.Run("missing parameters", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		k8sTool := newTestK8sTool(clientset)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"name": "test-deployment",
			// Missing replicas parameter
		}

		result, err := k8sTool.handleScaleDeployment(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
	})
}

func TestHandleGetEvents(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		event := &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-event",
				Namespace: "default",
			},
			Message: "Test event message",
		}
		clientset := fake.NewSimpleClientset(event)

		k8sTool := newTestK8sTool(clientset)

		req := mcp.CallToolRequest{}
		result, err := k8sTool.handleGetEvents(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		resultText := getResultText(result)
		assert.Contains(t, resultText, "test-event")
	})

	t.Run("with namespace", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		k8sTool := newTestK8sTool(clientset)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"namespace": "custom-namespace",
		}

		result, err := k8sTool.handleGetEvents(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		// Should not error even if no events found
	})
}

func TestHandlePatchResource(t *testing.T) {
	ctx := context.Background()

	t.Run("missing parameters", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		k8sTool := newTestK8sTool(clientset)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "deployment",
			// Missing resource_name and patch
		}

		result, err := k8sTool.handlePatchResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
	})

	t.Run("valid parameters", func(t *testing.T) {
		deployment := &v1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-deployment",
				Namespace: "default",
			},
		}
		clientset := fake.NewSimpleClientset(deployment)
		k8sTool := newTestK8sTool(clientset)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "deployment",
			"resource_name": "test-deployment",
			"patch":         `{"spec":{"replicas":5}}`,
		}

		result, err := k8sTool.handlePatchResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		// Should attempt to patch (may fail in test env but validates parameters)
	})
}

func TestHandleDeleteResource(t *testing.T) {
	ctx := context.Background()

	t.Run("missing parameters", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		k8sTool := newTestK8sTool(clientset)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "pod",
			// Missing resource_name
		}

		result, err := k8sTool.handleDeleteResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
	})

	t.Run("valid parameters", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
			},
		}
		clientset := fake.NewSimpleClientset(pod)
		k8sTool := newTestK8sTool(clientset)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "pod",
			"resource_name": "test-pod",
		}

		result, err := k8sTool.handleDeleteResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		// Should attempt to delete (may succeed or fail depending on implementation)
	})
}

func TestHandleCheckServiceConnectivity(t *testing.T) {
	ctx := context.Background()

	t.Run("missing service_name", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		k8sTool := newTestK8sTool(clientset)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{}

		result, err := k8sTool.handleCheckServiceConnectivity(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
	})

	t.Run("valid service_name", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		k8sTool := newTestK8sTool(clientset)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"service_name": "test-service.default.svc.cluster.local:80",
		}

		result, err := k8sTool.handleCheckServiceConnectivity(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		// Should attempt connectivity check (will likely fail in test env but validates params)
	})
}

func TestHandleKubectlDescribeTool(t *testing.T) {
	ctx := context.Background()

	t.Run("missing parameters", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		k8sTool := newTestK8sTool(clientset)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "deployment",
			// Missing resource_name
		}

		result, err := k8sTool.handleKubectlDescribeTool(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
	})

	t.Run("valid parameters", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		k8sTool := newTestK8sTool(clientset)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "deployment",
			"resource_name": "test-deployment",
			"namespace":     "default",
		}

		result, err := k8sTool.handleKubectlDescribeTool(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		// Should attempt to describe (may fail in test env but validates parameters)
	})
}

func TestHandleGenerateResource(t *testing.T) {
	ctx := context.Background()

	t.Run("missing parameters", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		mockLLM := newMockLLM(&llms.ContentResponse{}, nil)
		k8sTool := newTestK8sToolWithLLM(clientset, mockLLM)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "istio_auth_policy",
			// Missing resource_description
		}

		result, err := k8sTool.handleGenerateResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "resource_type and resource_description parameters are required")
	})

	t.Run("valid istio auth policy generation", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		expectedResponse := `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: test-auth-policy
  namespace: default
spec:
  selector:
    matchLabels:
      app: test-app
  mtls:
    mode: STRICT`

		mockLLM := newMockLLM(&llms.ContentResponse{
			Choices: []*llms.ContentChoice{
				{Content: expectedResponse},
			},
		}, nil)
		k8sTool := newTestK8sToolWithLLM(clientset, mockLLM)

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
		assert.Equal(t, expectedResponse, resultText)

		// Verify the mock was called
		assert.Equal(t, 1, mockLLM.called)
	})

	t.Run("valid gateway api gateway generation", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		expectedResponse := `apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  name: test-gateway
  namespace: default
spec:
  gatewayClassName: istio
  listeners:
  - name: http
    port: 80`

		mockLLM := newMockLLM(&llms.ContentResponse{
			Choices: []*llms.ContentChoice{
				{Content: expectedResponse},
			},
		}, nil)
		k8sTool := newTestK8sToolWithLLM(clientset, mockLLM)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type":        "gateway_api_gateway",
			"resource_description": "A gateway for HTTP traffic",
		}

		result, err := k8sTool.handleGenerateResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		resultText := getResultText(result)
		assert.Equal(t, expectedResponse, resultText)

		// Verify the mock was called
		assert.Equal(t, 1, mockLLM.called)
	})

	t.Run("unsupported resource type", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		mockLLM := newMockLLM(&llms.ContentResponse{}, nil)
		k8sTool := newTestK8sToolWithLLM(clientset, mockLLM)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type":        "unsupported_resource_type",
			"resource_description": "Some description",
		}

		result, err := k8sTool.handleGenerateResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "resource type unsupported_resource_type not found")

		// Verify the mock was never called since validation failed
		assert.Equal(t, 0, mockLLM.called)
	})

	t.Run("LLM generation error", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		mockLLM := newMockLLM(nil, assert.AnError)
		k8sTool := newTestK8sToolWithLLM(clientset, mockLLM)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type":        "istio_auth_policy",
			"resource_description": "A peer authentication policy for strict mTLS",
		}

		result, err := k8sTool.handleGenerateResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "failed to generate content")

		// Verify the mock was called
		assert.Equal(t, 1, mockLLM.called)
	})

	t.Run("LLM empty response", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		mockLLM := newMockLLM(&llms.ContentResponse{
			Choices: []*llms.ContentChoice{}, // Empty choices
		}, nil)
		k8sTool := newTestK8sToolWithLLM(clientset, mockLLM)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type":        "istio_auth_policy",
			"resource_description": "A peer authentication policy for strict mTLS",
		}

		result, err := k8sTool.handleGenerateResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "empty response from model")

		// Verify the mock was called
		assert.Equal(t, 1, mockLLM.called)
	})
}

func TestHandleKubectlLogsEnhanced(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	k8sTool := newTestK8sTool(clientset)

	t.Run("missing pod_name", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		result, err := k8sTool.handleKubectlLogsEnhanced(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
	})

	t.Run("valid pod_name", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"pod_name": "test-pod"}
		result, err := k8sTool.handleKubectlLogsEnhanced(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
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

		clientset := fake.NewSimpleClientset()
		k8sTool := newTestK8sTool(clientset)

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

		clientset := fake.NewSimpleClientset()
		k8sTool := newTestK8sTool(clientset)

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

		clientset := fake.NewSimpleClientset()
		k8sTool := newTestK8sTool(clientset)

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

		clientset := fake.NewSimpleClientset()
		k8sTool := newTestK8sTool(clientset)

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

		// Verify no commands were executed
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

		clientset := fake.NewSimpleClientset()
		k8sTool := newTestK8sTool(clientset)

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

	t.Run("rollout status check", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `deployment "myapp" successfully rolled out`

		mock.AddCommandString("kubectl", []string{"rollout", "status", "deployment/myapp", "-n", "default"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		clientset := fake.NewSimpleClientset()
		k8sTool := newTestK8sTool(clientset)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"action":        "status",
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
		assert.Contains(t, content, "successfully rolled out")

		// Verify the correct kubectl command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "kubectl", callLog[0].Command)
		assert.Equal(t, []string{"rollout", "status", "deployment/myapp", "-n", "default"}, callLog[0].Args)
	})

	t.Run("missing required parameters", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		ctx := utils.WithShellExecutor(context.Background(), mock)

		clientset := fake.NewSimpleClientset()
		k8sTool := newTestK8sTool(clientset)

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

		// Verify no commands were executed
		callLog := mock.GetCallLog()
		assert.Len(t, callLog, 0)
	})
}

func TestHandleLabelResource(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	k8sTool := newTestK8sTool(clientset)

	t.Run("missing parameters", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		result, err := k8sTool.handleLabelResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
	})

	t.Run("valid parameters", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"resource_type": "pod", "resource_name": "test-pod", "labels": "app=test"}
		result, err := k8sTool.handleLabelResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})
}

func TestHandleAnnotateResource(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	k8sTool := newTestK8sTool(clientset)

	t.Run("missing parameters", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		result, err := k8sTool.handleAnnotateResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
	})

	t.Run("valid parameters", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"resource_type": "pod", "resource_name": "test-pod", "annotations": "foo=bar"}
		result, err := k8sTool.handleAnnotateResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})
}

func TestHandleRemoveAnnotation(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	k8sTool := newTestK8sTool(clientset)

	t.Run("missing parameters", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		result, err := k8sTool.handleRemoveAnnotation(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
	})

	t.Run("valid parameters", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"resource_type": "pod", "resource_name": "test-pod", "annotation_key": "foo"}
		result, err := k8sTool.handleRemoveAnnotation(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})
}

func TestHandleRemoveLabel(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	k8sTool := newTestK8sTool(clientset)

	t.Run("missing parameters", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		result, err := k8sTool.handleRemoveLabel(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
	})

	t.Run("valid parameters", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"resource_type": "pod", "resource_name": "test-pod", "label_key": "foo"}
		result, err := k8sTool.handleRemoveLabel(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})
}

func TestHandleCreateResourceFromURL(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	k8sTool := newTestK8sTool(clientset)

	t.Run("missing url", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		result, err := k8sTool.handleCreateResourceFromURL(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
	})

	t.Run("valid url", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"url": "http://example.com/manifest.yaml"}
		result, err := k8sTool.handleCreateResourceFromURL(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})
}

func TestHandleGetClusterConfiguration(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	k8sTool := newTestK8sTool(clientset)

	req := mcp.CallToolRequest{}
	result, err := k8sTool.handleGetClusterConfiguration(ctx, req)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestHandleGetResourceYAML(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	k8sTool := newTestK8sTool(clientset)
	// This handler is registered as an anonymous func, so we test the logic directly
	// Simulate the parameters
	resourceType := "pod"
	resourceName := "test-pod"
	namespace := "default"

	args := []string{"get", resourceType, resourceName, "-o", "yaml", "-n", namespace}
	result, err := k8sTool.runKubectlCommand(ctx, args)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestHandleKubectlGetTool(t *testing.T) {
	t.Run("success with mocked kubectl", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		expectedOutput := `NAME      READY   STATUS    RESTARTS   AGE
pod1      1/1     Running   0          1d
pod2      1/1     Running   0          2d`

		mock.AddCommandString("kubectl", []string{"get", "pods", "-n", "default", "-o", "json"}, expectedOutput, nil)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		clientset := fake.NewSimpleClientset()
		k8sTool := newTestK8sTool(clientset)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "pods",
			"namespace":     "default",
			"output":        "json",
		}

		result, err := k8sTool.handleKubectlGetTool(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		// Verify the expected output
		content := getResultText(result)
		assert.Contains(t, content, "pod1")
		assert.Contains(t, content, "pod2")

		// Verify the correct kubectl command was called
		callLog := mock.GetCallLog()
		require.Len(t, callLog, 1)
		assert.Equal(t, "kubectl", callLog[0].Command)
		assert.Equal(t, []string{"get", "pods", "-n", "default", "-o", "json"}, callLog[0].Args)
	})

	t.Run("kubectl command failure", func(t *testing.T) {
		mock := utils.NewMockShellExecutor()
		mock.AddCommandString("kubectl", []string{"get", "invalidresource", "-o", "json"}, "", assert.AnError)
		ctx := utils.WithShellExecutor(context.Background(), mock)

		clientset := fake.NewSimpleClientset()
		k8sTool := newTestK8sTool(clientset)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "invalidresource",
		}

		result, err := k8sTool.handleKubectlGetTool(ctx, req)
		assert.NoError(t, err) // MCP handlers should not return Go errors
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "command kubectl failed")
	})
}

func newMockLLM(response *llms.ContentResponse, err error) *mockLLM {
	return &mockLLM{
		called:   0,
		response: response,
		error:    err,
	}
}

// not synchronized, don't use concurrently!
type mockLLM struct {
	called   int
	response *llms.ContentResponse
	error    error
}

func (m *mockLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return llms.GenerateFromSinglePrompt(ctx, m, prompt, options...)
}

func (m *mockLLM) GenerateContent(ctx context.Context, _ []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	var opts llms.CallOptions
	for _, opt := range options {
		opt(&opts)
	}

	if opts.StreamingFunc != nil && len(m.response.Choices) > 0 {
		if err := opts.StreamingFunc(ctx, []byte(m.response.Choices[0].Content)); err != nil {
			return nil, err
		}
	}

	m.called++

	return m.response, m.error
}
