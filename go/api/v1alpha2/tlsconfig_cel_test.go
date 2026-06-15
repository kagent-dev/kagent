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

package v1alpha2

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

// TestTLSConfigCELValidation pins the TLSConfig CEL rules and the
// RemoteMCPServerSpec http://-with-tls rule against a real kube-apiserver
// loaded with the shipped CRDs. Catches regressions where:
//   - the type-level XValidation rules silently drop from the generated
//     CRD YAML (controller-gen upgrade, type rename, accidental edit)
//   - one CRD consumer loses the rules while another keeps them
//   - rule semantics drift when consumers are added
//
// The rules live on the TLSConfig type itself so every consumer (ModelConfig
// today, RemoteMCPServer in the current PR, future resources) inherits the
// same admission validation without re-declaring rules per spec.
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

	cases := []struct {
		name       string
		build      func() ctrl_client.Object
		wantReject string // substring in admission error; empty means accept
	}{
		// TLSConfig type-level rules — apply to every consumer.
		{
			name: "ModelConfig: caCertSecretRef without key rejected",
			build: func() ctrl_client.Object {
				return &ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "mc-ref-no-key", Namespace: ns},
					Spec: ModelConfigSpec{
						Model:    "gpt-4",
						Provider: ModelProviderOpenAI,
						TLS:      &TLSConfig{CACertSecretRef: "ca"},
					},
				}
			},
			wantReject: "caCertSecretRef requires caCertSecretKey",
		},
		{
			name: "ModelConfig: caCertSecretKey without ref rejected",
			build: func() ctrl_client.Object {
				return &ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "mc-key-no-ref", Namespace: ns},
					Spec: ModelConfigSpec{
						Model:    "gpt-4",
						Provider: ModelProviderOpenAI,
						TLS:      &TLSConfig{CACertSecretKey: "ca.crt"},
					},
				}
			},
			wantReject: "caCertSecretKey requires caCertSecretRef",
		},
		{
			name: "ModelConfig: disableSystemCAs alone rejected",
			build: func() ctrl_client.Object {
				return &ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "mc-dscas-alone", Namespace: ns},
					Spec: ModelConfigSpec{
						Model:    "gpt-4",
						Provider: ModelProviderOpenAI,
						TLS:      &TLSConfig{DisableSystemCAs: true},
					},
				}
			},
			wantReject: "disableSystemCAs requires caCertSecretRef or disableVerify",
		},
		{
			name: "RemoteMCPServer: caCertSecretRef without key rejected",
			build: func() ctrl_client.Object {
				return &RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "rms-ref-no-key", Namespace: ns},
					Spec: RemoteMCPServerSpec{
						Description: "test",
						URL:         "https://upstream.example.com/mcp",
						TLS:         &TLSConfig{CACertSecretRef: "ca"},
					},
				}
			},
			wantReject: "caCertSecretRef requires caCertSecretKey",
		},
		{
			name: "RemoteMCPServer: caCertSecretKey without ref rejected",
			build: func() ctrl_client.Object {
				return &RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "rms-key-no-ref", Namespace: ns},
					Spec: RemoteMCPServerSpec{
						Description: "test",
						URL:         "https://upstream.example.com/mcp",
						TLS:         &TLSConfig{CACertSecretKey: "ca.crt"},
					},
				}
			},
			wantReject: "caCertSecretKey requires caCertSecretRef",
		},
		{
			name: "RemoteMCPServer: disableSystemCAs alone rejected",
			build: func() ctrl_client.Object {
				return &RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "rms-dscas-alone", Namespace: ns},
					Spec: RemoteMCPServerSpec{
						Description: "test",
						URL:         "https://upstream.example.com/mcp",
						TLS:         &TLSConfig{DisableSystemCAs: true},
					},
				}
			},
			wantReject: "disableSystemCAs requires caCertSecretRef or disableVerify",
		},
		// RemoteMCPServerSpec-level rule — only on the RMS CRD.
		{
			name: "RemoteMCPServer: http URL with non-empty tls rejected",
			build: func() ctrl_client.Object {
				return &RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "rms-http-with-tls", Namespace: ns},
					Spec: RemoteMCPServerSpec{
						Description: "test",
						URL:         "http://upstream.example.com/mcp",
						TLS:         &TLSConfig{DisableVerify: true},
					},
				}
			},
			wantReject: "spec.tls must be unset when spec.url has http:// scheme",
		},
		// Positive case: https + no spec.tls is admitted (system trust default).
		{
			name: "RemoteMCPServer: https URL with no spec.tls accepted",
			build: func() ctrl_client.Object {
				return &RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "rms-https-no-tls", Namespace: ns},
					Spec: RemoteMCPServerSpec{
						Description: "test",
						URL:         "https://upstream.example.com/mcp",
					},
				}
			},
		},
		// caCertSecretRef ⊥ cross-namespace-permitting allowedNamespaces: a pinned
		// CA Secret can't be mounted across namespaces. from=All / from=Selector
		// are rejected alongside a CA; from=Same (or omitted) is fine.
		{
			name: "RemoteMCPServer: allowedNamespaces from=All with caCertSecretRef rejected",
			build: func() ctrl_client.Object {
				return &RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "rms-allowedns-all-ca", Namespace: ns},
					Spec: RemoteMCPServerSpec{
						Description:       "test",
						URL:               "https://upstream.example.com/mcp",
						AllowedNamespaces: &AllowedNamespaces{From: NamespacesFromAll},
						TLS:               &TLSConfig{CACertSecretRef: "ca", CACertSecretKey: "ca.crt"},
					},
				}
			},
			wantReject: "spec.allowedNamespaces must not permit cross-namespace access",
		},
		{
			name: "RemoteMCPServer: allowedNamespaces from=Selector with caCertSecretRef rejected",
			build: func() ctrl_client.Object {
				return &RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "rms-allowedns-selector-ca", Namespace: ns},
					Spec: RemoteMCPServerSpec{
						Description: "test",
						URL:         "https://upstream.example.com/mcp",
						AllowedNamespaces: &AllowedNamespaces{
							From:     NamespacesFromSelector,
							Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "x"}},
						},
						TLS: &TLSConfig{CACertSecretRef: "ca", CACertSecretKey: "ca.crt"},
					},
				}
			},
			wantReject: "spec.allowedNamespaces must not permit cross-namespace access",
		},
		{
			name: "RemoteMCPServer: allowedNamespaces from=Same with caCertSecretRef accepted",
			build: func() ctrl_client.Object {
				return &RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "rms-allowedns-same-ca", Namespace: ns},
					Spec: RemoteMCPServerSpec{
						Description:       "test",
						URL:               "https://upstream.example.com/mcp",
						AllowedNamespaces: &AllowedNamespaces{From: NamespacesFromSame},
						TLS:               &TLSConfig{CACertSecretRef: "ca", CACertSecretKey: "ca.crt"},
					},
				}
			},
		},
		{
			name: "RemoteMCPServer: allowedNamespaces from=All without CA accepted",
			build: func() ctrl_client.Object {
				return &RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "rms-allowedns-no-ca", Namespace: ns},
					Spec: RemoteMCPServerSpec{
						Description:       "test",
						URL:               "https://upstream.example.com/mcp",
						AllowedNamespaces: &AllowedNamespaces{From: NamespacesFromAll},
					},
				}
			},
		},
		{
			name: "RemoteMCPServer: caCertSecretRef without allowedNamespaces accepted",
			build: func() ctrl_client.Object {
				return &RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "rms-ca-no-allowedns", Namespace: ns},
					Spec: RemoteMCPServerSpec{
						Description: "test",
						URL:         "https://upstream.example.com/mcp",
						TLS:         &TLSConfig{CACertSecretRef: "ca", CACertSecretKey: "ca.crt"},
					},
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := cl.Create(ctx, c.build())
			if c.wantReject == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), c.wantReject)
		})
	}
}
