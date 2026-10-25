package v1alpha3

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func testEgressPolicyValidation(t *testing.T, cl ctrlclient.Client, namespace string) {
	secret := corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "auth"}, Key: "token"}
	bearer := []Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: HeaderValueSource{SecretKeyRef: new(secret)}}}
	for i, tc := range []struct {
		name        string
		destination EgressDestination
		reject      bool
	}{
		{"HTTPS", EgressDestination{Origin: "https://api.example.com"}, false},
		{"HTTP", EgressDestination{Origin: "http://registry.example.com:8080"}, false},
		{"wildcard", EgressDestination{Origin: "https://*.example.com"}, false},
		{"bearer", EgressDestination{Origin: "https://api.example.com:8443", Headers: bearer}, false},
		{"API key", EgressDestination{Origin: "https://api.example.com", Headers: []Header{{Name: "X-API-Key", ValueFrom: HeaderValueSource{SecretKeyRef: new(secret)}}}}, false},
		{"empty prefix", EgressDestination{Origin: "https://api.example.com", Headers: []Header{{Name: "X-API-Key", Prefix: new(""), ValueFrom: HeaderValueSource{SecretKeyRef: new(secret)}}}}, false},
		{"custom prefix", EgressDestination{Origin: "https://api.example.com", Headers: []Header{{Name: "Authorization", Prefix: new("Token "), ValueFrom: HeaderValueSource{SecretKeyRef: new(secret)}}}}, false},
		{"HTTP auth", EgressDestination{Origin: "http://api.example.com", Headers: bearer}, false},
		{"HTTP wildcard auth", EgressDestination{Origin: "http://*.example.com", Headers: bearer}, true},
		{"wildcard auth", EgressDestination{Origin: "https://*.example.com", Headers: bearer}, true},
		{"empty auth", EgressDestination{Origin: "https://api.example.com", Headers: []Header{{}}}, true},
		{"missing header", EgressDestination{Origin: "https://api.example.com", Headers: []Header{{ValueFrom: HeaderValueSource{SecretKeyRef: new(secret)}}}}, true},
		{"path", EgressDestination{Origin: "https://api.example.com/v1"}, true},
		{"userinfo", EgressDestination{Origin: "https://user@api.example.com"}, true},
		{"query", EgressDestination{Origin: "https://api.example.com?"}, true},
		{"IP", EgressDestination{Origin: "https://192.0.2.1"}, true},
		{"all hosts", EgressDestination{Origin: "https://*"}, true},
		{"top level wildcard", EgressDestination{Origin: "https://*.com"}, true},
		{"interior wildcard", EgressDestination{Origin: "https://api.*.com"}, true},
		{"port zero", EgressDestination{Origin: "https://api.example.com:0"}, true},
		{"port overflow", EgressDestination{Origin: "https://api.example.com:65536"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := &EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: fmt.Sprintf("egress-%d", i)}, Spec: EgressPolicySpec{Destinations: []EgressDestination{tc.destination}}}
			err := cl.Create(t.Context(), policy)
			if tc.reject {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NoError(t, cl.Get(t.Context(), ctrlclient.ObjectKeyFromObject(policy), policy))
				require.Equal(t, tc.destination.Headers, policy.Spec.Destinations[0].Headers)
			}
		})
	}
	for i, header := range []string{"Host", "cOnNeCtIoN", "Content-Length", "Transfer-Encoding", "Cookie", "Proxy-Authorization", "X-Key\r\nInjected"} {
		policy := &EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: fmt.Sprintf("bad-header-%d", i)}, Spec: EgressPolicySpec{Destinations: []EgressDestination{{Origin: "https://api.example.com", Headers: []Header{{Name: header, ValueFrom: HeaderValueSource{SecretKeyRef: new(secret)}}}}}}}
		require.Error(t, cl.Create(t.Context(), policy), header)
	}
	for i, prefix := range []string{"Bearer\r", "Bearer\n", "\x00", "\x08", "\x0b", "\x1f", "\x7f", strings.Repeat("x", 257)} {
		policy := &EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: fmt.Sprintf("bad-prefix-%d", i)}, Spec: EgressPolicySpec{Destinations: []EgressDestination{{Origin: "https://api.example.com", Headers: []Header{{Name: "Authorization", Prefix: new(prefix), ValueFrom: HeaderValueSource{SecretKeyRef: new(secret)}}}}}}}
		require.Error(t, cl.Create(t.Context(), policy), "prefix %q", prefix)
	}
	for i, ref := range []corev1.SecretKeySelector{{Key: "token"}, {LocalObjectReference: corev1.LocalObjectReference{Name: "auth"}}, {LocalObjectReference: secret.LocalObjectReference, Key: "token", Optional: new(true)}} {
		policy := &EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: fmt.Sprintf("bad-secret-%d", i)}, Spec: EgressPolicySpec{Destinations: []EgressDestination{{Origin: "https://api.example.com", Headers: []Header{{Name: "Authorization", ValueFrom: HeaderValueSource{SecretKeyRef: new(ref)}}}}}}}
		require.Error(t, cl.Create(t.Context(), policy))
	}
	for _, kind := range []string{"Agent", "SandboxTemplate", "AgentTemplate", "Harness"} {
		t.Run(kind+" attachment", func(t *testing.T) {
			var object ctrlclient.Object
			switch kind {
			case "Agent":
				object = &Agent{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "egress-agent"}, Spec: AgentSpec{Template: AgentTemplateSource{Ref: &corev1.LocalObjectReference{Name: "behavior"}}, HarnessRef: corev1.LocalObjectReference{Name: "runner"}}}
			case "SandboxTemplate":
				object = sandboxTemplateForValidation(namespace, "egress-sandbox", nil)
			case "AgentTemplate":
				object = validAgentTemplate(namespace, "egress-template", nil)
			case "Harness":
				object = validHarness(namespace, "egress-harness", HarnessSpec{Kagent: &KagentHarness{}})
			}
			data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(object)
			require.NoError(t, err)
			resource := &unstructured.Unstructured{Object: data}
			resource.SetAPIVersion(GroupVersion.String())
			resource.SetKind(kind)
			require.NoError(t, unstructured.SetNestedSlice(data, []any{map[string]any{"name": "tickets"}}, "spec", "egressPolicyRefs"))
			err = cl.Create(t.Context(), resource, &ctrlclient.CreateOptions{FieldValidation: metav1.FieldValidationStrict})
			if kind == "AgentTemplate" || kind == "Harness" {
				require.ErrorContains(t, err, "unknown field")
				return
			}
			require.NoError(t, err)
			require.NoError(t, cl.Get(t.Context(), ctrlclient.ObjectKeyFromObject(resource), resource))
			refs, _, err := unstructured.NestedSlice(resource.Object, "spec", "egressPolicyRefs")
			require.NoError(t, err)
			require.Equal(t, []any{map[string]any{"name": "tickets"}}, refs)
			require.NoError(t, unstructured.SetNestedSlice(resource.Object, []any{map[string]any{"name": ""}}, "spec", "egressPolicyRefs"))
			require.Error(t, cl.Update(t.Context(), resource))
		})
	}
}
