package istio

import (
	"strings"
	"testing"
)

func TestIstioCommandExecution(t *testing.T) {
	// Test basic command splitting for istio commands
	testCases := []struct {
		command  string
		expected []string
	}{
		{
			command:  "istioctl version",
			expected: []string{"istioctl", "version"},
		},
		{
			command:  "istioctl proxy-config cluster pod-name",
			expected: []string{"istioctl", "proxy-config", "cluster", "pod-name"},
		},
		{
			command:  "istioctl analyze -n default",
			expected: []string{"istioctl", "analyze", "-n", "default"},
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

func TestIstioAnalyzeArgs(t *testing.T) {
	// Test istio analyze argument construction
	testCases := []struct {
		namespace     string
		allNamespaces bool
		verbose       bool
		expectedArgs  []string
	}{
		{
			namespace:    "",
			expectedArgs: []string{"analyze"},
		},
		{
			namespace:    "default",
			expectedArgs: []string{"analyze", "-n", "default"},
		},
		{
			allNamespaces: true,
			expectedArgs:  []string{"analyze", "-A"},
		},
		{
			verbose:      true,
			expectedArgs: []string{"analyze", "-v"},
		},
		{
			namespace:    "istio-system",
			verbose:      true,
			expectedArgs: []string{"analyze", "-n", "istio-system", "-v"},
		},
	}

	for i, tc := range testCases {
		args := []string{"analyze"}

		if tc.namespace != "" {
			args = append(args, "-n", tc.namespace)
		}

		if tc.allNamespaces {
			args = append(args, "-A")
		}

		if tc.verbose {
			args = append(args, "-v")
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

func TestIstioProxyConfigArgs(t *testing.T) {
	// Test istio proxy-config argument construction
	testCases := []struct {
		configType     string
		podName        string
		namespace      string
		output         string
		expectedLength int
	}{
		{
			configType:     "cluster",
			podName:        "my-pod",
			expectedLength: 3, // ["proxy-config", "cluster", "my-pod"]
		},
		{
			configType:     "listener",
			podName:        "my-pod",
			namespace:      "default",
			expectedLength: 5, // ["proxy-config", "listener", "my-pod", "-n", "default"]
		},
		{
			configType:     "route",
			podName:        "my-pod",
			output:         "json",
			expectedLength: 5, // ["proxy-config", "route", "my-pod", "-o", "json"]
		},
	}

	for i, tc := range testCases {
		args := []string{"proxy-config", tc.configType, tc.podName}

		if tc.namespace != "" {
			args = append(args, "-n", tc.namespace)
		}

		if tc.output != "" {
			args = append(args, "-o", tc.output)
		}

		if len(args) != tc.expectedLength {
			t.Errorf("Test case %d: expected %d args, got %d. Args: %v", i, tc.expectedLength, len(args), args)
		}
	}
}

func TestIstioKialiArgs(t *testing.T) {
	// Test istio kiali argument construction
	testCases := []struct {
		action         string
		namespace      string
		expectedLength int
	}{
		{
			action:         "dashboard",
			expectedLength: 2, // ["kiali", "dashboard"]
		},
		{
			action:         "dashboard",
			namespace:      "istio-system",
			expectedLength: 4, // ["kiali", "dashboard", "-n", "istio-system"]
		},
	}

	for i, tc := range testCases {
		args := []string{"kiali", tc.action}

		if tc.namespace != "" {
			args = append(args, "-n", tc.namespace)
		}

		if len(args) != tc.expectedLength {
			t.Errorf("Test case %d: expected %d args, got %d. Args: %v", i, tc.expectedLength, len(args), args)
		}
	}
}

func TestIstioVersionArgs(t *testing.T) {
	// Test istio version argument construction
	testCases := []struct {
		remote         bool
		short          bool
		expectedLength int
	}{
		{
			expectedLength: 1, // ["version"]
		},
		{
			remote:         true,
			expectedLength: 2, // ["version", "--remote"]
		},
		{
			short:          true,
			expectedLength: 2, // ["version", "--short"]
		},
		{
			remote:         true,
			short:          true,
			expectedLength: 3, // ["version", "--remote", "--short"]
		},
	}

	for i, tc := range testCases {
		args := []string{"version"}

		if tc.remote {
			args = append(args, "--remote")
		}

		if tc.short {
			args = append(args, "--short")
		}

		if len(args) != tc.expectedLength {
			t.Errorf("Test case %d: expected %d args, got %d. Args: %v", i, tc.expectedLength, len(args), args)
		}
	}
}
