package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTLSConfigJSONMarshaling verifies that TLSConfig properly serializes to/from JSON
func TestTLSConfigJSONMarshaling(t *testing.T) {
	tests := []struct {
		name     string
		config   *TLSConfig
		expected string
	}{
		{
			name: "all fields set",
			config: &TLSConfig{
				VerifyDisabled:  true,
				CACertSecretRef: "my-ca-secret",
				CACertSecretKey: "custom-ca.crt",
				UseSystemCAs:    false,
			},
			// Note: useSystemCAs:false is omitted due to omitempty with zero value
			expected: `{"verifyDisabled":true,"caCertSecretRef":"my-ca-secret","caCertSecretKey":"custom-ca.crt"}`,
		},
		{
			name: "minimal config with defaults",
			config: &TLSConfig{
				VerifyDisabled: false,
				UseSystemCAs:   true,
			},
			expected: `{"useSystemCAs":true}`,
		},
		{
			name: "custom CA only",
			config: &TLSConfig{
				CACertSecretRef: "internal-ca",
				CACertSecretKey: "ca.crt",
				UseSystemCAs:    true,
			},
			expected: `{"caCertSecretRef":"internal-ca","caCertSecretKey":"ca.crt","useSystemCAs":true}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal
			data, err := json.Marshal(tt.config)
			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(data))

			// Unmarshal
			var unmarshaled TLSConfig
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)
			assert.Equal(t, tt.config.VerifyDisabled, unmarshaled.VerifyDisabled)
			assert.Equal(t, tt.config.CACertSecretRef, unmarshaled.CACertSecretRef)
			assert.Equal(t, tt.config.CACertSecretKey, unmarshaled.CACertSecretKey)
			assert.Equal(t, tt.config.UseSystemCAs, unmarshaled.UseSystemCAs)
		})
	}
}

// TestTLSConfigDefaults verifies default values are properly set
func TestTLSConfigDefaults(t *testing.T) {
	t.Run("empty TLSConfig has correct zero values", func(t *testing.T) {
		config := &TLSConfig{}
		assert.False(t, config.VerifyDisabled, "VerifyDisabled should default to false")
		assert.Empty(t, config.CACertSecretRef)
		assert.Empty(t, config.CACertSecretKey)
		assert.False(t, config.UseSystemCAs, "UseSystemCAs zero value is false")
	})

	t.Run("nil TLS field in ModelConfigSpec is valid", func(t *testing.T) {
		spec := ModelConfigSpec{
			Model:    "gpt-4",
			Provider: ModelProviderOpenAI,
			TLS:      nil,
		}
		assert.Nil(t, spec.TLS, "TLS field should be optional")
	})
}

// TestTLSConfigSecretReferencePatterns verifies secret reference patterns
func TestTLSConfigSecretReferencePatterns(t *testing.T) {
	t.Run("both secret ref and key set", func(t *testing.T) {
		config := &TLSConfig{
			CACertSecretRef: "my-secret",
			CACertSecretKey: "ca.crt",
		}
		assert.NotEmpty(t, config.CACertSecretRef)
		assert.NotEmpty(t, config.CACertSecretKey)
	})

	t.Run("neither secret ref nor key set", func(t *testing.T) {
		config := &TLSConfig{
			VerifyDisabled: false,
			UseSystemCAs:   true,
		}
		assert.Empty(t, config.CACertSecretRef)
		assert.Empty(t, config.CACertSecretKey)
	})

	t.Run("follows APIKeySecretRef pattern", func(t *testing.T) {
		// TLSConfig follows the same pattern as APIKeySecretRef/APIKeySecretKey
		spec := ModelConfigSpec{
			Model:           "gpt-4",
			Provider:        ModelProviderOpenAI,
			APIKeySecretRef: "openai-key",
			APIKeySecretKey: "api-key",
			TLS: &TLSConfig{
				CACertSecretRef: "ca-cert",
				CACertSecretKey: "ca.crt",
			},
		}
		assert.Equal(t, "openai-key", spec.APIKeySecretRef)
		assert.Equal(t, "api-key", spec.APIKeySecretKey)
		assert.Equal(t, "ca-cert", spec.TLS.CACertSecretRef)
		assert.Equal(t, "ca.crt", spec.TLS.CACertSecretKey)
	})
}

// TestTLSConfigBackwardCompatibility verifies existing ModelConfigs work without TLS
func TestTLSConfigBackwardCompatibility(t *testing.T) {
	t.Run("existing ModelConfig without TLS field deserializes", func(t *testing.T) {
		// Simulate existing ModelConfig JSON without TLS field
		existingJSON := `{
			"model": "gpt-4",
			"provider": "OpenAI",
			"apiKeySecretRef": "my-secret",
			"apiKeySecretKey": "key"
		}`

		var spec ModelConfigSpec
		err := json.Unmarshal([]byte(existingJSON), &spec)
		require.NoError(t, err)
		assert.Equal(t, "gpt-4", spec.Model)
		assert.Equal(t, ModelProviderOpenAI, spec.Provider)
		assert.Nil(t, spec.TLS, "TLS should be nil for backward compatibility")
	})

	t.Run("new ModelConfig with TLS serializes cleanly", func(t *testing.T) {
		spec := ModelConfigSpec{
			Model:           "gpt-4",
			Provider:        ModelProviderOpenAI,
			APIKeySecretRef: "my-secret",
			APIKeySecretKey: "key",
			TLS: &TLSConfig{
				CACertSecretRef: "ca-secret",
				CACertSecretKey: "ca.crt",
				UseSystemCAs:    true,
			},
		}

		data, err := json.Marshal(&spec)
		require.NoError(t, err)

		// Verify TLS is in JSON
		assert.Contains(t, string(data), "tls")
		assert.Contains(t, string(data), "caCertSecretRef")
	})
}

// TestTLSConfigModes verifies different TLS configuration modes
func TestTLSConfigModes(t *testing.T) {
	tests := []struct {
		name        string
		config      *TLSConfig
		description string
	}{
		{
			name: "verification disabled mode",
			config: &TLSConfig{
				VerifyDisabled: true,
			},
			description: "SSL verification completely disabled (dev/test only)",
		},
		{
			name: "custom CA only mode",
			config: &TLSConfig{
				CACertSecretRef: "custom-ca",
				CACertSecretKey: "ca.crt",
				UseSystemCAs:    false,
			},
			description: "Use only custom CA, ignore system CAs",
		},
		{
			name: "custom CA + system CAs mode",
			config: &TLSConfig{
				CACertSecretRef: "custom-ca",
				CACertSecretKey: "ca.crt",
				UseSystemCAs:    true,
			},
			description: "Use both custom and system CAs (additive)",
		},
		{
			name: "system CAs only mode",
			config: &TLSConfig{
				UseSystemCAs: true,
			},
			description: "Use only system CAs (default behavior)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify config can be marshaled and unmarshaled
			data, err := json.Marshal(tt.config)
			require.NoError(t, err)

			var unmarshaled TLSConfig
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)

			assert.Equal(t, tt.config.VerifyDisabled, unmarshaled.VerifyDisabled)
			assert.Equal(t, tt.config.CACertSecretRef, unmarshaled.CACertSecretRef)
			assert.Equal(t, tt.config.CACertSecretKey, unmarshaled.CACertSecretKey)
			assert.Equal(t, tt.config.UseSystemCAs, unmarshaled.UseSystemCAs)
		})
	}
}
