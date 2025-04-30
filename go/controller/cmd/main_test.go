package main

import (
	"os"
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

func TestGetAllowedNamespacesFromEnv(t *testing.T) {
	tests := []struct {
		name        string
		envValue    string
		envSet      bool
		expectError bool
		expected    string
	}{
		{
			name:        "environment variable not set should return error",
			envSet:      false,
			expectError: true,
		},
		{
			name:        "single namespace should be returned as is",
			envValue:    "default",
			envSet:      true,
			expectError: false,
			expected:    "default",
		},
		{
			name:        "multiple namespaces should be returned as is",
			envValue:    "default,kube-system,app-ns",
			envSet:      true,
			expectError: false,
			expected:    "default,kube-system,app-ns",
		},
		{
			name:        "empty string is valid but will be validated later",
			envValue:    "",
			envSet:      true,
			expectError: false,
			expected:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean env before test
			os.Unsetenv("ALLOWED_NAMESPACES")

			// Setup env for test
			if tt.envSet {
				os.Setenv("ALLOWED_NAMESPACES", tt.envValue)
			}

			result, err := getAllowedNamespacesFromEnv()

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}
