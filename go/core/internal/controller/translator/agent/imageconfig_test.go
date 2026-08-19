package agent

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestImageConfigImage(t *testing.T) {
	cfg := ImageConfig{
		Registry:   "ghcr.io",
		Repository: "kagent-dev/kagent/app",
		Tag:        "v1.0.0",
	}
	require.Equal(t, "ghcr.io/kagent-dev/kagent/app:v1.0.0", cfg.Image())
}

func TestImageConfigPinnedImage(t *testing.T) {
	cfg := ImageConfig{
		Registry:   "localhost:5001",
		Repository: "kagent-dev/kagent/app",
		Tag:        "v1.0.0",
		Digest:     "sha256:abc123",
	}
	require.Equal(t, "localhost:5001/kagent-dev/kagent/app@sha256:abc123", cfg.PinnedImage())
	require.Equal(t, "localhost:5001/kagent-dev/kagent/app:v1.0.0", cfg.Image())
}

func TestImageConfigPinnedImageWithoutDigest(t *testing.T) {
	cfg := ImageConfig{
		Registry:   "ghcr.io",
		Repository: "kagent-dev/kagent/app",
		Tag:        "v1.0.0",
	}
	require.Equal(t, cfg.Image(), cfg.PinnedImage())
}

func TestResolveGoRuntimeImageWithDigest(t *testing.T) {
	originalBase := GoADKImageDigest
	t.Cleanup(func() {
		GoADKImageDigest = originalBase
	})
	GoADKImageDigest = "sha256:go-base"

	got, err := resolveGoRuntimeImage("localhost:5001", true)
	require.NoError(t, err)
	require.Equal(t, "localhost:5001/kagent-dev/kagent/golang-adk@sha256:go-base", got)
}

func TestResolveGoRuntimeImageWithoutDigest(t *testing.T) {
	originalBase := GoADKImageDigest
	t.Cleanup(func() {
		GoADKImageDigest = originalBase
	})
	GoADKImageDigest = ""

	_, err := resolveGoRuntimeImage("localhost:5001", true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "golang-adk")
}

func TestResolvePythonRuntimeImageWithDigest(t *testing.T) {
	original := PythonADKImageDigest
	t.Cleanup(func() {
		PythonADKImageDigest = original
	})
	PythonADKImageDigest = "sha256:app-digest"

	got, err := resolvePythonRuntimeImage("ghcr.io", true)
	require.NoError(t, err)
	require.Equal(t, "ghcr.io/kagent-dev/kagent/app@sha256:app-digest", got)
}

func TestPythonADKImageDigestSupportsLinkerFlag(t *testing.T) {
	// PythonADKImageDigest must be a package-level string var so
	// scripts/controller-digest-ldflags.sh can inject it via -ldflags -X.
	original := PythonADKImageDigest
	t.Cleanup(func() {
		PythonADKImageDigest = original
	})
	PythonADKImageDigest = "sha256:link-time-check"
	require.Equal(t, "sha256:link-time-check", PythonADKImageDigest)
}

func TestResolvePythonRuntimeImageWithoutDigest(t *testing.T) {
	original := PythonADKImageDigest
	t.Cleanup(func() {
		PythonADKImageDigest = original
	})
	PythonADKImageDigest = ""

	_, err := resolvePythonRuntimeImage("ghcr.io", true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "app")
}

func TestResolveRuntimeImageByTag(t *testing.T) {
	originalTag := DefaultImageConfig.Tag
	originalGoTag := DefaultGoImageConfig.Tag
	t.Cleanup(func() {
		DefaultImageConfig.Tag = originalTag
		DefaultGoImageConfig.Tag = originalGoTag
	})
	DefaultImageConfig.Tag = "v9.9.9"
	DefaultGoImageConfig.Tag = "v8.8.8"

	got, err := resolvePythonRuntimeImage("my-registry.example.com", false)
	require.NoError(t, err)
	require.Equal(t, "my-registry.example.com/kagent-dev/kagent/app:v9.9.9", got)

	got, err = resolveGoRuntimeImage("my-registry.example.com", false)
	require.NoError(t, err)
	require.Equal(t, "my-registry.example.com/kagent-dev/kagent/golang-adk:v8.8.8", got)
}

func TestResolveRuntimeImageByTagIgnoresMissingDigest(t *testing.T) {
	original := PythonADKImageDigest
	t.Cleanup(func() { PythonADKImageDigest = original })
	PythonADKImageDigest = ""

	_, err := resolvePythonRuntimeImage("ghcr.io", false)
	require.NoError(t, err)
}

func TestResolveInlineDeploymentImagePinning(t *testing.T) {
	original := PythonADKImageDigest
	t.Cleanup(func() { PythonADKImageDigest = original })
	PythonADKImageDigest = "sha256:pin-test"

	spec := v1alpha3.AgentSpec{
		Type:        v1alpha3.AgentType_Declarative,
		Declarative: &v1alpha3.DeclarativeAgentSpec{SystemMessage: "test", ModelConfig: "test-model"},
	}

	sandbox := &v1alpha3.SandboxAgent{Spec: spec}
	sdep, err := resolveInlineDeployment(sandbox, &modelDeploymentData{})
	require.NoError(t, err)
	require.Contains(t, sdep.Image, "@sha256:pin-test", "sandbox agents require digest-pinned images (Substrate rejects tag refs)")
}
