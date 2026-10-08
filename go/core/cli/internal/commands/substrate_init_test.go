// Copyright 2026 The Kagent Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package commands

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/stretchr/testify/require"
)

func TestEnsureSubstratePrerequisites(t *testing.T) {
	client := fake.NewSimpleClientset()
	require.NoError(t, ensureSubstratePrerequisites(t.Context(), client))

	for _, namespace := range []string{substrateNamespace, podCertificateNamespace} {
		_, err := client.CoreV1().Namespaces().Get(t.Context(), namespace, metav1.GetOptions{})
		require.NoError(t, err)
	}
	podCertificateNS, err := client.CoreV1().Namespaces().Get(t.Context(), podCertificateNamespace, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "Helm", podCertificateNS.Labels["app.kubernetes.io/managed-by"])
	require.Equal(t, podCertificateRelease, podCertificateNS.Annotations["meta.helm.sh/release-name"])
	require.Equal(t, podCertificateNamespace, podCertificateNS.Annotations["meta.helm.sh/release-namespace"])

	secretNames := map[string][]string{
		substrateNamespace: {
			"actor-id-jwt-pool",
			"actor-id-ca-pool",
			"actor-id-ca-certs",
			"egress-mitm-ca-pool",
		},
		podCertificateNamespace: {
			"service-dns-ca-pool",
			"pod-identity-ca-pool",
			"postgres-ca-pool",
		},
	}
	originalPools := map[string][]byte{}
	for namespace, names := range secretNames {
		for _, name := range names {
			secret, err := client.CoreV1().Secrets(namespace).Get(t.Context(), name, metav1.GetOptions{})
			require.NoError(t, err)
			require.NotEmpty(t, secret.Data)
			if pool := secret.Data["pool"]; len(pool) != 0 {
				originalPools[namespace+"/"+name] = bytes.Clone(pool)
			}
		}
	}

	assertCAKeyType(t, client, substrateNamespace, "actor-id-ca-pool", new(ed25519.PrivateKey))
	assertCAKeyType(t, client, substrateNamespace, "egress-mitm-ca-pool", new(*ecdsa.PrivateKey))

	actorCA, err := client.CoreV1().Secrets(substrateNamespace).Get(t.Context(), "actor-id-ca-pool", metav1.GetOptions{})
	require.NoError(t, err)
	actorCerts, err := client.CoreV1().Secrets(substrateNamespace).Get(t.Context(), "actor-id-ca-certs", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, actorCA.Data[corev1.TLSCertKey], actorCerts.Data["ca.crt"])

	authentication, err := client.CoreV1().ConfigMaps(substrateNamespace).Get(t.Context(), "ate-api-authentication", metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, authentication.Data["authentication.yaml"], "issuer: https://kubernetes.default.svc")
	require.Contains(t, authentication.Data["authentication.yaml"], "audiences: [api.ate-system.svc]")

	require.NoError(t, ensureSubstratePrerequisites(t.Context(), client))
	for key, original := range originalPools {
		namespace, name, _ := strings.Cut(key, "/")
		secret, err := client.CoreV1().Secrets(namespace).Get(t.Context(), name, metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, original, secret.Data["pool"], "%s was rotated", key)
	}
}

func assertCAKeyType(t *testing.T, client *fake.Clientset, namespace, name string, expected any) {
	t.Helper()
	secret, err := client.CoreV1().Secrets(namespace).Get(t.Context(), name, metav1.GetOptions{})
	require.NoError(t, err)
	var pool serializedCAPool
	require.NoError(t, json.Unmarshal(secret.Data["pool"], &pool))
	require.Len(t, pool.CAs, 1)
	key, err := x509.ParsePKCS8PrivateKey(pool.CAs[0].SigningKeyPKCS8)
	require.NoError(t, err)
	switch expected.(type) {
	case *ed25519.PrivateKey:
		_, ok := key.(ed25519.PrivateKey)
		require.True(t, ok)
	case **ecdsa.PrivateKey:
		_, ok := key.(*ecdsa.PrivateKey)
		require.True(t, ok)
	default:
		t.Fatalf("unsupported expected key type %T", expected)
	}
}
