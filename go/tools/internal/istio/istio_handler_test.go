package istio

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// Test all Istio MCP tool handlers

func TestHandleIstioProxyStatus(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		params map[string]interface{}
	}{
		{
			name:   "no parameters",
			params: map[string]interface{}{},
		},
		{
			name: "with pod name",
			params: map[string]interface{}{
				"pod_name": "test-pod",
			},
		},
		{
			name: "with namespace",
			params: map[string]interface{}{
				"namespace": "istio-system",
			},
		},
		{
			name: "with both pod and namespace",
			params: map[string]interface{}{
				"pod_name":  "test-pod",
				"namespace": "default",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handleIstioProxyStatus(ctx, request)
			if err != nil {
				t.Fatalf("handleIstioProxyStatus failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			// Should return some result (may be error if istioctl not available)
			if len(result.Content) == 0 {
				t.Error("Expected content in result")
			}
		})
	}
}

func TestHandleIstioProxyConfig(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		params      map[string]interface{}
		expectError bool
	}{
		{
			name: "missing pod_name parameter",
			params: map[string]interface{}{
				"namespace": "default",
			},
			expectError: true,
		},
		{
			name: "valid pod_name",
			params: map[string]interface{}{
				"pod_name": "test-pod",
			},
			expectError: false,
		},
		{
			name: "with config_type",
			params: map[string]interface{}{
				"pod_name":    "test-pod",
				"config_type": "cluster",
			},
			expectError: false,
		},
		{
			name: "with namespace",
			params: map[string]interface{}{
				"pod_name":  "test-pod",
				"namespace": "istio-system",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handleIstioProxyConfig(ctx, request)
			if err != nil {
				t.Fatalf("handleIstioProxyConfig failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			if tt.expectError && !result.IsError {
				t.Error("Expected error result for missing pod_name")
			}
		})
	}
}

func TestHandleIstioInstall(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		params map[string]interface{}
	}{
		{
			name:   "default profile",
			params: map[string]interface{}{},
		},
		{
			name: "demo profile",
			params: map[string]interface{}{
				"profile": "demo",
			},
		},
		{
			name: "minimal profile",
			params: map[string]interface{}{
				"profile": "minimal",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handleIstioInstall(ctx, request)
			if err != nil {
				t.Fatalf("handleIstioInstall failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			// Should return some result (may be error if istioctl not available)
			if len(result.Content) == 0 {
				t.Error("Expected content in result")
			}
		})
	}
}

func TestHandleIstioGenerateManifest(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		params map[string]interface{}
	}{
		{
			name:   "default profile",
			params: map[string]interface{}{},
		},
		{
			name: "ambient profile",
			params: map[string]interface{}{
				"profile": "ambient",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handleIstioGenerateManifest(ctx, request)
			if err != nil {
				t.Fatalf("handleIstioGenerateManifest failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			// Should return some result (may be error if istioctl not available)
			if len(result.Content) == 0 {
				t.Error("Expected content in result")
			}
		})
	}
}

func TestHandleIstioAnalyzeClusterConfiguration(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		params map[string]interface{}
	}{
		{
			name:   "no parameters",
			params: map[string]interface{}{},
		},
		{
			name: "specific namespace",
			params: map[string]interface{}{
				"namespace": "default",
			},
		},
		{
			name: "all namespaces",
			params: map[string]interface{}{
				"all_namespaces": "true",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handleIstioAnalyzeClusterConfiguration(ctx, request)
			if err != nil {
				t.Fatalf("handleIstioAnalyzeClusterConfiguration failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			// Should return some result (may be error if istioctl not available)
			if len(result.Content) == 0 {
				t.Error("Expected content in result")
			}
		})
	}
}

func TestHandleIstioVersion(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		params map[string]interface{}
	}{
		{
			name:   "default version",
			params: map[string]interface{}{},
		},
		{
			name: "short version",
			params: map[string]interface{}{
				"short": "true",
			},
		},
		{
			name: "short false",
			params: map[string]interface{}{
				"short": "false",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handleIstioVersion(ctx, request)
			if err != nil {
				t.Fatalf("handleIstioVersion failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			// Should return some result (may be error if istioctl not available)
			if len(result.Content) == 0 {
				t.Error("Expected content in result")
			}
		})
	}
}

func TestHandleIstioRemoteClusters(t *testing.T) {
	ctx := context.Background()
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]interface{}{}

	result, err := handleIstioRemoteClusters(ctx, request)
	if err != nil {
		t.Fatalf("handleIstioRemoteClusters failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Should return some result (may be error if istioctl not available)
	if len(result.Content) == 0 {
		t.Error("Expected content in result")
	}
}

func TestHandleWaypointList(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		params map[string]interface{}
	}{
		{
			name:   "no parameters",
			params: map[string]interface{}{},
		},
		{
			name: "specific namespace",
			params: map[string]interface{}{
				"namespace": "default",
			},
		},
		{
			name: "all namespaces",
			params: map[string]interface{}{
				"all_namespaces": "true",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handleWaypointList(ctx, request)
			if err != nil {
				t.Fatalf("handleWaypointList failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			// Should return some result (may be error if istioctl not available)
			if len(result.Content) == 0 {
				t.Error("Expected content in result")
			}
		})
	}
}

func TestHandleWaypointGenerate(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		params      map[string]interface{}
		expectError bool
	}{
		{
			name: "missing namespace parameter",
			params: map[string]interface{}{
				"name": "test-waypoint",
			},
			expectError: true,
		},
		{
			name: "valid namespace",
			params: map[string]interface{}{
				"namespace": "default",
			},
			expectError: false,
		},
		{
			name: "with name and traffic type",
			params: map[string]interface{}{
				"namespace":    "default",
				"name":         "test-waypoint",
				"traffic_type": "inbound",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handleWaypointGenerate(ctx, request)
			if err != nil {
				t.Fatalf("handleWaypointGenerate failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			if tt.expectError && !result.IsError {
				t.Error("Expected error result for missing namespace")
			}
		})
	}
}

func TestHandleWaypointApply(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		params      map[string]interface{}
		expectError bool
	}{
		{
			name:        "missing namespace parameter",
			params:      map[string]interface{}{},
			expectError: true,
		},
		{
			name: "valid namespace",
			params: map[string]interface{}{
				"namespace": "default",
			},
			expectError: false,
		},
		{
			name: "with enroll namespace",
			params: map[string]interface{}{
				"namespace":        "default",
				"enroll_namespace": "true",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handleWaypointApply(ctx, request)
			if err != nil {
				t.Fatalf("handleWaypointApply failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			if tt.expectError && !result.IsError {
				t.Error("Expected error result for missing namespace")
			}
		})
	}
}

func TestHandleWaypointDelete(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		params      map[string]interface{}
		expectError bool
	}{
		{
			name:        "missing namespace parameter",
			params:      map[string]interface{}{},
			expectError: true,
		},
		{
			name: "valid namespace with all",
			params: map[string]interface{}{
				"namespace": "default",
				"all":       "true",
			},
			expectError: false,
		},
		{
			name: "valid namespace with names",
			params: map[string]interface{}{
				"namespace": "default",
				"names":     "waypoint1,waypoint2",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handleWaypointDelete(ctx, request)
			if err != nil {
				t.Fatalf("handleWaypointDelete failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			if tt.expectError && !result.IsError {
				t.Error("Expected error result for missing namespace")
			}
		})
	}
}

func TestHandleWaypointStatus(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		params      map[string]interface{}
		expectError bool
	}{
		{
			name:        "missing namespace parameter",
			params:      map[string]interface{}{},
			expectError: true,
		},
		{
			name: "valid namespace",
			params: map[string]interface{}{
				"namespace": "default",
			},
			expectError: false,
		},
		{
			name: "with waypoint name",
			params: map[string]interface{}{
				"namespace": "default",
				"name":      "test-waypoint",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handleWaypointStatus(ctx, request)
			if err != nil {
				t.Fatalf("handleWaypointStatus failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			if tt.expectError && !result.IsError {
				t.Error("Expected error result for missing namespace")
			}
		})
	}
}

func TestHandleZtunnelConfig(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		params map[string]interface{}
	}{
		{
			name:   "default config type",
			params: map[string]interface{}{},
		},
		{
			name: "with namespace",
			params: map[string]interface{}{
				"namespace": "istio-system",
			},
		},
		{
			name: "with config type",
			params: map[string]interface{}{
				"config_type": "cluster",
			},
		},
		{
			name: "with both namespace and config type",
			params: map[string]interface{}{
				"namespace":   "istio-system",
				"config_type": "listener",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handleZtunnelConfig(ctx, request)
			if err != nil {
				t.Fatalf("handleZtunnelConfig failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			// Should return some result (may be error if istioctl not available)
			if len(result.Content) == 0 {
				t.Error("Expected content in result")
			}
		})
	}
}
