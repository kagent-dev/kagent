package agent

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

// Test_addTLSConfiguration_NoTLSConfig verifies that no volumes or env vars are added when TLS config is nil
func Test_addTLSConfiguration_NoTLSConfig(t *testing.T) {
	mdd := &modelDeploymentData{}

	addTLSConfiguration(mdd, nil)

	if len(mdd.EnvVars) != 0 {
		t.Errorf("Expected no environment variables, got %d", len(mdd.EnvVars))
	}
	if len(mdd.Volumes) != 0 {
		t.Errorf("Expected no volumes, got %d", len(mdd.Volumes))
	}
	if len(mdd.VolumeMounts) != 0 {
		t.Errorf("Expected no volume mounts, got %d", len(mdd.VolumeMounts))
	}
}

// Test_addTLSConfiguration_WithVerifyDisabled verifies env vars are set when TLS verify is disabled
func Test_addTLSConfiguration_WithVerifyDisabled(t *testing.T) {
	mdd := &modelDeploymentData{}
	tlsConfig := &v1alpha2.TLSConfig{
		VerifyDisabled: true,
		UseSystemCAs:   false,
	}

	addTLSConfiguration(mdd, tlsConfig)

	// Should have 2 environment variables (TLS_VERIFY_DISABLED, TLS_USE_SYSTEM_CAS)
	if len(mdd.EnvVars) != 2 {
		t.Errorf("Expected 2 environment variables, got %d", len(mdd.EnvVars))
	}

	// Verify TLS_VERIFY_DISABLED is set to true
	foundVerifyDisabled := false
	for _, env := range mdd.EnvVars {
		if env.Name == "TLS_VERIFY_DISABLED" {
			foundVerifyDisabled = true
			if env.Value != "true" {
				t.Errorf("Expected TLS_VERIFY_DISABLED=true, got %s", env.Value)
			}
		}
	}
	if !foundVerifyDisabled {
		t.Error("TLS_VERIFY_DISABLED environment variable not found")
	}

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
		VerifyDisabled:  false,
		CACertSecretRef: "internal-ca-cert",
		CACertSecretKey: "ca.crt",
		UseSystemCAs:    true,
	}

	addTLSConfiguration(mdd, tlsConfig)

	// Should have 3 environment variables (TLS_VERIFY_DISABLED, TLS_USE_SYSTEM_CAS, TLS_CA_CERT_PATH)
	if len(mdd.EnvVars) != 3 {
		t.Errorf("Expected 3 environment variables, got %d", len(mdd.EnvVars))
	}

	// Verify TLS_CA_CERT_PATH is set correctly
	foundCACertPath := false
	expectedPath := "/etc/ssl/certs/custom/ca.crt"
	for _, env := range mdd.EnvVars {
		if env.Name == "TLS_CA_CERT_PATH" {
			foundCACertPath = true
			if env.Value != expectedPath {
				t.Errorf("Expected TLS_CA_CERT_PATH=%s, got %s", expectedPath, env.Value)
			}
		}
	}
	if !foundCACertPath {
		t.Error("TLS_CA_CERT_PATH environment variable not found")
	}

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

// Test_addTLSConfiguration_DefaultCACertKey verifies default ca.crt key is used
func Test_addTLSConfiguration_DefaultCACertKey(t *testing.T) {
	mdd := &modelDeploymentData{}
	tlsConfig := &v1alpha2.TLSConfig{
		CACertSecretRef: "internal-ca-cert",
		// CACertSecretKey not set, should default to "ca.crt"
	}

	addTLSConfiguration(mdd, tlsConfig)

	// Verify TLS_CA_CERT_PATH uses default key
	foundCACertPath := false
	expectedPath := "/etc/ssl/certs/custom/ca.crt"
	for _, env := range mdd.EnvVars {
		if env.Name == "TLS_CA_CERT_PATH" {
			foundCACertPath = true
			if env.Value != expectedPath {
				t.Errorf("Expected TLS_CA_CERT_PATH with default key %s, got %s", expectedPath, env.Value)
			}
		}
	}
	if !foundCACertPath {
		t.Error("TLS_CA_CERT_PATH environment variable not found")
	}
}

// Test_addTLSConfiguration_CustomCertKey verifies custom certificate key is used
func Test_addTLSConfiguration_CustomCertKey(t *testing.T) {
	mdd := &modelDeploymentData{}
	tlsConfig := &v1alpha2.TLSConfig{
		CACertSecretRef: "internal-ca-cert",
		CACertSecretKey: "custom-ca.pem",
	}

	addTLSConfiguration(mdd, tlsConfig)

	// Verify TLS_CA_CERT_PATH uses custom key
	foundCACertPath := false
	expectedPath := "/etc/ssl/certs/custom/custom-ca.pem"
	for _, env := range mdd.EnvVars {
		if env.Name == "TLS_CA_CERT_PATH" {
			foundCACertPath = true
			if env.Value != expectedPath {
				t.Errorf("Expected TLS_CA_CERT_PATH=%s, got %s", expectedPath, env.Value)
			}
		}
	}
	if !foundCACertPath {
		t.Error("TLS_CA_CERT_PATH environment variable not found")
	}
}

// Test_addTLSConfiguration_UseSystemCAsFlag verifies UseSystemCAs env var
func Test_addTLSConfiguration_UseSystemCAsFlag(t *testing.T) {
	tests := []struct {
		name         string
		useSystemCAs bool
		expected     string
	}{
		{
			name:         "UseSystemCAs true",
			useSystemCAs: true,
			expected:     "true",
		},
		{
			name:         "UseSystemCAs false",
			useSystemCAs: false,
			expected:     "false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mdd := &modelDeploymentData{}
			tlsConfig := &v1alpha2.TLSConfig{
				UseSystemCAs: tt.useSystemCAs,
			}

			addTLSConfiguration(mdd, tlsConfig)

			foundUseSystemCAs := false
			for _, env := range mdd.EnvVars {
				if env.Name == "TLS_USE_SYSTEM_CAS" {
					foundUseSystemCAs = true
					if env.Value != tt.expected {
						t.Errorf("Expected TLS_USE_SYSTEM_CAS=%s, got %s", tt.expected, env.Value)
					}
				}
			}
			if !foundUseSystemCAs {
				t.Error("TLS_USE_SYSTEM_CAS environment variable not found")
			}
		})
	}
}

// Test_addTLSConfiguration_AllFieldsCombined verifies all fields work together
func Test_addTLSConfiguration_AllFieldsCombined(t *testing.T) {
	mdd := &modelDeploymentData{}
	tlsConfig := &v1alpha2.TLSConfig{
		VerifyDisabled:  false,
		CACertSecretRef: "my-ca-bundle",
		CACertSecretKey: "bundle.crt",
		UseSystemCAs:    true,
	}

	addTLSConfiguration(mdd, tlsConfig)

	// Verify all environment variables are set
	expectedEnvVars := map[string]string{
		"TLS_VERIFY_DISABLED": "false",
		"TLS_USE_SYSTEM_CAS":  "true",
		"TLS_CA_CERT_PATH":    "/etc/ssl/certs/custom/bundle.crt",
	}

	if len(mdd.EnvVars) != len(expectedEnvVars) {
		t.Errorf("Expected %d environment variables, got %d", len(expectedEnvVars), len(mdd.EnvVars))
	}

	for expectedName, expectedValue := range expectedEnvVars {
		found := false
		for _, env := range mdd.EnvVars {
			if env.Name == expectedName {
				found = true
				if env.Value != expectedValue {
					t.Errorf("Expected %s=%s, got %s", expectedName, expectedValue, env.Value)
				}
				break
			}
		}
		if !found {
			t.Errorf("Expected environment variable %s not found", expectedName)
		}
	}

	// Verify volume and mount
	if len(mdd.Volumes) != 1 || len(mdd.VolumeMounts) != 1 {
		t.Errorf("Expected 1 volume and 1 mount, got %d volumes and %d mounts",
			len(mdd.Volumes), len(mdd.VolumeMounts))
	}
}
