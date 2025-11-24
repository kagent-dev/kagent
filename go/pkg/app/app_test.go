package app

import (
	"flag"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/resource"
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

func TestLoadFromEnv(t *testing.T) {
	tests := []struct {
		name        string
		envVars     map[string]string
		flagName    string
		flagDefault string
		wantValue   string
	}{
		{
			name: "string flag with hyphen",
			envVars: map[string]string{
				"METRICS_BIND_ADDRESS": ":9090",
			},
			flagName:    "metrics-bind-address",
			flagDefault: ":8080",
			wantValue:   ":9090",
		},
		{
			name: "flag without env var uses default",
			envVars: map[string]string{
				"OTHER_FLAG": "value",
			},
			flagName:    "test-flag",
			flagDefault: "default",
			wantValue:   "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			// Create a new flag set for testing
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			var testVar string
			fs.StringVar(&testVar, tt.flagName, tt.flagDefault, "test flag")

			// Load from environment
			if err := LoadFromEnv(fs); err != nil {
				t.Fatalf("LoadFromEnv() error = %v", err)
			}

			// Check the value
			if testVar != tt.wantValue {
				t.Errorf("flag value = %v, want %v", testVar, tt.wantValue)
			}
		})
	}
}

func TestLoadFromEnvBoolFlags(t *testing.T) {
	tests := []struct {
		name      string
		envValue  string
		wantValue bool
		wantErr   bool
	}{
		{
			name:      "true value",
			envValue:  "true",
			wantValue: true,
			wantErr:   false,
		},
		{
			name:      "false value",
			envValue:  "false",
			wantValue: false,
			wantErr:   false,
		},
		{
			name:      "1 value",
			envValue:  "1",
			wantValue: true,
			wantErr:   false,
		},
		{
			name:      "0 value",
			envValue:  "0",
			wantValue: false,
			wantErr:   false,
		},
		{
			name:      "invalid value",
			envValue:  "invalid",
			wantValue: false,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envName := "TEST_BOOL"
			t.Setenv(envName, tt.envValue)

			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			var testVar bool
			fs.BoolVar(&testVar, "test-bool", false, "test bool flag")

			err := LoadFromEnv(fs)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadFromEnv() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && testVar != tt.wantValue {
				t.Errorf("flag value = %v, want %v", testVar, tt.wantValue)
			}
		})
	}
}

func TestLoadFromEnvDurationFlags(t *testing.T) {
	envName := "TEST_DURATION"
	t.Setenv(envName, "5m")

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var testVar time.Duration
	fs.DurationVar(&testVar, "test-duration", 1*time.Second, "test duration flag")

	if err := LoadFromEnv(fs); err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}

	wantValue := 5 * time.Minute
	if testVar != wantValue {
		t.Errorf("flag value = %v, want %v", testVar, wantValue)
	}
}

func TestLoadFromEnvIntegration(t *testing.T) {
	envVars := map[string]string{
		"METRICS_BIND_ADDRESS":           ":9090",
		"HEALTH_PROBE_BIND_ADDRESS":      ":8081",
		"LEADER_ELECT":                   "true",
		"METRICS_SECURE":                 "false",
		"ENABLE_HTTP2":                   "true",
		"DEFAULT_MODEL_CONFIG_NAME":      "custom-model",
		"DEFAULT_MODEL_CONFIG_NAMESPACE": "custom-ns",
		"HTTP_SERVER_ADDRESS":            ":9000",
		"A2A_BASE_URL":                   "http://example.com:9000",
		"DATABASE_TYPE":                  "postgres",
		"POSTGRES_DATABASE_URL":          "postgres://localhost:5432/testdb",
		"WATCH_NAMESPACES":               "ns1,ns2,ns3",
		"STREAMING_TIMEOUT":              "120s",
		"STREAMING_MAX_BUF_SIZE":         "2Mi",
		"STREAMING_INITIAL_BUF_SIZE":     "8Ki",
	}

	for k, v := range envVars {
		t.Setenv(k, v)
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg := Config{}
	cfg.SetFlags(fs) // Sets flags and defaults

	if err := LoadFromEnv(fs); err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}

	// Verify values - env vars should override default flags
	if cfg.Metrics.Addr != ":9090" {
		t.Errorf("Metrics.Addr = %v, want :9090", cfg.Metrics.Addr)
	}
	if cfg.ProbeAddr != ":8081" {
		t.Errorf("ProbeAddr = %v, want :8081", cfg.ProbeAddr)
	}
	if !cfg.LeaderElection {
		t.Errorf("LeaderElection = false, want true")
	}
	if cfg.SecureMetrics {
		t.Errorf("SecureMetrics = true, want false")
	}
	if !cfg.EnableHTTP2 {
		t.Errorf("EnableHTTP2 = false, want true")
	}
	if cfg.DefaultModelConfig.Name != "custom-model" {
		t.Errorf("DefaultModelConfig.Name = %v, want custom-model", cfg.DefaultModelConfig.Name)
	}
	if cfg.DefaultModelConfig.Namespace != "custom-ns" {
		t.Errorf("DefaultModelConfig.Namespace = %v, want custom-ns", cfg.DefaultModelConfig.Namespace)
	}
	if cfg.HttpServerAddr != ":9000" {
		t.Errorf("HttpServerAddr = %v, want :9000", cfg.HttpServerAddr)
	}
	if cfg.A2ABaseUrl != "http://example.com:9000" {
		t.Errorf("A2ABaseUrl = %v, want http://example.com:9000", cfg.A2ABaseUrl)
	}
	if cfg.Database.Type != "postgres" {
		t.Errorf("Database.Type = %v, want postgres", cfg.Database.Type)
	}
	if cfg.Database.Url != "postgres://localhost:5432/testdb" {
		t.Errorf("Database.Url = %v, want postgres://localhost:5432/testdb", cfg.Database.Url)
	}
	if cfg.WatchNamespaces != "ns1,ns2,ns3" {
		t.Errorf("WatchNamespaces = %v, want ns1,ns2,ns3", cfg.WatchNamespaces)
	}
	if cfg.Streaming.Timeout != 120*time.Second {
		t.Errorf("Streaming.Timeout = %v, want 120s", cfg.Streaming.Timeout)
	}

	// Check quantity values
	expectedMaxBuf := resource.MustParse("2Mi")
	if cfg.Streaming.MaxBufSize.Cmp(expectedMaxBuf) != 0 {
		t.Errorf("Streaming.MaxBufSize = %v, want 2Mi", cfg.Streaming.MaxBufSize)
	}

	expectedInitBuf := resource.MustParse("8Ki")
	if cfg.Streaming.InitialBufSize.Cmp(expectedInitBuf) != 0 {
		t.Errorf("Streaming.InitialBufSize = %v, want 8Ki", cfg.Streaming.InitialBufSize)
	}
}
