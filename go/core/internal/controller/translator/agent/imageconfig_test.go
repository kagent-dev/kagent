package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
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

	got, err := resolveGoRuntimeImage("localhost:5001", false)
	require.NoError(t, err)
	require.Equal(t, "localhost:5001/kagent-dev/kagent/golang-adk@sha256:go-base", got)

	got, err = resolveGoRuntimeImage("localhost:5001", true)
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

	_, err := resolveGoRuntimeImage("localhost:5001", false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "golang-adk")

	_, err = resolveGoRuntimeImage("localhost:5001", true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "golang-adk-full")
}

func TestResolvePythonRuntimeImageWithDigest(t *testing.T) {
	original := PythonADKImageDigest
	t.Cleanup(func() {
		PythonADKImageDigest = original
	})
	PythonADKImageDigest = "sha256:app-digest"

	got, err := resolvePythonRuntimeImage("cr.kagent.dev")
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

	_, err := resolvePythonRuntimeImage("cr.kagent.dev")
	require.Error(t, err)
	require.Contains(t, err.Error(), "app")
}
