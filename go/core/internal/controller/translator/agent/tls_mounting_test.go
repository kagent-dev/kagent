package agent

import (
	"path"
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

	wantVolumeName, wantMountPath, _ := tlsCAPaths("internal-ca-cert", "ca.crt")

	require.Len(t, mdd.Volumes, 1, "Expected 1 volume for TLS cert secret")
	volume := mdd.Volumes[0]
	assert.Equal(t, wantVolumeName, volume.Name, "Volume name should be derived from Secret name")
	require.NotNil(t, volume.Secret, "Expected Secret volume source")
	assert.Equal(t, "internal-ca-cert", volume.Secret.SecretName, "Secret name should match CACertSecretRef")
	assert.Equal(t, int32(0444), *volume.Secret.DefaultMode, "DefaultMode should be 0444 for read-only cert")

	require.Len(t, mdd.VolumeMounts, 1, "Expected 1 volume mount for TLS cert")
	mount := mdd.VolumeMounts[0]
	assert.Equal(t, wantVolumeName, mount.Name, "Volume mount name should match volume name")
	assert.Equal(t, wantMountPath, mount.MountPath, "Mount path should be derived from Secret name")
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

	_, wantMountPath, _ := tlsCAPaths("internal-ca-cert", "custom-ca.pem")

	require.Len(t, mdd.Volumes, 1, "Expected 1 volume for TLS cert with custom key")
	require.Len(t, mdd.VolumeMounts, 1, "Expected 1 volume mount for TLS cert")

	mount := mdd.VolumeMounts[0]
	assert.Equal(t, wantMountPath, mount.MountPath, "Mount path should be derived from Secret name")
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

	_, wantMountPath, _ := tlsCAPaths("my-ca-bundle", "bundle.crt")

	require.Len(t, mdd.Volumes, 1, "Expected 1 volume for combined TLS config")
	require.Len(t, mdd.VolumeMounts, 1, "Expected 1 volume mount for combined TLS config")

	volume := mdd.Volumes[0]
	require.NotNil(t, volume.Secret, "Expected Secret volume source")
	assert.Equal(t, "my-ca-bundle", volume.Secret.SecretName, "Secret name should match CACertSecretRef")

	mount := mdd.VolumeMounts[0]
	assert.Equal(t, wantMountPath, mount.MountPath, "Mount path should be derived from Secret name")
}

// Test_addTLSConfiguration_MultipleSecretsDoNotCollide verifies that mounting
// CAs from different Secrets on the same agent produces distinct volumes and
// distinct mount paths so the merge dedupe (mergeDeploymentData) preserves
// both. Without per-Secret naming, the merge would silently drop one because
// it dedupes by volume name and mount path. That matters now that RMSs and
// ModelConfigs can each contribute CA mounts to the same agent pod.
func Test_addTLSConfiguration_MultipleSecretsDoNotCollide(t *testing.T) {
	mdd := &modelDeploymentData{}

	addTLSConfiguration(mdd, &v1alpha2.TLSConfig{
		CACertSecretRef: "chat-ca",
		CACertSecretKey: "ca.crt",
	})
	addTLSConfiguration(mdd, &v1alpha2.TLSConfig{
		CACertSecretRef: "embedding-ca",
		CACertSecretKey: "ca.crt",
	})
	addTLSConfiguration(mdd, &v1alpha2.TLSConfig{
		CACertSecretRef: "rms-corp-ca",
		CACertSecretKey: "ca.crt",
	})

	require.Len(t, mdd.Volumes, 3, "Three distinct Secrets should produce three volumes")
	require.Len(t, mdd.VolumeMounts, 3, "Three distinct Secrets should produce three mounts")

	seenNames := map[string]struct{}{}
	seenPaths := map[string]struct{}{}
	for _, v := range mdd.Volumes {
		seenNames[v.Name] = struct{}{}
	}
	for _, m := range mdd.VolumeMounts {
		seenPaths[m.MountPath] = struct{}{}
	}
	assert.Len(t, seenNames, 3, "Volume names should be distinct per Secret")
	assert.Len(t, seenPaths, 3, "Mount paths should be distinct per Secret")
}

// Test_addTLSConfiguration_SameSecretMergesCleanly verifies that referencing
// the same Secret + key from multiple sources produces identical volume name
// and mount path on each call. The downstream merge dedupes by both, so the
// agent pod ends up with exactly one mount even when several
// ModelConfigs/RMSs share a Secret.
func Test_addTLSConfiguration_SameSecretMergesCleanly(t *testing.T) {
	a := &modelDeploymentData{}
	b := &modelDeploymentData{}

	addTLSConfiguration(a, &v1alpha2.TLSConfig{
		CACertSecretRef: "shared-ca",
		CACertSecretKey: "ca.crt",
	})
	addTLSConfiguration(b, &v1alpha2.TLSConfig{
		CACertSecretRef: "shared-ca",
		CACertSecretKey: "ca.crt",
	})

	require.Len(t, a.Volumes, 1)
	require.Len(t, b.Volumes, 1)
	assert.Equal(t, a.Volumes[0].Name, b.Volumes[0].Name, "Same Secret should produce identical volume name")
	assert.Equal(t, a.VolumeMounts[0].MountPath, b.VolumeMounts[0].MountPath, "Same Secret should produce identical mount path")
}

// Test_tlsCAPaths_LongSecretNameHashes verifies that Secret names which would
// overflow the K8s DNS_LABEL 63-char limit on the volume name get hashed.
func Test_tlsCAPaths_LongSecretNameHashes(t *testing.T) {
	// 60-char Secret name → "tls-ca-" + 60 = 67 chars > 63. Must hash.
	longName := "a-very-long-secret-name-that-just-exceeds-the-volume-limit-x"
	require.Len(t, longName, 60)

	volumeName, mountPath, certPath := tlsCAPaths(longName, "ca.crt")

	assert.LessOrEqual(t, len(volumeName), 63, "Volume name must fit DNS_LABEL")
	assert.NotContains(t, volumeName, longName, "Long names should be hashed, not embedded literally")
	assert.True(t, len(mountPath) > len(tlsCAMountRoot), "Mount path should include hashed component")
	assert.Equal(t, certPath, path.Join(mountPath, "ca.crt"))
}

// Test_tlsCAPaths_ShortSecretNameEmbeds verifies that Secret names that fit
// the volume-name budget appear in the volume name and mount path verbatim,
// for operator readability when inspecting an agent's pod spec.
func Test_tlsCAPaths_ShortSecretNameEmbeds(t *testing.T) {
	volumeName, mountPath, certPath := tlsCAPaths("corp-ca", "ca.crt")

	assert.Equal(t, "tls-ca-corp-ca", volumeName)
	assert.Equal(t, "/etc/ssl/certs/custom/corp-ca", mountPath)
	assert.Equal(t, "/etc/ssl/certs/custom/corp-ca/ca.crt", certPath)
}

// Test_tlsCAPaths_DottedSecretNameHashes covers the case where the Secret
// name follows DNS_SUBDOMAIN (allowed by K8s for Secret names: dots and
// up to 253 chars) but violates DNS_LABEL (required for volume names: no
// dots, ≤ 63 chars). Common examples: cert-manager output Secrets named
// after the certificate (`mcp.example.com-tls`), or operator-authored
// names that mirror an FQDN. Without charset-aware hashing, the produced
// volume name would embed the dot and the agent Deployment would fail to
// apply with a `spec.template.spec.volumes[N].name: Invalid value` error.
func Test_tlsCAPaths_DottedSecretNameHashes(t *testing.T) {
	cases := []string{
		"corp.ca",
		"mcp.example.com-tls",
		"my.cert.bundle",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			volumeName, mountPath, certPath := tlsCAPaths(name, "ca.crt")
			assert.NotContains(t, volumeName, ".",
				"Volume name must not contain dots (DNS_LABEL) — hash when Secret name does")
			assert.Regexp(t, `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`, volumeName,
				"Volume name must satisfy RFC 1123 label")
			assert.LessOrEqual(t, len(volumeName), 63, "Volume name must fit DNS_LABEL")
			assert.Equal(t, certPath, path.Join(mountPath, "ca.crt"))
		})
	}
}

// Test_tlsCAPaths_Deterministic verifies that two calls with the same input
// produce identical output, including when hashing kicks in. The merge
// dedupe relies on identical volume names + mount paths for the same
// Secret regardless of which call site produced them.
func Test_tlsCAPaths_Deterministic(t *testing.T) {
	for _, name := range []string{"short-name", "corp.ca", "a-very-long-secret-name-that-just-exceeds-the-volume-limit-x"} {
		v1, m1, c1 := tlsCAPaths(name, "ca.crt")
		v2, m2, c2 := tlsCAPaths(name, "ca.crt")
		assert.Equal(t, v1, v2, "tlsCAPaths must be deterministic for volume name (%s)", name)
		assert.Equal(t, m1, m2, "tlsCAPaths must be deterministic for mount path (%s)", name)
		assert.Equal(t, c1, c2, "tlsCAPaths must be deterministic for cert path (%s)", name)
	}
}
