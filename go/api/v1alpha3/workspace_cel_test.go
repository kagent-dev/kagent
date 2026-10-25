// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package v1alpha3

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func testWorkspaceValidation(t *testing.T, cl ctrlclient.Client, namespace string) {
	t.Helper()
	for _, resource := range []struct {
		kind   string
		object ctrlclient.Object
	}{
		{"Agent", &Agent{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "workspace-agent"}, Spec: AgentSpec{Template: AgentTemplateSource{Ref: &corev1.LocalObjectReference{Name: "behavior"}}, HarnessRef: corev1.LocalObjectReference{Name: "runtime"}}}},
		{"SandboxTemplate", sandboxTemplateForValidation(namespace, "workspace-sandbox", nil)},
	} {
		for _, test := range []struct {
			name      string
			workspace map[string]any
			invalid   bool
		}{
			{"omitted", nil, false},
			{"empty", map[string]any{}, false},
			{"git", map[string]any{"git": map[string]any{"url": "https://github.com/org/repo.git", "ref": "main", "credentialRef": map[string]any{"name": "git-auth", "key": "authorization"}}}, false},
			{"missing-ref", map[string]any{"git": map[string]any{"url": "https://github.com/org/repo.git"}}, true},
			{"http", map[string]any{"git": map[string]any{"url": "http://github.com/org/repo.git", "ref": "main"}}, true},
			{"credential-url", map[string]any{"git": map[string]any{"url": "https://token@github.com/org/repo.git", "ref": "main"}}, true},
			{"ref-option", map[string]any{"git": map[string]any{"url": "https://github.com/org/repo.git", "ref": "--option"}}, true},
			{"optional-secret", map[string]any{"git": map[string]any{"url": "https://github.com/org/repo.git", "ref": "main", "credentialRef": map[string]any{"name": "auth", "key": "authorization", "optional": true}}}, true},
		} {
			t.Run(resource.kind+"/"+test.name, func(t *testing.T) {
				data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(resource.object)
				require.NoError(t, err)
				object := &unstructured.Unstructured{Object: data}
				object.SetAPIVersion(GroupVersion.String())
				object.SetKind(resource.kind)
				object.SetName(object.GetName() + "-" + test.name)
				if test.workspace != nil {
					require.NoError(t, unstructured.SetNestedMap(object.Object, test.workspace, "spec", "workspace"))
				}
				err = cl.Create(t.Context(), object, &ctrlclient.CreateOptions{FieldValidation: metav1.FieldValidationStrict})
				if test.invalid {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				require.NoError(t, cl.Get(t.Context(), ctrlclient.ObjectKeyFromObject(object), object))
				value, found, err := unstructured.NestedMap(object.Object, "spec", "workspace")
				require.NoError(t, err)
				require.Equal(t, test.workspace != nil, found)
				require.Equal(t, test.workspace, value)
			})
		}
	}

	for _, resource := range []struct {
		kind   string
		object ctrlclient.Object
		path   []string
	}{
		{"AgentTemplate", validAgentTemplate(namespace, "workspace-template-rejected", nil), []string{"spec", "workspace"}},
		{"Agent", &Agent{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "workspace-inline-rejected"}, Spec: AgentSpec{Template: AgentTemplateSource{Inline: validAgentTemplate(namespace, "behavior", nil).Spec.DeepCopy()}, HarnessRef: corev1.LocalObjectReference{Name: "runtime"}}}, []string{"spec", "template", "inline", "workspace"}},
	} {
		t.Run(resource.kind+" rejects template workspace", func(t *testing.T) {
			data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(resource.object)
			require.NoError(t, err)
			object := &unstructured.Unstructured{Object: data}
			object.SetAPIVersion(GroupVersion.String())
			object.SetKind(resource.kind)
			require.NoError(t, unstructured.SetNestedMap(object.Object, map[string]any{"git": map[string]any{"url": "https://github.com/org/repo.git", "ref": "main"}}, resource.path...))
			err = cl.Create(t.Context(), object, &ctrlclient.CreateOptions{FieldValidation: metav1.FieldValidationStrict})
			require.ErrorContains(t, err, "unknown field")
			require.ErrorContains(t, err, "workspace")
		})
	}
}
