package agent

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

func TestImageConfigImage(t *testing.T) {
	cfg := ImageConfig{
		Registry:   "cr.kagent.dev",
		Repository: "kagent-dev/kagent/app",
		Tag:        "v1.0.0",
	}
	require.Equal(t, "cr.kagent.dev/kagent-dev/kagent/app:v1.0.0", cfg.Image())
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
		Registry:   "cr.kagent.dev",
		Repository: "kagent-dev/kagent/app",
		Tag:        "v1.0.0",
	}
	require.Equal(t, cfg.Image(), cfg.PinnedImage())
}

func TestResolveGoRuntimeImageWithDigest(t *testing.T) {
	originalBase := GoADKImageDigest
	originalFull := GoADKFullImageDigest
	t.Cleanup(func() {
		GoADKImageDigest = originalBase
		GoADKFullImageDigest = originalFull
	})
	GoADKImageDigest = "sha256:go-base"
	GoADKFullImageDigest = "sha256:go-full"

	got, err := resolveGoRuntimeImage("localhost:5001", false, true)
	require.NoError(t, err)
	require.Equal(t, "localhost:5001/kagent-dev/kagent/golang-adk@sha256:go-base", got)

	got, err = resolveGoRuntimeImage("localhost:5001", true, true)
	require.NoError(t, err)
	require.Equal(t, "localhost:5001/kagent-dev/kagent/golang-adk@sha256:go-full", got)
}

func TestResolveGoRuntimeImageWithoutDigest(t *testing.T) {
	originalBase := GoADKImageDigest
	originalFull := GoADKFullImageDigest
	t.Cleanup(func() {
		GoADKImageDigest = originalBase
		GoADKFullImageDigest = originalFull
	})
	GoADKImageDigest = ""
	GoADKFullImageDigest = ""

	_, err := resolveGoRuntimeImage("localhost:5001", false, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "golang-adk")

	_, err = resolveGoRuntimeImage("localhost:5001", true, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "golang-adk-full")
}

func TestResolvePythonRuntimeImageWithDigest(t *testing.T) {
	original := PythonADKImageDigest
	t.Cleanup(func() {
		PythonADKImageDigest = original
	})
	PythonADKImageDigest = "sha256:app-digest"

	got, err := resolvePythonRuntimeImage("cr.kagent.dev", true)
	require.NoError(t, err)
	require.Equal(t, "cr.kagent.dev/kagent-dev/kagent/app@sha256:app-digest", got)
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

	_, err := resolvePythonRuntimeImage("cr.kagent.dev", true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "app")
}

func TestResolveRuntimeImageByTag(t *testing.T) {
	originalTag := DefaultImageConfig.Tag
	t.Cleanup(func() { DefaultImageConfig.Tag = originalTag })
	DefaultImageConfig.Tag = "v9.9.9"

	got, err := resolvePythonRuntimeImage("my-registry.example.com", false)
	require.NoError(t, err)
	require.Equal(t, "my-registry.example.com/kagent-dev/kagent/app:v9.9.9", got)

	got, err = resolveGoRuntimeImage("my-registry.example.com", false, false)
	require.NoError(t, err)
	require.Equal(t, "my-registry.example.com/kagent-dev/kagent/golang-adk:v9.9.9", got)

	got, err = resolveGoRuntimeImage("my-registry.example.com", true, false)
	require.NoError(t, err)
	require.Equal(t, "my-registry.example.com/kagent-dev/kagent/golang-adk:v9.9.9-full", got)
}

func TestResolveRuntimeImageByTagIgnoresMissingDigest(t *testing.T) {
	original := PythonADKImageDigest
	t.Cleanup(func() { PythonADKImageDigest = original })
	PythonADKImageDigest = ""

	_, err := resolvePythonRuntimeImage("cr.kagent.dev", false)
	require.NoError(t, err)
}

func TestResolveInlineDeploymentImagePinning(t *testing.T) {
	original := PythonADKImageDigest
	originalGo := GoADKImageDigest
	t.Cleanup(func() {
		PythonADKImageDigest = original
		GoADKImageDigest = originalGo
	})
	PythonADKImageDigest = "sha256:pin-test"
	GoADKImageDigest = "sha256:pin-test-go"

	spec := v1alpha2.AgentSpec{
		Type:        v1alpha2.AgentType_Declarative,
		Declarative: &v1alpha2.DeclarativeAgentSpec{SystemMessage: "test", ModelConfig: "test-model"},
	}

	regular := &v1alpha2.Agent{Spec: spec}
	dep, err := resolveInlineDeployment(regular, &modelDeploymentData{})
	require.NoError(t, err)
	require.NotContains(t, dep.Image, "@sha256:", "regular agents reference images by tag")
	require.Contains(t, dep.Image, ":"+DefaultImageConfig.Tag)

	agentSandbox := &v1alpha2.SandboxAgent{Spec: v1alpha2.SandboxAgentSpec{AgentSpec: spec}}
	adep, err := resolveInlineDeployment(agentSandbox, &modelDeploymentData{})
	require.NoError(t, err)
	require.NotContains(t, adep.Image, "@sha256:", "agent-sandbox platform sandbox agents reference images by tag")
	require.Contains(t, adep.Image, ":"+DefaultImageConfig.Tag)

	substrate := &v1alpha2.SandboxAgent{Spec: v1alpha2.SandboxAgentSpec{AgentSpec: spec, Platform: v1alpha2.SandboxPlatformSubstrate}}
	sdep, err := resolveInlineDeployment(substrate, &modelDeploymentData{})
	require.NoError(t, err)
	require.Contains(t, sdep.Image, "@sha256:pin-test-go", "substrate sandbox agents require digest-pinned images (Substrate rejects tag refs)")
}
