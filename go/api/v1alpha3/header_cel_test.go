// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package v1alpha3

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func testHeaderValidation(t *testing.T, cl ctrlclient.Client, namespace string) {
	secret := &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "auth"}, Key: "token"}
	for _, tc := range []struct {
		name    string
		headers []Header
		valid   bool
	}{
		{"secret", []Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: HeaderValueSource{SecretKeyRef: secret}}}, true},
		{"passthrough", []Header{{Name: "Authorization", ValueFrom: HeaderValueSource{TokenPassthrough: &TokenPassthrough{}}}}, true},
		{"multiple", []Header{{Name: "Authorization", ValueFrom: HeaderValueSource{TokenPassthrough: &TokenPassthrough{}}}, {Name: "X-Tenant-Key", ValueFrom: HeaderValueSource{SecretKeyRef: secret}}}, true},
		{"omitted", nil, true},
		{"missing source", []Header{{Name: "Authorization"}}, false},
		{"both sources", []Header{{Name: "Authorization", ValueFrom: HeaderValueSource{SecretKeyRef: secret, TokenPassthrough: &TokenPassthrough{}}}}, false},
		{"case duplicate", []Header{{Name: "Authorization", ValueFrom: HeaderValueSource{SecretKeyRef: secret}}, {Name: "authorization", ValueFrom: HeaderValueSource{TokenPassthrough: &TokenPassthrough{}}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metadata := metav1.ObjectMeta{Namespace: namespace, GenerateName: "credential-headers-"}
			for _, object := range []ctrlclient.Object{
				&ModelConfig{ObjectMeta: metadata, Spec: ModelConfigSpec{Model: "gpt", Provider: ModelProviderOpenAI, Headers: tc.headers}},
				&RemoteMCPServer{ObjectMeta: metadata, Spec: RemoteMCPServerSpec{URL: "https://mcp.example.com", Headers: tc.headers}},
				&EgressPolicy{ObjectMeta: metadata, Spec: EgressPolicySpec{Destinations: []EgressDestination{{Origin: "https://api.example.com", Headers: tc.headers}}}},
			} {
				err := cl.Create(t.Context(), object)
				if tc.valid {
					require.NoError(t, err, "%T", object)
				} else {
					require.Error(t, err, "%T", object)
				}
			}
		})
	}

	for _, tc := range []struct {
		name    string
		headers []any
	}{
		{"empty list", []any{}},
		{"missing valueFrom", []any{map[string]any{"name": "Authorization"}}},
		{"literal value", []any{map[string]any{"name": "Authorization", "value": "literal"}}},
		{"literal source", []any{map[string]any{"name": "Authorization", "valueFrom": map[string]any{"value": "literal"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, kind := range []string{"ModelConfig", "RemoteMCPServer", "EgressPolicy"} {
				spec := map[string]any{"headers": tc.headers}
				switch kind {
				case "ModelConfig":
					spec["model"], spec["provider"] = "gpt", "OpenAI"
				case "RemoteMCPServer":
					spec["url"], spec["description"] = "https://mcp.example.com", "Test MCP server"
				case "EgressPolicy":
					spec = map[string]any{"destinations": []any{map[string]any{"origin": "https://api.example.com", "headers": tc.headers}}}
				}
				object := &unstructured.Unstructured{Object: map[string]any{
					"apiVersion": "api.kagent.dev/v1alpha3", "kind": kind,
					"metadata": map[string]any{"namespace": namespace, "generateName": "invalid-headers-"}, "spec": spec,
				}}
				err := cl.Create(t.Context(), object)
				require.ErrorContains(t, err, "headers", "%s must reject %s", kind, tc.name)
			}
		})
	}
}
