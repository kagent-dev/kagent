package main

import (
	"os"
	"testing"

	"github.com/caarlos0/env/v11"
	"github.com/stretchr/testify/assert"
	ctrl "sigs.k8s.io/controller-runtime"
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
			input:    []string{" default ", "  ", " test-ns  "},
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

func TestConfigureNamespaceWatching(t *testing.T) {
	tests := []struct {
		name          string
		nsConfig      ControllerConfig
		expectWatched []string
		expectOptions bool
	}{
		{
			name: "empty watchNamespaces should watch all",
			nsConfig: ControllerConfig{
				WatchNamespaces: "",
			},
			expectWatched: nil,
			expectOptions: false,
		},
		{
			name: "valid namespaces should be watched",
			nsConfig: ControllerConfig{
				WatchNamespaces: "default,kube-system",
			},
			expectWatched: []string{"default", "kube-system"},
			expectOptions: true,
		},
		{
			name: "invalid namespaces should be filtered out",
			nsConfig: ControllerConfig{
				WatchNamespaces: "default,invalid_name,kube-system",
			},
			expectWatched: []string{"default", "kube-system"},
			expectOptions: true,
		},
		{
			name: "only invalid namespaces should result in watching all",
			nsConfig: ControllerConfig{
				WatchNamespaces: "invalid_name,another-invalid!",
			},
			expectWatched: nil,
			expectOptions: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := ctrl.Options{}
			watched := ConfigureNamespaceWatching(&opts, tt.nsConfig)

			assert.Equal(t, tt.expectWatched, watched)

			if tt.expectOptions {
				assert.NotNil(t, opts.Cache.DefaultNamespaces)
				for _, ns := range tt.expectWatched {
					_, exists := opts.Cache.DefaultNamespaces[ns]
					assert.True(t, exists, "Expected namespace %s to be in DefaultNamespaces", ns)
				}
			} else {
				if opts.Cache.DefaultNamespaces != nil {
					assert.Empty(t, opts.Cache.DefaultNamespaces)
				}
			}
		})
	}
}

func TestConfigureNamespaceFiltering(t *testing.T) {
	tests := []struct {
		name               string
		nsConfig           ControllerConfig
		expectAllowed      []string
		expectPredicateNil bool
	}{
		{
			name: "empty allowedNamespaces should allow all",
			nsConfig: ControllerConfig{
				AllowedNamespaces: "",
			},
			expectAllowed:      nil,
			expectPredicateNil: false,
		},
		{
			name: "valid namespaces should be allowed",
			nsConfig: ControllerConfig{
				AllowedNamespaces: "default,kube-system",
			},
			expectAllowed:      []string{"default", "kube-system"},
			expectPredicateNil: false,
		},
		{
			name: "invalid namespaces should be filtered out",
			nsConfig: ControllerConfig{
				AllowedNamespaces: "default,invalid_name,kube-system",
			},
			expectAllowed:      []string{"default", "kube-system"},
			expectPredicateNil: false,
		},
		{
			name: "only invalid namespaces should result in allowing all",
			nsConfig: ControllerConfig{
				AllowedNamespaces: "invalid_name,another-invalid!",
			},
			expectAllowed:      nil,
			expectPredicateNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pred, allowed := ConfigureNamespaceFiltering(tt.nsConfig)

			assert.Equal(t, tt.expectAllowed, allowed)
			assert.NotNil(t, pred, "Predicate should never be nil")
		})
	}
}

func TestControllerConfigFromEnv(t *testing.T) {
	tests := []struct {
		name          string
		envVars       map[string]string
		expectWatchNs string
		expectAllowNs string
	}{
		{
			name:          "no env vars set should use defaults",
			envVars:       map[string]string{},
			expectWatchNs: "",
			expectAllowNs: "",
		},
		{
			name: "watch namespaces only",
			envVars: map[string]string{
				"WATCH_NAMESPACES": "ns1,ns2",
			},
			expectWatchNs: "ns1,ns2",
			expectAllowNs: "",
		},
		{
			name: "allowed namespaces only",
			envVars: map[string]string{
				"ALLOWED_NAMESPACES": "ns1,ns2",
			},
			expectWatchNs: "",
			expectAllowNs: "ns1,ns2",
		},
		{
			name: "both watch and allowed namespaces",
			envVars: map[string]string{
				"WATCH_NAMESPACES":   "ns1,ns2",
				"ALLOWED_NAMESPACES": "ns2,ns3",
			},
			expectWatchNs: "ns1,ns2",
			expectAllowNs: "ns2,ns3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Unsetenv("WATCH_NAMESPACES")
			os.Unsetenv("ALLOWED_NAMESPACES")

			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			config := ControllerConfig{}
			err := env.Parse(&config)

			assert.NoError(t, err)
			assert.Equal(t, tt.expectWatchNs, config.WatchNamespaces)
			assert.Equal(t, tt.expectAllowNs, config.AllowedNamespaces)
		})
	}
}

func TestCheckNamespaceFilterConsistency(t *testing.T) {
	tests := []struct {
		name             string
		watchedNs        []string
		allowedNs        []string
		expectConsistent bool
	}{
		{
			name:             "empty allowed should always be consistent",
			watchedNs:        []string{"ns1", "ns2"},
			allowedNs:        nil,
			expectConsistent: true,
		},
		{
			name:             "empty watched (all namespaces) should be consistent",
			watchedNs:        nil,
			allowedNs:        []string{"ns1", "ns2"},
			expectConsistent: true,
		},
		{
			name:             "both empty should be consistent",
			watchedNs:        nil,
			allowedNs:        nil,
			expectConsistent: true,
		},
		{
			name:             "empty slice vs nil slice for allowed should be consistent",
			watchedNs:        []string{"ns1", "ns2"},
			allowedNs:        []string{},
			expectConsistent: true,
		},
		{
			name:             "empty slice vs nil slice for watched should be consistent",
			watchedNs:        []string{},
			allowedNs:        []string{"ns1", "ns2"},
			expectConsistent: true,
		},
		{
			name:             "identical lists should be consistent",
			watchedNs:        []string{"ns1", "ns2", "ns3"},
			allowedNs:        []string{"ns1", "ns2", "ns3"},
			expectConsistent: true,
		},
		{
			name:             "watched is superset of allowed should be consistent",
			watchedNs:        []string{"ns1", "ns2", "ns3", "ns4"},
			allowedNs:        []string{"ns1", "ns3"},
			expectConsistent: true,
		},
		{
			name:             "allowed is subset of watched should be consistent",
			watchedNs:        []string{"ns1", "ns2", "ns3"},
			allowedNs:        []string{"ns1", "ns2"},
			expectConsistent: true,
		},
		{
			name:             "partially overlapping lists should be inconsistent",
			watchedNs:        []string{"ns1", "ns2", "ns3"},
			allowedNs:        []string{"ns1", "ns3", "ns4"},
			expectConsistent: false,
		},
		{
			name:             "completely disjoint lists should be inconsistent",
			watchedNs:        []string{"ns1", "ns2", "ns3"},
			allowedNs:        []string{"ns4", "ns5", "ns6"},
			expectConsistent: false,
		},
		{
			name:             "single unwatched allowed namespace should be inconsistent",
			watchedNs:        []string{"ns1", "ns2"},
			allowedNs:        []string{"ns1", "ns3"},
			expectConsistent: false,
		},
		{
			name:             "case sensitivity should be preserved",
			watchedNs:        []string{"ns1", "NS2", "ns3"},
			allowedNs:        []string{"ns1", "ns2", "ns3"},
			expectConsistent: false, // "NS2" != "ns2"
		},
		{
			name:             "duplicate entries should not affect result",
			watchedNs:        []string{"ns1", "ns2", "ns2", "ns3"},
			allowedNs:        []string{"ns1", "ns1", "ns3"},
			expectConsistent: true,
		},
		{
			name:             "single-element lists with same value should be consistent",
			watchedNs:        []string{"ns1"},
			allowedNs:        []string{"ns1"},
			expectConsistent: true,
		},
		{
			name:             "single-element lists with different values should be inconsistent",
			watchedNs:        []string{"ns1"},
			allowedNs:        []string{"ns2"},
			expectConsistent: false,
		},
		{
			name:             "whitespace differences should be treated as different namespaces",
			watchedNs:        []string{"ns1", "ns2"},
			allowedNs:        []string{"ns1", " ns2"},
			expectConsistent: false, // "ns2" != " ns2"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			consistent := CheckNamespaceFilterConsistency(tt.watchedNs, tt.allowedNs)
			assert.Equal(t, tt.expectConsistent, consistent,
				"Expected consistency=%v for watched=%v, allowed=%v",
				tt.expectConsistent, tt.watchedNs, tt.allowedNs)
		})
	}
}
