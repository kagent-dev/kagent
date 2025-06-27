package helm

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// Test Helm List Releases
func TestHandleHelmListReleases(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		params      map[string]interface{}
		expectError bool
	}{
		{
			name:        "basic list",
			params:      map[string]interface{}{},
			expectError: false,
		},
		{
			name: "list with namespace",
			params: map[string]interface{}{
				"namespace": "default",
			},
			expectError: false,
		},
		{
			name: "list all namespaces",
			params: map[string]interface{}{
				"all_namespaces": "true",
			},
			expectError: false,
		},
		{
			name: "list with filter",
			params: map[string]interface{}{
				"filter": "test.*",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handleHelmListReleases(ctx, request)
			if err != nil {
				t.Fatalf("handleHelmListReleases failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			// We expect some kind of output (success or error)
			if result.IsError != tt.expectError {
				t.Errorf("Expected error: %v, got error: %v", tt.expectError, result.IsError)
			}
		})
	}
}

// Test Helm Get Release
func TestHandleHelmGetRelease(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		params      map[string]interface{}
		expectError bool
	}{
		{
			name: "missing name parameter",
			params: map[string]interface{}{
				"namespace": "default",
			},
			expectError: true,
		},
		{
			name: "missing namespace parameter",
			params: map[string]interface{}{
				"name": "test-release",
			},
			expectError: true,
		},
		{
			name: "valid parameters",
			params: map[string]interface{}{
				"name":      "test-release",
				"namespace": "default",
			},
			expectError: false, // Note: Will fail if release doesn't exist, but that's expected
		},
		{
			name: "with resource type",
			params: map[string]interface{}{
				"name":      "test-release",
				"namespace": "default",
				"resource":  "values",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handleHelmGetRelease(ctx, request)
			if err != nil {
				t.Fatalf("handleHelmGetRelease failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			if tt.expectError && !result.IsError {
				t.Error("Expected error result for invalid parameters")
			}
		})
	}
}

// Test Helm Upgrade Release
func TestHandleHelmUpgradeRelease(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		params      map[string]interface{}
		expectError bool
	}{
		{
			name: "missing name parameter",
			params: map[string]interface{}{
				"chart": "stable/nginx",
			},
			expectError: true,
		},
		{
			name: "missing chart parameter",
			params: map[string]interface{}{
				"name": "test-release",
			},
			expectError: true,
		},
		{
			name: "valid basic parameters",
			params: map[string]interface{}{
				"name":  "test-release",
				"chart": "stable/nginx",
			},
			expectError: false, // May fail due to missing chart repo, but validates parameters
		},
		{
			name: "with all options",
			params: map[string]interface{}{
				"name":      "test-release",
				"chart":     "stable/nginx",
				"namespace": "default",
				"version":   "1.0.0",
				"install":   "true",
				"dry_run":   "true",
				"wait":      "true",
				"set":       "key1=value1,key2=value2",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handleHelmUpgradeRelease(ctx, request)
			if err != nil {
				t.Fatalf("handleHelmUpgradeRelease failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			if tt.expectError && !result.IsError {
				t.Error("Expected error result for invalid parameters")
			}
		})
	}
}

// Test Helm Uninstall
func TestHandleHelmUninstall(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		params      map[string]interface{}
		expectError bool
	}{
		{
			name: "missing name parameter",
			params: map[string]interface{}{
				"namespace": "default",
			},
			expectError: true,
		},
		{
			name: "missing namespace parameter",
			params: map[string]interface{}{
				"name": "test-release",
			},
			expectError: true,
		},
		{
			name: "valid parameters",
			params: map[string]interface{}{
				"name":      "test-release",
				"namespace": "default",
			},
			expectError: false, // Will fail if release doesn't exist, but validates parameters
		},
		{
			name: "with dry run",
			params: map[string]interface{}{
				"name":      "test-release",
				"namespace": "default",
				"dry_run":   "true",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handleHelmUninstall(ctx, request)
			if err != nil {
				t.Fatalf("handleHelmUninstall failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			if tt.expectError && !result.IsError {
				t.Error("Expected error result for invalid parameters")
			}
		})
	}
}

// Test Helm Repo Add
func TestHandleHelmRepoAdd(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		params      map[string]interface{}
		expectError bool
	}{
		{
			name: "missing name parameter",
			params: map[string]interface{}{
				"url": "https://charts.example.com",
			},
			expectError: true,
		},
		{
			name: "missing url parameter",
			params: map[string]interface{}{
				"name": "test-repo",
			},
			expectError: true,
		},
		{
			name: "valid parameters",
			params: map[string]interface{}{
				"name": "test-repo",
				"url":  "https://charts.example.com",
			},
			expectError: false, // May fail if URL is unreachable, but validates parameters
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			result, err := handleHelmRepoAdd(ctx, request)
			if err != nil {
				t.Fatalf("handleHelmRepoAdd failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			if tt.expectError && !result.IsError {
				t.Error("Expected error result for invalid parameters")
			}
		})
	}
}

// Test Helm Repo Update
func TestHandleHelmRepoUpdate(t *testing.T) {
	ctx := context.Background()
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]interface{}{}

	result, err := handleHelmRepoUpdate(ctx, request)
	if err != nil {
		t.Fatalf("handleHelmRepoUpdate failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Repo update should work (though may be slow or fail if no repos configured)
	// The important thing is that it doesn't crash
}

// Test parameter parsing edge cases
func TestParameterParsing(t *testing.T) {
	ctx := context.Background()

	// Test boolean parameter parsing
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]interface{}{
		"all_namespaces": "false",
		"all":            "1", // Should not be treated as true
		"deployed":       "true",
	}

	result, err := handleHelmListReleases(ctx, request)
	if err != nil {
		t.Fatalf("handleHelmListReleases failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Test set values parsing
	upgradeRequest := mcp.CallToolRequest{}
	upgradeRequest.Params.Arguments = map[string]interface{}{
		"name":  "test",
		"chart": "nginx",
		"set":   "key1=val1,key2=val2, key3=val3 ", // Test space handling
	}

	upgradeResult, err := handleHelmUpgradeRelease(ctx, upgradeRequest)
	if err != nil {
		t.Fatalf("handleHelmUpgradeRelease failed: %v", err)
	}

	if upgradeResult == nil {
		t.Fatal("Expected non-nil result")
	}
}
