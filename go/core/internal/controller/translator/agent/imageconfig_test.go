package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

const (
	testSlimDigest = "sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	testFullDigest = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
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
	originalFull := PythonADKFullImageDigest
	t.Cleanup(func() {
		PythonADKImageDigest = original
		PythonADKFullImageDigest = originalFull
	})
	PythonADKImageDigest = "sha256:app-digest"
	PythonADKFullImageDigest = "sha256:app-full-digest"

	got, err := resolvePythonRuntimeImage("ghcr.io", false, true)
	require.NoError(t, err)
	require.Equal(t, "ghcr.io/kagent-dev/kagent/app@sha256:app-digest", got)

	gotFull, err := resolvePythonRuntimeImage("ghcr.io", true, true)
	require.NoError(t, err)
	require.Equal(t, "ghcr.io/kagent-dev/kagent/app@sha256:app-full-digest", gotFull)
}

func TestResolvePythonFullRuntimeImageWithoutDigest(t *testing.T) {
	original := PythonADKFullImageDigest
	t.Cleanup(func() {
		PythonADKFullImageDigest = original
	})
	PythonADKFullImageDigest = ""

	_, err := resolvePythonRuntimeImage("ghcr.io", true, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "app-full")
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

	_, err := resolvePythonRuntimeImage("ghcr.io", false, true)
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

	got, err := resolvePythonRuntimeImage("my-registry.example.com", false, false)
	require.NoError(t, err)
	require.Equal(t, "my-registry.example.com/kagent-dev/kagent/app:v9.9.9", got)

	got, err = resolvePythonRuntimeImage("my-registry.example.com", true, false)
	require.NoError(t, err)
	require.Equal(t, "my-registry.example.com/kagent-dev/kagent/app:v9.9.9-full", got)

	got, err = resolveGoRuntimeImage("my-registry.example.com", false, false)
	require.NoError(t, err)
	require.Equal(t, "my-registry.example.com/kagent-dev/kagent/golang-adk:v8.8.8", got)

	got, err = resolveGoRuntimeImage("my-registry.example.com", true, false)
	require.NoError(t, err)
	require.Equal(t, "my-registry.example.com/kagent-dev/kagent/golang-adk:v8.8.8-full", got)
}

func TestResolveRuntimeImageByTagIgnoresMissingDigest(t *testing.T) {
	original := PythonADKImageDigest
	t.Cleanup(func() { PythonADKImageDigest = original })
	PythonADKImageDigest = ""

	_, err := resolvePythonRuntimeImage("ghcr.io", false, false)
	require.NoError(t, err)
}

func TestResolveInlineDeploymentImagePinning(t *testing.T) {
	original := PythonADKImageDigest
	t.Cleanup(func() { PythonADKImageDigest = original })
	PythonADKImageDigest = "sha256:pin-test"

	spec := v1alpha2.AgentSpec{
		Type:        v1alpha2.AgentType_Declarative,
		Declarative: &v1alpha2.DeclarativeAgentSpec{SystemMessage: "test", ModelConfig: "test-model"},
	}

	regular := &v1alpha2.Agent{Spec: spec}
	dep, err := resolveInlineDeployment(regular, &modelDeploymentData{})
	require.NoError(t, err)
	require.NotContains(t, dep.Image, "@sha256:", "regular agents reference images by tag")
	require.Contains(t, dep.Image, ":"+DefaultImageConfig.Tag)

	sandbox := &v1alpha2.SandboxAgent{Spec: v1alpha2.SandboxAgentSpec{AgentSpec: spec}}
	sdep, err := resolveInlineDeployment(sandbox, &modelDeploymentData{})
	require.NoError(t, err)
	require.Contains(t, sdep.Image, "@sha256:pin-test", "sandbox agents require digest-pinned images (Substrate rejects tag refs)")
}

func TestSplitImageTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		tag        string
		wantName   string
		wantDigest string
		wantErr    string
	}{
		{name: "bare tag", tag: "0.10.0-rc3", wantName: "0.10.0-rc3"},
		{name: "tag at digest", tag: "0.10.0-rc3@" + testSlimDigest, wantName: "0.10.0-rc3", wantDigest: testSlimDigest},
		{name: "digest only", tag: testSlimDigest, wantDigest: testSlimDigest},
		{name: "at digest only", tag: "@" + testSlimDigest, wantDigest: testSlimDigest},
		{name: "empty", tag: "   ", wantErr: "image tag is empty"},
		{name: "empty digest", tag: "0.10.0-rc3@", wantErr: "empty digest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotName, gotDigest, err := splitImageTag(tt.tag)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantName, gotName)
			require.Equal(t, tt.wantDigest, gotDigest)
		})
	}
}

func TestResolveRuntimeImageDigestPinnedTagDoesNotAppendFullToDigest(t *testing.T) {
	originalTag := DefaultImageConfig.Tag
	originalFull := PythonADKFullImageDigest
	t.Cleanup(func() {
		DefaultImageConfig.Tag = originalTag
		PythonADKFullImageDigest = originalFull
	})
	DefaultImageConfig.Tag = "0.10.0-rc3@" + testSlimDigest
	PythonADKFullImageDigest = ""

	got, err := resolvePythonRuntimeImage("ghcr.io", true, false)
	require.NoError(t, err)
	require.Equal(t, "ghcr.io/kagent-dev/kagent/app:0.10.0-rc3-full", got)
	require.NotContains(t, got, "deadbeef")
	require.False(t, strings.Contains(got, testSlimDigest+"-full"), "must not append -full after the slim digest")
}

func TestResolveRuntimeImageDigestPinnedTagUsesDedicatedFullDigest(t *testing.T) {
	originalTag := DefaultImageConfig.Tag
	originalFull := PythonADKFullImageDigest
	t.Cleanup(func() {
		DefaultImageConfig.Tag = originalTag
		PythonADKFullImageDigest = originalFull
	})
	DefaultImageConfig.Tag = "0.10.0-rc3@" + testSlimDigest
	PythonADKFullImageDigest = testFullDigest

	got, err := resolvePythonRuntimeImage("ghcr.io", true, false)
	require.NoError(t, err)
	require.Equal(t, "ghcr.io/kagent-dev/kagent/app:0.10.0-rc3-full@"+testFullDigest, got)
	require.NotContains(t, got, "deadbeef")
}

func TestResolveRuntimeImageDigestPinnedTagDoesNotReuseSlimDigestForGo(t *testing.T) {
	originalTag := DefaultGoImageConfig.Tag
	originalFull := GoADKFullImageDigest
	t.Cleanup(func() {
		DefaultGoImageConfig.Tag = originalTag
		GoADKFullImageDigest = originalFull
	})
	DefaultGoImageConfig.Tag = "0.10.0-rc3@" + testSlimDigest
	GoADKFullImageDigest = testFullDigest

	got, err := resolveGoRuntimeImage("ghcr.io", true, false)
	require.NoError(t, err)
	require.Equal(t, "ghcr.io/kagent-dev/kagent/golang-adk:0.10.0-rc3-full@"+testFullDigest, got)
	require.NotContains(t, got, "deadbeef")
}

func TestResolveRuntimeImagePreservesEmbeddedDigestForSlimImage(t *testing.T) {
	originalTag := DefaultImageConfig.Tag
	t.Cleanup(func() { DefaultImageConfig.Tag = originalTag })
	DefaultImageConfig.Tag = "0.10.0-rc3@" + testSlimDigest

	got, err := resolvePythonRuntimeImage("ghcr.io", false, false)
	require.NoError(t, err)
	require.Equal(t, "ghcr.io/kagent-dev/kagent/app:0.10.0-rc3@"+testSlimDigest, got)
}

func TestResolveRuntimeImageDoesNotDoubleFullSuffix(t *testing.T) {
	originalTag := DefaultImageConfig.Tag
	t.Cleanup(func() { DefaultImageConfig.Tag = originalTag })
	DefaultImageConfig.Tag = "0.10.0-rc3-full"

	got, err := resolvePythonRuntimeImage("ghcr.io", true, false)
	require.NoError(t, err)
	require.Equal(t, "ghcr.io/kagent-dev/kagent/app:0.10.0-rc3-full", got)
}

func TestResolveRuntimeImageDigestOnlyTagFailsClosedWithoutFullDigest(t *testing.T) {
	originalTag := DefaultImageConfig.Tag
	originalFull := PythonADKFullImageDigest
	t.Cleanup(func() {
		DefaultImageConfig.Tag = originalTag
		PythonADKFullImageDigest = originalFull
	})
	DefaultImageConfig.Tag = testSlimDigest
	PythonADKFullImageDigest = ""

	_, err := resolvePythonRuntimeImage("ghcr.io", true, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "digest-only")
	require.Contains(t, err.Error(), "app-full-image-digest")
}

func TestResolveRuntimeImageDigestOnlyTagUsesDedicatedFullDigest(t *testing.T) {
	originalTag := DefaultImageConfig.Tag
	originalFull := PythonADKFullImageDigest
	t.Cleanup(func() {
		DefaultImageConfig.Tag = originalTag
		PythonADKFullImageDigest = originalFull
	})
	DefaultImageConfig.Tag = testSlimDigest
	PythonADKFullImageDigest = testFullDigest

	got, err := resolvePythonRuntimeImage("ghcr.io", true, false)
	require.NoError(t, err)
	require.Equal(t, "ghcr.io/kagent-dev/kagent/app@"+testFullDigest, got)
}

func TestResolveRuntimeImageRejectsEmptyDigest(t *testing.T) {
	originalTag := DefaultImageConfig.Tag
	t.Cleanup(func() { DefaultImageConfig.Tag = originalTag })
	DefaultImageConfig.Tag = "0.10.0-rc3@"

	_, err := resolvePythonRuntimeImage("ghcr.io", true, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty digest")
}

func TestResolveInlineDeploymentSkillsDropsEmbeddedSlimDigest(t *testing.T) {
	originalTag := DefaultImageConfig.Tag
	originalFull := PythonADKFullImageDigest
	t.Cleanup(func() {
		DefaultImageConfig.Tag = originalTag
		PythonADKFullImageDigest = originalFull
	})
	DefaultImageConfig.Tag = "0.10.0-rc3@" + testSlimDigest
	PythonADKFullImageDigest = ""

	agent := &v1alpha2.Agent{
		Spec: v1alpha2.AgentSpec{
			Type:        v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{SystemMessage: "test", ModelConfig: "test-model"},
			Skills:      &v1alpha2.SkillForAgent{Refs: []string{"example.com/skill:latest"}},
		},
	}
	dep, err := resolveInlineDeployment(agent, &modelDeploymentData{})
	require.NoError(t, err)
	require.Equal(t, "ghcr.io/kagent-dev/kagent/app:0.10.0-rc3-full", dep.Image)
	require.NotContains(t, dep.Image, "deadbeef")
}
