package argo

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// Test the actual MCP tool handler functions

func TestHandlePromoteRollout(t *testing.T) {
	ctx := context.Background()

	// Test missing rollout_name parameter
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]interface{}{}

	result, err := handlePromoteRollout(ctx, request)
	if err != nil {
		t.Fatalf("handlePromoteRollout failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Should return error result for missing rollout_name
	if !result.IsError {
		t.Error("Expected error result for missing rollout_name")
	}
}

func TestHandlePauseRollout(t *testing.T) {
	ctx := context.Background()

	// Test missing rollout_name parameter
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]interface{}{}

	result, err := handlePauseRollout(ctx, request)
	if err != nil {
		t.Fatalf("handlePauseRollout failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Should return error result for missing rollout_name
	if !result.IsError {
		t.Error("Expected error result for missing rollout_name")
	}
}

func TestHandleSetRolloutImage(t *testing.T) {
	ctx := context.Background()

	// Test missing rollout_name parameter
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]interface{}{
		"container_image": "nginx:latest",
	}

	result, err := handleSetRolloutImage(ctx, request)
	if err != nil {
		t.Fatalf("handleSetRolloutImage failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Should return error result for missing rollout_name
	if !result.IsError {
		t.Error("Expected error result for missing rollout_name")
	}

	// Test missing container_image parameter
	request2 := mcp.CallToolRequest{}
	request2.Params.Arguments = map[string]interface{}{
		"rollout_name": "my-rollout",
	}

	result2, err := handleSetRolloutImage(ctx, request2)
	if err != nil {
		t.Fatalf("handleSetRolloutImage failed: %v", err)
	}

	if result2 == nil {
		t.Fatal("Expected non-nil result")
	}

	// Should return error result for missing container_image
	if !result2.IsError {
		t.Error("Expected error result for missing container_image")
	}
}

func TestGetSystemArchitecture(t *testing.T) {
	arch, err := getSystemArchitecture()
	if err != nil {
		t.Fatalf("getSystemArchitecture failed: %v", err)
	}

	if arch == "" {
		t.Error("Expected non-empty architecture")
	}

	// Architecture should contain system info
	if len(arch) < 5 {
		t.Errorf("Expected architecture string to be reasonable length, got: %s", arch)
	}
}

func TestGetLatestVersion(t *testing.T) {
	version := getLatestVersion()
	if version == "" {
		t.Error("Expected non-empty version")
	}

	// Should return at least the default version
	if version != "0.5.0" && len(version) < 3 {
		t.Errorf("Expected valid version format, got: %s", version)
	}
}

func TestGatewayPluginStatus(t *testing.T) {
	status := GatewayPluginStatus{
		Installed:    true,
		Version:      "0.5.0",
		Architecture: "linux-amd64",
		DownloadTime: 1.5,
	}

	str := status.String()
	if str == "" {
		t.Error("Expected non-empty string representation")
	}

	// Should be valid JSON
	if !strings.Contains(str, "installed") {
		t.Error("Expected string to contain 'installed' field")
	}
}

func TestHandleVerifyGatewayPlugin(t *testing.T) {
	ctx := context.Background()

	// Test with should_install=false
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]interface{}{
		"should_install": "false",
		"namespace":      "argo-rollouts",
	}

	result, err := handleVerifyGatewayPlugin(ctx, request)
	if err != nil {
		t.Fatalf("handleVerifyGatewayPlugin failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Should return result (may be success or error depending on kubectl availability)
	if len(result.Content) == 0 {
		t.Error("Expected content in result")
	}
}

func TestConfigureGatewayPlugin(t *testing.T) {
	// Test plugin configuration (will likely fail due to kubectl dependency, but tests the logic)
	status := configureGatewayPlugin("0.5.0", "test-namespace")

	// Should return a status object
	if status.Version != "0.5.0" && status.ErrorMessage == "" {
		t.Error("Expected either version to be set or error message")
	}

	// Test with empty version (should use latest)
	status2 := configureGatewayPlugin("", "test-namespace")
	if status2.ErrorMessage == "" && status2.Version == "" {
		t.Error("Expected either error or version to be set")
	}
}

func TestHandleVerifyArgoRolloutsControllerInstall(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		params map[string]interface{}
	}{
		{
			name:   "default parameters",
			params: map[string]interface{}{},
		},
		{
			name: "custom namespace",
			params: map[string]interface{}{
				"namespace": "custom-argo",
			},
		},
		{
			name: "custom label",
			params: map[string]interface{}{
				"label": "app=custom-rollouts",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handleVerifyArgoRolloutsControllerInstall(ctx, request)
			if err != nil {
				t.Fatalf("handleVerifyArgoRolloutsControllerInstall failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			// Should return some result (may be error if kubectl not available)
			if len(result.Content) == 0 {
				t.Error("Expected content in result")
			}
		})
	}
}

func TestHandleVerifyKubectlPluginInstall(t *testing.T) {
	ctx := context.Background()
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]interface{}{}

	result, err := handleVerifyKubectlPluginInstall(ctx, request)
	if err != nil {
		t.Fatalf("handleVerifyKubectlPluginInstall failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Should return some result (may be error if kubectl plugin not available)
	if len(result.Content) == 0 {
		t.Error("Expected content in result")
	}
}

func TestHandleCheckPluginLogs(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		params map[string]interface{}
	}{
		{
			name:   "default parameters",
			params: map[string]interface{}{},
		},
		{
			name: "custom namespace",
			params: map[string]interface{}{
				"namespace": "custom-argo",
			},
		},
		{
			name: "with timeout",
			params: map[string]interface{}{
				"timeout": "30",
			},
		},
		{
			name: "invalid timeout",
			params: map[string]interface{}{
				"timeout": "invalid",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handleCheckPluginLogs(ctx, request)
			if err != nil {
				t.Fatalf("handleCheckPluginLogs failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			// Should return some result (may be error if kubectl not available)
			if len(result.Content) == 0 {
				t.Error("Expected content in result")
			}
		})
	}
}

func TestHandlePromoteRolloutValidParams(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		params map[string]interface{}
	}{
		{
			name: "basic rollout",
			params: map[string]interface{}{
				"rollout_name": "test-rollout",
			},
		},
		{
			name: "with namespace",
			params: map[string]interface{}{
				"rollout_name": "test-rollout",
				"namespace":    "prod",
			},
		},
		{
			name: "with full promote",
			params: map[string]interface{}{
				"rollout_name": "test-rollout",
				"full":         "true",
			},
		},
		{
			name: "full false",
			params: map[string]interface{}{
				"rollout_name": "test-rollout",
				"full":         "false",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handlePromoteRollout(ctx, request)
			if err != nil {
				t.Fatalf("handlePromoteRollout failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			// Should return some result (may be error if kubectl/rollout not available)
			if len(result.Content) == 0 {
				t.Error("Expected content in result")
			}
		})
	}
}

func TestHandlePauseRolloutValidParams(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		params map[string]interface{}
	}{
		{
			name: "basic rollout",
			params: map[string]interface{}{
				"rollout_name": "test-rollout",
			},
		},
		{
			name: "with namespace",
			params: map[string]interface{}{
				"rollout_name": "test-rollout",
				"namespace":    "prod",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handlePauseRollout(ctx, request)
			if err != nil {
				t.Fatalf("handlePauseRollout failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			// Should return some result (may be error if kubectl/rollout not available)
			if len(result.Content) == 0 {
				t.Error("Expected content in result")
			}
		})
	}
}

func TestHandleSetRolloutImageValidParams(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		params map[string]interface{}
	}{
		{
			name: "basic image set",
			params: map[string]interface{}{
				"rollout_name":    "test-rollout",
				"container_image": "nginx:latest",
			},
		},
		{
			name: "with namespace",
			params: map[string]interface{}{
				"rollout_name":    "test-rollout",
				"container_image": "nginx:1.20",
				"namespace":       "prod",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handleSetRolloutImage(ctx, request)
			if err != nil {
				t.Fatalf("handleSetRolloutImage failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			// Should return some result (may be error if kubectl/rollout not available)
			if len(result.Content) == 0 {
				t.Error("Expected content in result")
			}
		})
	}
}
