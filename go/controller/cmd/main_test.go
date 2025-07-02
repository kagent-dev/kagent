package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilterValidNamespaces(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "valid namespaces should pass through",
			input:    []string{"default", "kube-system", "test-ns"},
			expected: []string{"default", "kube-system", "test-ns"},
		},
		{
			name:     "empty strings should be filtered out",
			input:    []string{"default", "", "test-ns", ""},
			expected: []string{"default", "test-ns"},
		},
		{
			name:     "whitespace should be trimmed",
			input:    []string{" whitespaces-invalid-1 ", "  ", " whitespaces-invalid-2  "},
			expected: nil,
		},
		{
			name:     "invalid namespace names should be filtered out",
			input:    []string{"default", "invalid_namespace", "test-ns", "namespace-with-too-long-name-that-exceeds-kubernetes-limit-123456789012345678901234567890123456789012345678901234567890"},
			expected: []string{"default", "test-ns"},
		},
		{
			name:     "mixed valid and invalid names",
			input:    []string{"default", "", "Test-ns", "valid-ns", "ns.with.dots"},
			expected: []string{"default", "valid-ns"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterValidNamespaces(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfigureNamespaceWatching(t *testing.T) {
	tests := []struct {
		name                  string
		watchNamespace        string
		expectedWatchAll      bool
		expectedNamespaceKeys []string
	}{
		{
			name:                  "empty watchNamespaces should watch all",
			watchNamespace:        "",
			expectedWatchAll:      true,
			expectedNamespaceKeys: []string{""},
		},
		{
			name:                  "valid namespaces should be watched",
			watchNamespace:        "default,kube-system",
			expectedWatchAll:      false,
			expectedNamespaceKeys: []string{"default", "kube-system"},
		},
		{
			name:                  "invalid namespaces should be filtered out",
			watchNamespace:        "default,invalid_name,kube-system",
			expectedWatchAll:      false,
			expectedNamespaceKeys: []string{"default", "kube-system"},
		},
		{
			name:                  "only invalid namespaces should result in watching all",
			watchNamespace:        "invalid_name,another-invalid!",
			expectedWatchAll:      true,
			expectedNamespaceKeys: []string{""},
		},
		{
			name:                  "whitespace should not be trimmed automatically",
			watchNamespace:        " default , kube-system ",
			expectedWatchAll:      true,
			expectedNamespaceKeys: []string{""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			watchNamespaces := strings.Split(strings.TrimSpace(tt.watchNamespace), ",")
			if tt.watchNamespace == "" {
				watchNamespaces = []string{}
			}
			filteredNamespaces := filterValidNamespaces(watchNamespaces)

			result := configureNamespaceWatching(filteredNamespaces)

			// For the "watch all" case
			if tt.expectedWatchAll {
				assert.Contains(t, result, "", "Should contain empty string key for watching all namespaces")
				assert.Len(t, result, 1, "Should only have one entry for watching all namespaces")
				return
			}

			// For specific namespaces, verify we have exactly the expected namespaces
			assert.Len(t, result, len(tt.expectedNamespaceKeys), "Should have the expected number of namespaces")
			for _, ns := range tt.expectedNamespaceKeys {
				assert.Contains(t, result, ns, "Expected namespace %s to be in result", ns)
			}
		})
	}
}
