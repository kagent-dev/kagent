/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha3

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl_client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// envtestAssetsDir returns the envtest binary dir from KUBEBUILDER_ASSETS, or
// shells out to the `envtest-path` Makefile target.
//
// On a fresh checkout the Makefile target's first invocation may also run
// `go install` for setup-envtest, mixing "go: downloading ..." chatter into
// the captured output ahead of the actual path. Return the last non-empty
// line so that bootstrap noise doesn't poison the binary directory string.
func envtestAssetsDir(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("KUBEBUILDER_ASSETS"); v != "" {
		return v
	}
	out, err := exec.Command("sh", "-c", "make -sC $(dirname $(go env GOMOD)) envtest-path").CombinedOutput()
	if err != nil {
		t.Fatalf("envtest binaries not found (run `make setup-envtest` or set KUBEBUILDER_ASSETS): %s – %v", out, err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	for _, raw := range slices.Backward(lines) {
		if line := strings.TrimSpace(raw); line != "" {
			return line
		}
	}
	t.Fatalf("envtest-path produced empty output")
	return ""
}

// crdBasesDir resolves the CRD-bases directory at runtime so the test
// reads the same YAML the helm chart ships.
func crdBasesDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	return filepath.Join(wd, "..", "config", "crd", "bases")
}

// TestTLSConfigCELValidation checks the supported TLS option and rejects deferred
// custom trust fields against the shipped CRDs with strict field validation.
func TestTLSConfigCELValidation(t *testing.T) {
	testEnv := &envtest.Environment{
		BinaryAssetsDirectory: envtestAssetsDir(t),
		CRDDirectoryPaths:     []string{crdBasesDir(t)},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { _ = testEnv.Stop() })
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, AddToScheme(scheme))
	cl, err := ctrl_client.New(cfg, ctrl_client.Options{Scheme: scheme})
	require.NoError(t, err)
	ctx := context.Background()
	const ns = "tls-cel"
	require.NoError(t, cl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}))

	for _, kind := range []string{"ModelConfig", "RemoteMCPServer"} {
		for _, tc := range []struct {
			name       string
			tls        map[string]any
			wantReject string
		}{
			{name: "default"},
			{name: "verify", tls: map[string]any{"disableVerify": false}},
			{name: "skip-verify", tls: map[string]any{"disableVerify": true}},
			{name: "ca-ref", tls: map[string]any{"caCertSecretRef": "ca"}, wantReject: "unknown field"},
			{name: "ca-key", tls: map[string]any{"caCertSecretKey": "ca.crt"}, wantReject: "unknown field"},
			{name: "system-cas", tls: map[string]any{"disableSystemCAs": true}, wantReject: "unknown field"},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				spec := map[string]any{"model": "gpt-4", "provider": "OpenAI"}
				if kind == "RemoteMCPServer" {
					spec = map[string]any{"description": "test", "url": "https://upstream.example.com/mcp"}
				}
				if tc.tls != nil {
					spec["tls"] = tc.tls
				}
				object := &unstructured.Unstructured{Object: map[string]any{
					"apiVersion": GroupVersion.String(), "kind": kind,
					"metadata": map[string]any{"name": strings.ToLower(kind) + "-" + tc.name, "namespace": ns},
					"spec":     spec,
				}}
				err := cl.Create(ctx, object, &ctrl_client.CreateOptions{FieldValidation: metav1.FieldValidationStrict})
				if tc.wantReject != "" {
					require.ErrorContains(t, err, tc.wantReject)
					return
				}
				require.NoError(t, err)
			})
		}
	}
	t.Run("HTTP rejects TLS", func(t *testing.T) {
		server := &RemoteMCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "http-with-tls", Namespace: ns},
			Spec:       RemoteMCPServerSpec{Description: "test", URL: "http://upstream.example.com/mcp", TLS: &TLSConfig{DisableVerify: true}},
		}
		require.ErrorContains(t, cl.Create(ctx, server), "spec.tls must be unset when spec.url has http:// scheme")
	})
}
