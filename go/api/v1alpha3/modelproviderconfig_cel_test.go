package v1alpha3

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func testModelDiscoveryValidation(t *testing.T, cl ctrlclient.Client, namespace string) {
	secret := corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "invoke"}, Key: "token"}
	discoverySecret := corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "discover"}, Key: "api-key"}
	jwt := &ActorJWT{Audiences: []string{"https://gateway.example.com"}}
	for _, tc := range []struct {
		name      string
		auth      *ModelInvocationAuth
		discovery *ModelDiscoveryAuth
		policy    *AllowedNamespaces
		invalid   bool
	}{
		{name: "omitted"},
		{name: "actor", auth: &ModelInvocationAuth{ActorJWT: jwt}},
		{name: "key", auth: &ModelInvocationAuth{SecretRef: &secret}, discovery: &ModelDiscoveryAuth{SecretRef: discoverySecret}},
		{name: "passthrough", auth: &ModelInvocationAuth{TokenPassthrough: new(true)}},
		{name: "all", policy: &AllowedNamespaces{From: NamespacesFromAll}},
		{name: "selector", policy: &AllowedNamespaces{From: NamespacesFromSelector, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "a"}}}},
		{name: "actor-key", auth: &ModelInvocationAuth{ActorJWT: jwt, SecretRef: &secret}, invalid: true},
		{name: "actor-passthrough", auth: &ModelInvocationAuth{ActorJWT: jwt, TokenPassthrough: new(true)}, invalid: true},
		{name: "key-passthrough", auth: &ModelInvocationAuth{SecretRef: &secret, TokenPassthrough: new(true)}, invalid: true},
		{name: "invocation-empty-reference", auth: &ModelInvocationAuth{SecretRef: &corev1.SecretKeySelector{}}, invalid: true},
		{name: "invocation-empty-name", auth: &ModelInvocationAuth{SecretRef: &corev1.SecretKeySelector{Key: "token"}}, invalid: true},
		{name: "invocation-empty-key", auth: &ModelInvocationAuth{SecretRef: &corev1.SecretKeySelector{LocalObjectReference: secret.LocalObjectReference}}, invalid: true},
		{name: "invocation-optional", auth: &ModelInvocationAuth{SecretRef: &corev1.SecretKeySelector{LocalObjectReference: secret.LocalObjectReference, Key: secret.Key, Optional: new(true)}}, invalid: true},
		{name: "invocation-required", auth: &ModelInvocationAuth{SecretRef: &corev1.SecretKeySelector{LocalObjectReference: secret.LocalObjectReference, Key: secret.Key, Optional: new(false)}}},
		{name: "empty-discovery", discovery: &ModelDiscoveryAuth{}, invalid: true},
		{name: "discovery-empty-name", discovery: &ModelDiscoveryAuth{SecretRef: corev1.SecretKeySelector{Key: "token"}}, invalid: true},
		{name: "discovery-empty-key", discovery: &ModelDiscoveryAuth{SecretRef: corev1.SecretKeySelector{LocalObjectReference: discoverySecret.LocalObjectReference}}, invalid: true},
		{name: "discovery-optional", discovery: &ModelDiscoveryAuth{SecretRef: corev1.SecretKeySelector{LocalObjectReference: discoverySecret.LocalObjectReference, Key: discoverySecret.Key, Optional: new(true)}}, invalid: true},
		{name: "discovery-required", discovery: &ModelDiscoveryAuth{SecretRef: corev1.SecretKeySelector{LocalObjectReference: discoverySecret.LocalObjectReference, Key: discoverySecret.Key, Optional: new(false)}}},
		{name: "empty-audiences", auth: &ModelInvocationAuth{ActorJWT: &ActorJWT{}}, invalid: true},
		{name: "missing-selector", policy: &AllowedNamespaces{From: NamespacesFromSelector}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			object := &ModelProviderConfig{ObjectMeta: metav1.ObjectMeta{GenerateName: "discovery-", Namespace: namespace}, Spec: ModelProviderConfigSpec{Type: ModelProviderOpenAI, Endpoint: "https://gateway.example.com/v1", DiscoveryAuth: tc.discovery, ModelConfigTemplate: &DiscoveredModelConfigTemplate{Auth: tc.auth, AllowedNamespaces: tc.policy}}}
			err := cl.Create(t.Context(), object)
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			stored := &ModelProviderConfig{}
			require.NoError(t, cl.Get(t.Context(), ctrlclient.ObjectKeyFromObject(object), stored))
			require.Equal(t, object.Spec, stored.Spec)
		})
	}
}
