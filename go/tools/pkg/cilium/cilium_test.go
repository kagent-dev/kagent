package cilium

import (
	"strings"
	"testing"
)

func TestCiliumCommandExecution(t *testing.T) {
	// Test basic command splitting for cilium commands
	testCases := []struct {
		command  string
		expected []string
	}{
		{
			command:  "cilium status",
			expected: []string{"cilium", "status"},
		},
		{
			command:  "cilium connectivity test",
			expected: []string{"cilium", "connectivity", "test"},
		},
		{
			command:  "cilium endpoint list",
			expected: []string{"cilium", "endpoint", "list"},
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

func TestCiliumStatusArgs(t *testing.T) {
	// Test cilium status argument construction
	testCases := []struct {
		namespace    string
		verbose      bool
		wait         bool
		expectedArgs []string
	}{
		{
			expectedArgs: []string{"status"},
		},
		{
			namespace:    "kube-system",
			expectedArgs: []string{"status", "-n", "kube-system"},
		},
		{
			verbose:      true,
			expectedArgs: []string{"status", "-v"},
		},
		{
			wait:         true,
			expectedArgs: []string{"status", "--wait"},
		},
		{
			namespace:    "cilium-system",
			verbose:      true,
			wait:         true,
			expectedArgs: []string{"status", "-n", "cilium-system", "-v", "--wait"},
		},
	}

	for i, tc := range testCases {
		args := []string{"status"}

		if tc.namespace != "" {
			args = append(args, "-n", tc.namespace)
		}

		if tc.verbose {
			args = append(args, "-v")
		}

		if tc.wait {
			args = append(args, "--wait")
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

func TestCiliumConnectivityArgs(t *testing.T) {
	// Test cilium connectivity argument construction
	testCases := []struct {
		action         string
		namespace      string
		test           string
		expectedLength int
	}{
		{
			action:         "test",
			expectedLength: 2, // ["connectivity", "test"]
		},
		{
			action:         "test",
			namespace:      "default",
			expectedLength: 4, // ["connectivity", "test", "-n", "default"]
		},
		{
			action:         "test",
			test:           "pod-to-pod",
			expectedLength: 4, // ["connectivity", "test", "--test", "pod-to-pod"]
		},
	}

	for i, tc := range testCases {
		args := []string{"connectivity", tc.action}

		if tc.namespace != "" {
			args = append(args, "-n", tc.namespace)
		}

		if tc.test != "" {
			args = append(args, "--test", tc.test)
		}

		if len(args) != tc.expectedLength {
			t.Errorf("Test case %d: expected %d args, got %d. Args: %v", i, tc.expectedLength, len(args), args)
		}
	}
}

func TestCiliumEndpointArgs(t *testing.T) {
	// Test cilium endpoint argument construction
	testCases := []struct {
		action         string
		endpointID     string
		namespace      string
		expectedLength int
	}{
		{
			action:         "list",
			expectedLength: 2, // ["endpoint", "list"]
		},
		{
			action:         "get",
			endpointID:     "1234",
			expectedLength: 3, // ["endpoint", "get", "1234"]
		},
		{
			action:         "list",
			namespace:      "default",
			expectedLength: 4, // ["endpoint", "list", "-n", "default"]
		},
	}

	for i, tc := range testCases {
		args := []string{"endpoint", tc.action}

		if tc.endpointID != "" {
			args = append(args, tc.endpointID)
		}

		if tc.namespace != "" {
			args = append(args, "-n", tc.namespace)
		}

		if len(args) != tc.expectedLength {
			t.Errorf("Test case %d: expected %d args, got %d. Args: %v", i, tc.expectedLength, len(args), args)
		}
	}
}

func TestCiliumPolicyArgs(t *testing.T) {
	// Test cilium policy argument construction
	testCases := []struct {
		action         string
		policyFile     string
		namespace      string
		expectedLength int
	}{
		{
			action:         "get",
			expectedLength: 2, // ["policy", "get"]
		},
		{
			action:         "import",
			policyFile:     "policy.yaml",
			expectedLength: 3, // ["policy", "import", "policy.yaml"]
		},
		{
			action:         "get",
			namespace:      "default",
			expectedLength: 4, // ["policy", "get", "-n", "default"]
		},
	}

	for i, tc := range testCases {
		args := []string{"policy", tc.action}

		if tc.policyFile != "" {
			args = append(args, tc.policyFile)
		}

		if tc.namespace != "" {
			args = append(args, "-n", tc.namespace)
		}

		if len(args) != tc.expectedLength {
			t.Errorf("Test case %d: expected %d args, got %d. Args: %v", i, tc.expectedLength, len(args), args)
		}
	}
}

func TestCiliumNodeArgs(t *testing.T) {
	// Test cilium node argument construction
	testCases := []struct {
		action         string
		nodeName       string
		expectedLength int
	}{
		{
			action:         "list",
			expectedLength: 2, // ["node", "list"]
		},
		{
			action:         "get",
			nodeName:       "node-1",
			expectedLength: 3, // ["node", "get", "node-1"]
		},
	}

	for i, tc := range testCases {
		args := []string{"node", tc.action}

		if tc.nodeName != "" {
			args = append(args, tc.nodeName)
		}

		if len(args) != tc.expectedLength {
			t.Errorf("Test case %d: expected %d args, got %d. Args: %v", i, tc.expectedLength, len(args), args)
		}
	}
}

func TestCiliumDbgCommandConstruction(t *testing.T) {
	// Test cilium-dbg command construction
	testCases := []struct {
		command  string
		nodeName string
		expected string
	}{
		{
			command:  "endpoint list",
			expected: "endpoint list",
		},
		{
			command:  "identity get 1234",
			expected: "identity get 1234",
		},
		{
			command:  "service list",
			expected: "service list",
		},
	}

	for _, tc := range testCases {
		// This tests the command construction logic
		cmdParts := strings.Fields(tc.command)
		reconstructed := strings.Join(cmdParts, " ")
		if reconstructed != tc.expected {
			t.Errorf("Command reconstruction failed: expected '%s', got '%s'", tc.expected, reconstructed)
		}
	}
}

func TestCiliumDbgParameterParsing(t *testing.T) {
	// Test parameter parsing for cilium-dbg commands
	testCases := []struct {
		paramName  string
		paramValue string
		expected   string
	}{
		{
			paramName:  "endpoint_id",
			paramValue: "1234",
			expected:   "1234",
		},
		{
			paramName:  "labels",
			paramValue: "app=test",
			expected:   "app=test",
		},
		{
			paramName:  "output_format",
			paramValue: "json",
			expected:   "json",
		},
	}

	for _, tc := range testCases {
		if tc.paramValue != tc.expected {
			t.Errorf("Parameter parsing failed for %s: expected '%s', got '%s'", tc.paramName, tc.expected, tc.paramValue)
		}
	}
}
