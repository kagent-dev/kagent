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

package install

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/jwk"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEnsureSubstratePrerequisites(t *testing.T) {
	client := fake.NewSimpleClientset()
	require.NoError(t, ensureSubstratePrerequisites(t.Context(), client, "kagent"))

	for _, namespace := range []string{substrateNamespace, podCertificateNamespace} {
		_, err := client.CoreV1().Namespaces().Get(t.Context(), namespace, metav1.GetOptions{})
		require.NoError(t, err)
	}
	kagentNS, err := client.CoreV1().Namespaces().Get(t.Context(), "kagent", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "true", kagentNS.Labels["ate.dev/api-access"])
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
	jwtSecret, err := client.CoreV1().Secrets(substrateNamespace).Get(t.Context(), "actor-id-jwt-pool", metav1.GetOptions{})
	require.NoError(t, err)
	assertJWTPoolPublicKeys(t, jwtSecret)

	actorCA, err := client.CoreV1().Secrets(substrateNamespace).Get(t.Context(), "actor-id-ca-pool", metav1.GetOptions{})
	require.NoError(t, err)
	actorCerts, err := client.CoreV1().Secrets(substrateNamespace).Get(t.Context(), "actor-id-ca-certs", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, actorCA.Data[corev1.TLSCertKey], actorCerts.Data["ca.crt"])

	authentication, err := client.CoreV1().ConfigMaps(substrateNamespace).Get(t.Context(), "ate-api-authentication", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, `jwtProviders:
- name: kubernetes
  issuer: https://kubernetes.default.svc
  audiences: [api.ate-system.svc]
  certificateAuthorityFile: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
  discoveryTokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token`, authentication.Data["authentication.yaml"])

	kagentNS.Labels["team"] = "existing"
	_, err = client.CoreV1().Namespaces().Update(t.Context(), kagentNS, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.NoError(t, ensureSubstratePrerequisites(t.Context(), client, "kagent"))
	kagentNS, err = client.CoreV1().Namespaces().Get(t.Context(), "kagent", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "existing", kagentNS.Labels["team"])
	require.Equal(t, "true", kagentNS.Labels["ate.dev/api-access"])
	for key, original := range originalPools {
		namespace, name, _ := strings.Cut(key, "/")
		secret, err := client.CoreV1().Secrets(namespace).Get(t.Context(), name, metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, original, secret.Data["pool"], "%s was rotated", key)
	}
}

func TestEnsureAPIAuthenticationPreservesProviders(t *testing.T) {
	const providers = `jwtProviders:
- name: custom
  issuer: https://issuer.example.com
  audiences: [custom-audience]`
	for _, obsolete := range []bool{false, true} {
		authentication := providers
		if obsolete {
			authentication = "actorIdentityJWTProvider: kubernetes\n" + providers
		}
		client := fake.NewSimpleClientset(&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: substrateNamespace, Name: "ate-api-authentication"},
			Data:       map[string]string{"authentication.yaml": authentication, "unrelated": "preserved"},
		})
		require.NoError(t, ensureAPIAuthentication(t.Context(), client))
		if !obsolete {
			require.Len(t, client.Actions(), 1, "current authentication config should only be read")
		}
		configMap, err := client.CoreV1().ConfigMaps(substrateNamespace).Get(t.Context(), "ate-api-authentication", metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, providers, configMap.Data["authentication.yaml"])
		require.Equal(t, "preserved", configMap.Data["unrelated"])
	}
}

func TestEnsureJWTPoolPreservesSigningKeys(t *testing.T) {
	for _, state := range []string{"missing", "stale", "current"} {
		t.Run(state, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			require.NoError(t, ensureJWTPool(t.Context(), client, substrateNamespace, "actor-id-jwt-pool"))
			secret, err := client.CoreV1().Secrets(substrateNamespace).Get(t.Context(), "actor-id-jwt-pool", metav1.GetOptions{})
			require.NoError(t, err)
			originalPool := bytes.Clone(secret.Data["pool"])
			secret.Data["unrelated"] = []byte("preserved")
			switch state {
			case "missing":
				delete(secret.Data, "jwks")
			case "stale":
				secret.Data["jwks"] = []byte(`{"keys":[]}`)
			}
			_, err = client.CoreV1().Secrets(substrateNamespace).Update(t.Context(), secret, metav1.UpdateOptions{})
			require.NoError(t, err)
			client.ClearActions()

			require.NoError(t, ensureJWTPool(t.Context(), client, substrateNamespace, "actor-id-jwt-pool"))
			if state == "current" {
				require.Len(t, client.Actions(), 1, "an up-to-date pool should only be read")
			}
			secret, err = client.CoreV1().Secrets(substrateNamespace).Get(t.Context(), "actor-id-jwt-pool", metav1.GetOptions{})
			require.NoError(t, err)
			require.True(t, bytes.Equal(originalPool, secret.Data["pool"]), "signing keys must not rotate")
			require.Equal(t, []byte("preserved"), secret.Data["unrelated"])
			assertJWTPoolPublicKeys(t, secret)
		})
	}
}

func TestEnsureJWTPoolRejectsMalformedPool(t *testing.T) {
	for _, pool := range []string{
		`invalid json`,
		`{}`,
		`{"Authorities":[null]}`,
		`{"Authorities":[{}]}`,
		`{"Authorities":[{"ID":"key","Algorithm":"ES256","SigningKeyPKCS8":"aW52YWxpZA=="}]}`,
	} {
		t.Run(pool, func(t *testing.T) {
			client := fake.NewSimpleClientset(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Namespace: substrateNamespace, Name: "actor-id-jwt-pool"},
				Data:       map[string][]byte{"pool": []byte(pool)},
			})
			require.Error(t, ensureJWTPool(t.Context(), client, substrateNamespace, "actor-id-jwt-pool"))
			require.Len(t, client.Actions(), 1, "an invalid pool must not be overwritten")
		})
	}
}

func assertJWTPoolPublicKeys(t *testing.T, secret *corev1.Secret) {
	t.Helper()
	var pool serializedJWTPool
	require.NoError(t, json.Unmarshal(secret.Data["pool"], &pool))
	keys, err := jwk.Parse(secret.Data["jwks"])
	require.NoError(t, err)
	require.Equal(t, len(pool.Authorities), keys.Len())
	for _, authority := range pool.Authorities {
		publicKey, ok := keys.LookupKeyID(authority.ID)
		require.True(t, ok)
		require.Equal(t, authority.Algorithm, publicKey.Algorithm())
		require.Equal(t, "sig", publicKey.KeyUsage())
		_, hasPrivateKey := publicKey.Get("d")
		require.False(t, hasPrivateKey, "JWKS must contain only public keys")
		var verificationKey any
		require.NoError(t, publicKey.Raw(&verificationKey))
		privateKey, err := x509.ParsePKCS8PrivateKey(authority.SigningKeyPKCS8)
		require.NoError(t, err)
		token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{"sub": "actor/test"})
		signed, err := token.SignedString(privateKey)
		require.NoError(t, err)
		verified, err := jwt.Parse(signed, func(*jwt.Token) (any, error) { return verificationKey, nil }, jwt.WithValidMethods([]string{"ES256"}))
		require.NoError(t, err)
		require.True(t, verified.Valid)
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
