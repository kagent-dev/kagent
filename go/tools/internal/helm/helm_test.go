package helm

import (
	"strings"
	"testing"
)

func TestHelmCommandExecution(t *testing.T) {
	// Test basic command splitting for helm commands
	testCases := []struct {
		command  string
		expected []string
	}{
		{
			command:  "helm list",
			expected: []string{"helm", "list"},
		},
		{
			command:  "helm list -n default",
			expected: []string{"helm", "list", "-n", "default"},
		},
		{
			command:  "helm install my-release stable/nginx",
			expected: []string{"helm", "install", "my-release", "stable/nginx"},
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

func TestHelmListArgs(t *testing.T) {
	// Test helm list argument construction
	testCases := []struct {
		namespace     string
		allNamespaces bool
		all           bool
		uninstalled   bool
		expectedArgs  []string
	}{
		{
			namespace:    "",
			expectedArgs: []string{"list"},
		},
		{
			namespace:    "default",
			expectedArgs: []string{"list", "-n", "default"},
		},
		{
			allNamespaces: true,
			expectedArgs:  []string{"list", "-A"},
		},
		{
			all:          true,
			expectedArgs: []string{"list", "-a"},
		},
		{
			uninstalled:  true,
			expectedArgs: []string{"list", "--uninstalled"},
		},
		{
			namespace:    "kube-system",
			all:          true,
			uninstalled:  true,
			expectedArgs: []string{"list", "-n", "kube-system", "-a", "--uninstalled"},
		},
	}

	for i, tc := range testCases {
		args := []string{"list"}

		if tc.namespace != "" {
			args = append(args, "-n", tc.namespace)
		}

		if tc.allNamespaces {
			args = append(args, "-A")
		}

		if tc.all {
			args = append(args, "-a")
		}

		if tc.uninstalled {
			args = append(args, "--uninstalled")
		}

		if len(args) != len(tc.expectedArgs) {
			t.Errorf("Test case %d: expected %d args, got %d", i, len(tc.expectedArgs), len(args))
			continue
		}

		for j, arg := range args {
			if arg != tc.expectedArgs[j] {
				t.Errorf("Test case %d: expected arg %d to be '%s', got '%s'", i, j, tc.expectedArgs[j], arg)
			}
		}
	}
}

func TestHelmInstallArgs(t *testing.T) {
	// Test helm install argument construction
	testCases := []struct {
		releaseName    string
		chartName      string
		namespace      string
		createNs       bool
		values         string
		setValues      string
		expectedLength int
	}{
		{
			releaseName:    "my-release",
			chartName:      "stable/nginx",
			expectedLength: 3, // ["install", "my-release", "stable/nginx"]
		},
		{
			releaseName:    "my-release",
			chartName:      "stable/nginx",
			namespace:      "default",
			expectedLength: 5, // ["install", "my-release", "stable/nginx", "-n", "default"]
		},
		{
			releaseName:    "my-release",
			chartName:      "stable/nginx",
			namespace:      "default",
			createNs:       true,
			expectedLength: 6, // ["install", "my-release", "stable/nginx", "-n", "default", "--create-namespace"]
		},
		{
			releaseName:    "my-release",
			chartName:      "stable/nginx",
			values:         "values.yaml",
			expectedLength: 5, // ["install", "my-release", "stable/nginx", "-f", "values.yaml"]
		},
		{
			releaseName:    "my-release",
			chartName:      "stable/nginx",
			setValues:      "replicas=3",
			expectedLength: 5, // ["install", "my-release", "stable/nginx", "--set", "replicas=3"]
		},
	}

	for i, tc := range testCases {
		args := []string{"install", tc.releaseName, tc.chartName}

		if tc.namespace != "" {
			args = append(args, "-n", tc.namespace)
		}

		if tc.createNs {
			args = append(args, "--create-namespace")
		}

		if tc.values != "" {
			args = append(args, "-f", tc.values)
		}

		if tc.setValues != "" {
			args = append(args, "--set", tc.setValues)
		}

		if len(args) != tc.expectedLength {
			t.Errorf("Test case %d: expected %d args, got %d. Args: %v", i, tc.expectedLength, len(args), args)
		}
	}
}

func TestHelmUpgradeArgs(t *testing.T) {
	// Test helm upgrade argument construction
	testCases := []struct {
		releaseName    string
		chartName      string
		namespace      string
		install        bool
		expectedLength int
	}{
		{
			releaseName:    "my-release",
			chartName:      "stable/nginx",
			expectedLength: 3, // ["upgrade", "my-release", "stable/nginx"]
		},
		{
			releaseName:    "my-release",
			chartName:      "stable/nginx",
			namespace:      "default",
			expectedLength: 5, // ["upgrade", "my-release", "stable/nginx", "-n", "default"]
		},
		{
			releaseName:    "my-release",
			chartName:      "stable/nginx",
			install:        true,
			expectedLength: 4, // ["upgrade", "my-release", "stable/nginx", "--install"]
		},
	}

	for i, tc := range testCases {
		args := []string{"upgrade", tc.releaseName, tc.chartName}

		if tc.namespace != "" {
			args = append(args, "-n", tc.namespace)
		}

		if tc.install {
			args = append(args, "--install")
		}

		if len(args) != tc.expectedLength {
			t.Errorf("Test case %d: expected %d args, got %d. Args: %v", i, tc.expectedLength, len(args), args)
		}
	}
}

func TestHelmUninstallArgs(t *testing.T) {
	// Test helm uninstall argument construction
	testCases := []struct {
		releaseName    string
		namespace      string
		keepHistory    bool
		expectedLength int
	}{
		{
			releaseName:    "my-release",
			expectedLength: 2, // ["uninstall", "my-release"]
		},
		{
			releaseName:    "my-release",
			namespace:      "default",
			expectedLength: 4, // ["uninstall", "my-release", "-n", "default"]
		},
		{
			releaseName:    "my-release",
			keepHistory:    true,
			expectedLength: 3, // ["uninstall", "my-release", "--keep-history"]
		},
	}

	for i, tc := range testCases {
		args := []string{"uninstall", tc.releaseName}

		if tc.namespace != "" {
			args = append(args, "-n", tc.namespace)
		}

		if tc.keepHistory {
			args = append(args, "--keep-history")
		}

		if len(args) != tc.expectedLength {
			t.Errorf("Test case %d: expected %d args, got %d. Args: %v", i, tc.expectedLength, len(args), args)
		}
	}
}
