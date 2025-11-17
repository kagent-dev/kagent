package agent

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test_addTLSConfiguration_NoTLSConfig verifies that no volumes are added when TLS config is nil
func Test_addTLSConfiguration_NoTLSConfig(t *testing.T) {
	mdd := &modelDeploymentData{}

	addTLSConfiguration(mdd, nil)

	assert.Empty(t, mdd.Volumes, "Expected no volumes when TLS config is nil")
	assert.Empty(t, mdd.VolumeMounts, "Expected no volume mounts when TLS config is nil")
}

// Test_addTLSConfiguration_WithDisableVerify verifies no volumes are added when TLS verify is disabled without cert
func Test_addTLSConfiguration_WithDisableVerify(t *testing.T) {
	mdd := &modelDeploymentData{}
	tlsConfig := &v1alpha2.TLSConfig{
		DisableVerify:    true,
		DisableSystemCAs: true,
	}

	addTLSConfiguration(mdd, tlsConfig)

	// Should not add volumes/mounts when no CACertSecretRef is set
	assert.Empty(t, mdd.Volumes, "Expected no volumes when CACertSecretRef is empty")
	assert.Empty(t, mdd.VolumeMounts, "Expected no volume mounts when CACertSecretRef is empty")
}

// Test_addTLSConfiguration_WithCACertSecret verifies Secret volume mounting
func Test_addTLSConfiguration_WithCACertSecret(t *testing.T) {
	mdd := &modelDeploymentData{}
	tlsConfig := &v1alpha2.TLSConfig{
		DisableVerify:    false,
		CACertSecretRef:  "internal-ca-cert",
		CACertSecretKey:  "ca.crt",
		DisableSystemCAs: false,
	}

	addTLSConfiguration(mdd, tlsConfig)

	// Verify volume is added
	require.Len(t, mdd.Volumes, 1, "Expected 1 volume for TLS cert secret")

	volume := mdd.Volumes[0]
	assert.Equal(t, tlsCACertVolumeName, volume.Name, "Volume name should match TLS CA cert volume name")
	require.NotNil(t, volume.Secret, "Expected Secret volume source")
	assert.Equal(t, "internal-ca-cert", volume.Secret.SecretName, "Secret name should match CACertSecretRef")
	assert.Equal(t, int32(0444), *volume.Secret.DefaultMode, "DefaultMode should be 0444 for read-only cert")

	// Verify volume mount is added
	require.Len(t, mdd.VolumeMounts, 1, "Expected 1 volume mount for TLS cert")

	mount := mdd.VolumeMounts[0]
	assert.Equal(t, tlsCACertVolumeName, mount.Name, "Volume mount name should match volume name")
	assert.Equal(t, tlsCACertMountPath, mount.MountPath, "Mount path should be TLS cert mount path")
	assert.True(t, mount.ReadOnly, "Volume mount should be read-only")
}

// Test_addTLSConfiguration_MissingCACertKey verifies no volumes are mounted when CACertSecretKey is not set
func Test_addTLSConfiguration_MissingCACertKey(t *testing.T) {
	mdd := &modelDeploymentData{}
	tlsConfig := &v1alpha2.TLSConfig{
		CACertSecretRef: "internal-ca-cert",
		// CACertSecretKey not set - both fields are required
	}

	addTLSConfiguration(mdd, tlsConfig)

	// Should not add volumes when CACertSecretKey is not provided
	assert.Empty(t, mdd.Volumes, "Expected no volumes when CACertSecretKey is empty")
	assert.Empty(t, mdd.VolumeMounts, "Expected no volume mounts when CACertSecretKey is empty")
}

// Test_addTLSConfiguration_CustomCertKey verifies volume mounting works with custom key
func Test_addTLSConfiguration_CustomCertKey(t *testing.T) {
	mdd := &modelDeploymentData{}
	tlsConfig := &v1alpha2.TLSConfig{
		CACertSecretRef: "internal-ca-cert",
		CACertSecretKey: "custom-ca.pem",
	}

	addTLSConfiguration(mdd, tlsConfig)

	// Verify volume is added
	require.Len(t, mdd.Volumes, 1, "Expected 1 volume for TLS cert with custom key")

	// Verify volume mount is added at the correct path
	require.Len(t, mdd.VolumeMounts, 1, "Expected 1 volume mount for TLS cert")

	mount := mdd.VolumeMounts[0]
	assert.Equal(t, tlsCACertMountPath, mount.MountPath, "Mount path should be TLS cert mount path")
}

// Test_addTLSConfiguration_DisableSystemCAsFlag verifies no volumes added when no cert secret
func Test_addTLSConfiguration_DisableSystemCAsFlag(t *testing.T) {
	tests := []struct {
		name             string
		disableSystemCAs bool
	}{
		{
			name:             "DisableSystemCAs false (use system CAs)",
			disableSystemCAs: false,
		},
		{
			name:             "DisableSystemCAs true (don't use system CAs)",
			disableSystemCAs: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mdd := &modelDeploymentData{}
			tlsConfig := &v1alpha2.TLSConfig{
				DisableSystemCAs: tt.disableSystemCAs,
			}

			addTLSConfiguration(mdd, tlsConfig)

			// Should not add volumes when no CACertSecretRef is set
			assert.Empty(t, mdd.Volumes, "Expected no volumes when CACertSecretRef is empty")
			assert.Empty(t, mdd.VolumeMounts, "Expected no volume mounts when CACertSecretRef is empty")
		})
	}
}

// Test_addTLSConfiguration_AllFieldsCombined verifies volume mounting works with all fields set
func Test_addTLSConfiguration_AllFieldsCombined(t *testing.T) {
	mdd := &modelDeploymentData{}
	tlsConfig := &v1alpha2.TLSConfig{
		DisableVerify:    false,
		CACertSecretRef:  "my-ca-bundle",
		CACertSecretKey:  "bundle.crt",
		DisableSystemCAs: false,
	}

	addTLSConfiguration(mdd, tlsConfig)

	// Verify volume and mount
	require.Len(t, mdd.Volumes, 1, "Expected 1 volume for combined TLS config")
	require.Len(t, mdd.VolumeMounts, 1, "Expected 1 volume mount for combined TLS config")

	// Verify volume references correct Secret
	volume := mdd.Volumes[0]
	require.NotNil(t, volume.Secret, "Expected Secret volume source")
	assert.Equal(t, "my-ca-bundle", volume.Secret.SecretName, "Secret name should match CACertSecretRef")

	// Verify mount path is correct
	mount := mdd.VolumeMounts[0]
	assert.Equal(t, tlsCACertMountPath, mount.MountPath, "Mount path should be TLS cert mount path")
}
