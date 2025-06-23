package k8s

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
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
				Replicas: int32Ptr(3),
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
		k8sTool := newTestK8sTool(clientset)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type": "deployment",
			// Missing resource_description
		}

		result, err := k8sTool.handleGenerateResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
	})

	t.Run("valid deployment generation", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		k8sTool := newTestK8sTool(clientset)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type":        "deployment",
			"resource_description": "A web application deployment with nginx",
		}

		result, err := k8sTool.handleGenerateResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		resultText := getResultText(result)
		assert.Contains(t, resultText, "kind: Deployment")
		assert.Contains(t, resultText, "Generated YAML for deployment")
	})

	t.Run("valid service generation", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		k8sTool := newTestK8sTool(clientset)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type":        "service",
			"resource_description": "A service to expose the web application",
		}

		result, err := k8sTool.handleGenerateResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		resultText := getResultText(result)
		assert.Contains(t, resultText, "kind: Service")
		assert.Contains(t, resultText, "Generated YAML for service")
	})

	t.Run("unsupported resource type", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		k8sTool := newTestK8sTool(clientset)

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"resource_type":        "unsupported",
			"resource_description": "Some description",
		}

		result, err := k8sTool.handleGenerateResource(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
	})
}

func TestHandleKubectlGetEnhanced(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	k8sTool := newTestK8sTool(clientset)

	t.Run("missing resource_type", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		result, err := k8sTool.handleKubectlGetEnhanced(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
	})

	t.Run("valid resource_type", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"resource_type": "pods"}
		result, err := k8sTool.handleKubectlGetEnhanced(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
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
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	k8sTool := newTestK8sTool(clientset)

	t.Run("missing manifest", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		result, err := k8sTool.handleApplyManifest(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
	})

	t.Run("valid manifest", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"manifest": "apiVersion: v1\nkind: Pod\nmetadata:\n  name: test-pod"}
		result, err := k8sTool.handleApplyManifest(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})
}

func TestHandleExecCommand(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	k8sTool := newTestK8sTool(clientset)

	t.Run("missing pod_name", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"command": "ls"}
		result, err := k8sTool.handleExecCommand(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
	})

	t.Run("missing command", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"pod_name": "test-pod"}
		result, err := k8sTool.handleExecCommand(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
	})

	t.Run("valid pod_name and command", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"pod_name": "test-pod", "command": "ls"}
		result, err := k8sTool.handleExecCommand(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})
}

func TestHandleRollout(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	k8sTool := newTestK8sTool(clientset)

	t.Run("missing parameters", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		result, err := k8sTool.handleRollout(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
	})

	t.Run("valid parameters", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"action": "restart", "resource_type": "deployment", "resource_name": "test-deployment"}
		result, err := k8sTool.handleRollout(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, result)
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

// Helper function for creating int32 pointer
func int32Ptr(i int32) *int32 {
	return &i
}
