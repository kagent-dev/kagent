package agent

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

// Test_addTLSConfiguration_NoTLSConfig verifies that no volumes are added when TLS config is nil
func Test_addTLSConfiguration_NoTLSConfig(t *testing.T) {
	mdd := &modelDeploymentData{}

	addTLSConfiguration(mdd, nil)

	// Note: TLS configuration is now passed via agent config JSON, not environment variables
	if len(mdd.Volumes) != 0 {
		t.Errorf("Expected no volumes, got %d", len(mdd.Volumes))
	}
	if len(mdd.VolumeMounts) != 0 {
		t.Errorf("Expected no volume mounts, got %d", len(mdd.VolumeMounts))
	}
}

// Test_addTLSConfiguration_WithDisableVerify verifies no volumes are added when TLS verify is disabled without cert
func Test_addTLSConfiguration_WithDisableVerify(t *testing.T) {
	mdd := &modelDeploymentData{}
	tlsConfig := &v1alpha2.TLSConfig{
		DisableVerify:    true,
		DisableSystemCAs: true,
	}

	addTLSConfiguration(mdd, tlsConfig)

	// Note: TLS configuration (DisableVerify, DisableSystemCAs) is now passed via agent config JSON
	// addTLSConfiguration only handles volume mounting for certificates

	// Should not add volumes/mounts when no CACertSecretRef is set
	if len(mdd.Volumes) != 0 {
		t.Errorf("Expected no volumes when CACertSecretRef is empty, got %d", len(mdd.Volumes))
	}
	if len(mdd.VolumeMounts) != 0 {
		t.Errorf("Expected no volume mounts when CACertSecretRef is empty, got %d", len(mdd.VolumeMounts))
	}
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

	// Note: TLS configuration fields are now passed via agent config JSON, not environment variables
	// This function only handles volume mounting for certificates

	// Verify volume is added
	if len(mdd.Volumes) != 1 {
		t.Fatalf("Expected 1 volume, got %d", len(mdd.Volumes))
	}

	volume := mdd.Volumes[0]
	if volume.Name != tlsCACertVolumeName {
		t.Errorf("Expected volume name %s, got %s", tlsCACertVolumeName, volume.Name)
	}
	if volume.VolumeSource.Secret == nil {
		t.Fatal("Expected Secret volume source, got nil")
	}
	if volume.VolumeSource.Secret.SecretName != "internal-ca-cert" {
		t.Errorf("Expected SecretName=internal-ca-cert, got %s", volume.VolumeSource.Secret.SecretName)
	}
	if *volume.VolumeSource.Secret.DefaultMode != 0444 {
		t.Errorf("Expected DefaultMode=0444, got %d", *volume.VolumeSource.Secret.DefaultMode)
	}

	// Verify volume mount is added
	if len(mdd.VolumeMounts) != 1 {
		t.Fatalf("Expected 1 volume mount, got %d", len(mdd.VolumeMounts))
	}

	mount := mdd.VolumeMounts[0]
	if mount.Name != tlsCACertVolumeName {
		t.Errorf("Expected volume mount name %s, got %s", tlsCACertVolumeName, mount.Name)
	}
	if mount.MountPath != tlsCACertMountPath {
		t.Errorf("Expected MountPath=%s, got %s", tlsCACertMountPath, mount.MountPath)
	}
	if !mount.ReadOnly {
		t.Error("Expected ReadOnly=true, got false")
	}
}

// Test_addTLSConfiguration_DefaultCACertKey verifies volume mounting works with default key
func Test_addTLSConfiguration_DefaultCACertKey(t *testing.T) {
	mdd := &modelDeploymentData{}
	tlsConfig := &v1alpha2.TLSConfig{
		CACertSecretRef: "internal-ca-cert",
		// CACertSecretKey not set, should default to "ca.crt"
	}

	addTLSConfiguration(mdd, tlsConfig)

	// Note: Certificate path is now included in agent config JSON via populateTLSFields
	// This test verifies volume mounting still works correctly

	// Verify volume is added
	if len(mdd.Volumes) != 1 {
		t.Fatalf("Expected 1 volume, got %d", len(mdd.Volumes))
	}

	// Verify volume mount is added at the correct path
	if len(mdd.VolumeMounts) != 1 {
		t.Fatalf("Expected 1 volume mount, got %d", len(mdd.VolumeMounts))
	}

	mount := mdd.VolumeMounts[0]
	if mount.MountPath != tlsCACertMountPath {
		t.Errorf("Expected MountPath=%s, got %s", tlsCACertMountPath, mount.MountPath)
	}
}

// Test_addTLSConfiguration_CustomCertKey verifies volume mounting works with custom key
func Test_addTLSConfiguration_CustomCertKey(t *testing.T) {
	mdd := &modelDeploymentData{}
	tlsConfig := &v1alpha2.TLSConfig{
		CACertSecretRef: "internal-ca-cert",
		CACertSecretKey: "custom-ca.pem",
	}

	addTLSConfiguration(mdd, tlsConfig)

	// Note: Certificate path (including custom key) is now included in agent config JSON via populateTLSFields
	// This test verifies volume mounting still works correctly

	// Verify volume is added
	if len(mdd.Volumes) != 1 {
		t.Fatalf("Expected 1 volume, got %d", len(mdd.Volumes))
	}

	// Verify volume mount is added at the correct path
	if len(mdd.VolumeMounts) != 1 {
		t.Fatalf("Expected 1 volume mount, got %d", len(mdd.VolumeMounts))
	}

	mount := mdd.VolumeMounts[0]
	if mount.MountPath != tlsCACertMountPath {
		t.Errorf("Expected MountPath=%s, got %s", tlsCACertMountPath, mount.MountPath)
	}
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

			// Note: DisableSystemCAs configuration is now passed via agent config JSON, not environment variables
			// addTLSConfiguration only handles volume mounting

			// Should not add volumes when no CACertSecretRef is set
			if len(mdd.Volumes) != 0 {
				t.Errorf("Expected no volumes when CACertSecretRef is empty, got %d", len(mdd.Volumes))
			}
			if len(mdd.VolumeMounts) != 0 {
				t.Errorf("Expected no volume mounts when CACertSecretRef is empty, got %d", len(mdd.VolumeMounts))
			}
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

	// Note: TLS configuration fields (DisableVerify, DisableSystemCAs, CACertPath) are now
	// passed via agent config JSON by populateTLSFields, not environment variables
	// This test verifies volume mounting still works correctly

	// Verify volume and mount
	if len(mdd.Volumes) != 1 || len(mdd.VolumeMounts) != 1 {
		t.Errorf("Expected 1 volume and 1 mount, got %d volumes and %d mounts",
			len(mdd.Volumes), len(mdd.VolumeMounts))
	}

	// Verify volume references correct Secret
	volume := mdd.Volumes[0]
	if volume.VolumeSource.Secret == nil {
		t.Fatal("Expected Secret volume source, got nil")
	}
	if volume.VolumeSource.Secret.SecretName != "my-ca-bundle" {
		t.Errorf("Expected SecretName=my-ca-bundle, got %s", volume.VolumeSource.Secret.SecretName)
	}

	// Verify mount path is correct
	mount := mdd.VolumeMounts[0]
	if mount.MountPath != tlsCACertMountPath {
		t.Errorf("Expected MountPath=%s, got %s", tlsCACertMountPath, mount.MountPath)
	}
}
