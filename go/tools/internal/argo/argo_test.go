package argo

import (
	"strings"
	"testing"
)

func TestArgoCommandExecution(t *testing.T) {
	// Test basic command splitting for argo commands
	testCases := []struct {
		command  string
		expected []string
	}{
		{
			command:  "argocd app list",
			expected: []string{"argocd", "app", "list"},
		},
		{
			command:  "argocd app get my-app",
			expected: []string{"argocd", "app", "get", "my-app"},
		},
		{
			command:  "argocd app sync my-app --dry-run",
			expected: []string{"argocd", "app", "sync", "my-app", "--dry-run"},
		},
	}

	for _, tc := range testCases {
		parts := strings.Fields(tc.command)
		if len(parts) != len(tc.expected) {
			t.Errorf("Command '%s': expected %d parts, got %d", tc.command, len(tc.expected), len(parts))
			continue
		}

		for i, part := range parts {
			if part != tc.expected[i] {
				t.Errorf("Command '%s': expected part %d to be '%s', got '%s'", tc.command, i, tc.expected[i], part)
			}
		}
	}
}

func TestArgoAppListArgs(t *testing.T) {
	// Test argo app list argument construction
	testCases := []struct {
		project        string
		selector       string
		output         string
		expectedLength int
	}{
		{
			expectedLength: 2, // ["app", "list"]
		},
		{
			project:        "default",
			expectedLength: 4, // ["app", "list", "-p", "default"]
		},
		{
			selector:       "app=my-app",
			expectedLength: 4, // ["app", "list", "-l", "app=my-app"]
		},
		{
			output:         "json",
			expectedLength: 4, // ["app", "list", "-o", "json"]
		},
		{
			project:        "my-project",
			selector:       "env=prod",
			output:         "yaml",
			expectedLength: 8, // ["app", "list", "-p", "my-project", "-l", "env=prod", "-o", "yaml"]
		},
	}

	for i, tc := range testCases {
		args := []string{"app", "list"}

		if tc.project != "" {
			args = append(args, "-p", tc.project)
		}

		if tc.selector != "" {
			args = append(args, "-l", tc.selector)
		}

		if tc.output != "" {
			args = append(args, "-o", tc.output)
		}

		if len(args) != tc.expectedLength {
			t.Errorf("Test case %d: expected %d args, got %d. Args: %v", i, tc.expectedLength, len(args), args)
		}
	}
}

func TestArgoAppGetArgs(t *testing.T) {
	// Test argo app get argument construction
	testCases := []struct {
		appName        string
		output         string
		expectedLength int
	}{
		{
			appName:        "my-app",
			expectedLength: 3, // ["app", "get", "my-app"]
		},
		{
			appName:        "my-app",
			output:         "json",
			expectedLength: 5, // ["app", "get", "my-app", "-o", "json"]
		},
	}

	for i, tc := range testCases {
		args := []string{"app", "get", tc.appName}

		if tc.output != "" {
			args = append(args, "-o", tc.output)
		}

		if len(args) != tc.expectedLength {
			t.Errorf("Test case %d: expected %d args, got %d. Args: %v", i, tc.expectedLength, len(args), args)
		}
	}
}

func TestArgoAppSyncArgs(t *testing.T) {
	// Test argo app sync argument construction
	testCases := []struct {
		appName        string
		dryRun         bool
		prune          bool
		force          bool
		expectedLength int
	}{
		{
			appName:        "my-app",
			expectedLength: 3, // ["app", "sync", "my-app"]
		},
		{
			appName:        "my-app",
			dryRun:         true,
			expectedLength: 4, // ["app", "sync", "my-app", "--dry-run"]
		},
		{
			appName:        "my-app",
			prune:          true,
			expectedLength: 4, // ["app", "sync", "my-app", "--prune"]
		},
		{
			appName:        "my-app",
			force:          true,
			expectedLength: 4, // ["app", "sync", "my-app", "--force"]
		},
		{
			appName:        "my-app",
			dryRun:         true,
			prune:          true,
			force:          true,
			expectedLength: 6, // ["app", "sync", "my-app", "--dry-run", "--prune", "--force"]
		},
	}

	for i, tc := range testCases {
		args := []string{"app", "sync", tc.appName}

		if tc.dryRun {
			args = append(args, "--dry-run")
		}

		if tc.prune {
			args = append(args, "--prune")
		}

		if tc.force {
			args = append(args, "--force")
		}

		if len(args) != tc.expectedLength {
			t.Errorf("Test case %d: expected %d args, got %d. Args: %v", i, tc.expectedLength, len(args), args)
		}
	}
}

func TestArgoAppDeleteArgs(t *testing.T) {
	// Test argo app delete argument construction
	testCases := []struct {
		appName        string
		cascade        bool
		expectedLength int
	}{
		{
			appName:        "my-app",
			expectedLength: 3, // ["app", "delete", "my-app"]
		},
		{
			appName:        "my-app",
			cascade:        true,
			expectedLength: 4, // ["app", "delete", "my-app", "--cascade"]
		},
	}

	for i, tc := range testCases {
		args := []string{"app", "delete", tc.appName}

		if tc.cascade {
			args = append(args, "--cascade")
		}

		if len(args) != tc.expectedLength {
			t.Errorf("Test case %d: expected %d args, got %d. Args: %v", i, tc.expectedLength, len(args), args)
		}
	}
}

func TestArgoRolloutArgs(t *testing.T) {
	// Test argo rollout argument construction
	testCases := []struct {
		action         string
		rolloutName    string
		namespace      string
		expectedLength int
	}{
		{
			action:         "list",
			expectedLength: 2, // ["rollout", "list"]
		},
		{
			action:         "get",
			rolloutName:    "my-rollout",
			expectedLength: 3, // ["rollout", "get", "my-rollout"]
		},
		{
			action:         "status",
			rolloutName:    "my-rollout",
			namespace:      "default",
			expectedLength: 5, // ["rollout", "status", "my-rollout", "-n", "default"]
		},
	}

	for i, tc := range testCases {
		args := []string{"rollout", tc.action}

		if tc.rolloutName != "" {
			args = append(args, tc.rolloutName)
		}

		if tc.namespace != "" {
			args = append(args, "-n", tc.namespace)
		}

		if len(args) != tc.expectedLength {
			t.Errorf("Test case %d: expected %d args, got %d. Args: %v", i, tc.expectedLength, len(args), args)
		}
	}
}
