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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAllowedNamespaces_AllowsNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	// Create test namespaces
	namespaces := []runtime.Object{
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "target-ns",
			},
		},
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "source-ns",
			},
		},
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "labeled-ns",
				Labels: map[string]string{
					"shared-access": "true",
					"team":          "platform",
				},
			},
		},
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "empty-labels-ns",
				Labels: map[string]string{},
			},
		},
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "restricted-ns",
				Labels: map[string]string{
					"restricted": "true",
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(namespaces...).
		Build()

	ctx := context.Background()

	tests := []struct {
		name            string
		allowedNs       *AllowedNamespaces
		sourceNamespace string
		targetNamespace string
		wantAllowed     bool
		wantErr         bool
		errContains     string
	}{
		{
			name:            "nil AllowedNamespaces defaults to same namespace only - same ns",
			allowedNs:       nil,
			sourceNamespace: "target-ns",
			targetNamespace: "target-ns",
			wantAllowed:     true,
		},
		{
			name:            "nil AllowedNamespaces defaults to same namespace only - different ns",
			allowedNs:       nil,
			sourceNamespace: "source-ns",
			targetNamespace: "target-ns",
			wantAllowed:     false,
		},
		{
			name: "From=Same allows same namespace",
			allowedNs: &AllowedNamespaces{
				From: NamespacesFromSame,
			},
			sourceNamespace: "target-ns",
			targetNamespace: "target-ns",
			wantAllowed:     true,
		},
		{
			name: "From=Same denies different namespace",
			allowedNs: &AllowedNamespaces{
				From: NamespacesFromSame,
			},
			sourceNamespace: "source-ns",
			targetNamespace: "target-ns",
			wantAllowed:     false,
		},
		{
			name: "Empty From defaults to Same - same ns",
			allowedNs: &AllowedNamespaces{
				From: "",
			},
			sourceNamespace: "target-ns",
			targetNamespace: "target-ns",
			wantAllowed:     true,
		},
		{
			name: "Empty From defaults to Same - different ns",
			allowedNs: &AllowedNamespaces{
				From: "",
			},
			sourceNamespace: "source-ns",
			targetNamespace: "target-ns",
			wantAllowed:     false,
		},
		{
			name: "From=All allows any namespace",
			allowedNs: &AllowedNamespaces{
				From: NamespacesFromAll,
			},
			sourceNamespace: "source-ns",
			targetNamespace: "target-ns",
			wantAllowed:     true,
		},
		{
			name: "From=All allows same namespace",
			allowedNs: &AllowedNamespaces{
				From: NamespacesFromAll,
			},
			sourceNamespace: "target-ns",
			targetNamespace: "target-ns",
			wantAllowed:     true,
		},
		{
			name: "From=Selector allows namespace with matching label",
			allowedNs: &AllowedNamespaces{
				From: NamespacesFromSelector,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"shared-access": "true",
					},
				},
			},
			sourceNamespace: "labeled-ns",
			targetNamespace: "target-ns",
			wantAllowed:     true,
		},
		{
			name: "From=Selector denies namespace without matching label",
			allowedNs: &AllowedNamespaces{
				From: NamespacesFromSelector,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"shared-access": "true",
					},
				},
			},
			sourceNamespace: "source-ns",
			targetNamespace: "target-ns",
			wantAllowed:     false,
		},
		{
			name: "From=Selector with multiple labels - all match",
			allowedNs: &AllowedNamespaces{
				From: NamespacesFromSelector,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"shared-access": "true",
						"team":          "platform",
					},
				},
			},
			sourceNamespace: "labeled-ns",
			targetNamespace: "target-ns",
			wantAllowed:     true,
		},
		{
			name: "From=Selector with multiple labels - partial match fails",
			allowedNs: &AllowedNamespaces{
				From: NamespacesFromSelector,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"shared-access": "true",
						"team":          "other-team",
					},
				},
			},
			sourceNamespace: "labeled-ns",
			targetNamespace: "target-ns",
			wantAllowed:     false,
		},
		{
			name: "From=Selector with MatchExpressions - In operator",
			allowedNs: &AllowedNamespaces{
				From: NamespacesFromSelector,
				Selector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "team",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"platform", "infra"},
						},
					},
				},
			},
			sourceNamespace: "labeled-ns",
			targetNamespace: "target-ns",
			wantAllowed:     true,
		},
		{
			name: "From=Selector with MatchExpressions - Exists operator",
			allowedNs: &AllowedNamespaces{
				From: NamespacesFromSelector,
				Selector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "shared-access",
							Operator: metav1.LabelSelectorOpExists,
						},
					},
				},
			},
			sourceNamespace: "labeled-ns",
			targetNamespace: "target-ns",
			wantAllowed:     true,
		},
		{
			name: "From=Selector with empty selector matches all namespaces",
			allowedNs: &AllowedNamespaces{
				From:     NamespacesFromSelector,
				Selector: &metav1.LabelSelector{},
			},
			sourceNamespace: "source-ns",
			targetNamespace: "target-ns",
			wantAllowed:     true,
		},
		{
			name: "From=Selector with MatchExpressions - DoesNotExist operator allows unrestricted",
			allowedNs: &AllowedNamespaces{
				From: NamespacesFromSelector,
				Selector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "restricted",
							Operator: metav1.LabelSelectorOpDoesNotExist,
						},
					},
				},
			},
			sourceNamespace: "source-ns",
			targetNamespace: "target-ns",
			wantAllowed:     true,
		},
		{
			name: "From=Selector with MatchExpressions - DoesNotExist operator denies restricted",
			allowedNs: &AllowedNamespaces{
				From: NamespacesFromSelector,
				Selector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "restricted",
							Operator: metav1.LabelSelectorOpDoesNotExist,
						},
					},
				},
			},
			sourceNamespace: "restricted-ns",
			targetNamespace: "target-ns",
			wantAllowed:     false,
		},
		{
			name: "From=Selector without selector returns error",
			allowedNs: &AllowedNamespaces{
				From:     NamespacesFromSelector,
				Selector: nil,
			},
			sourceNamespace: "source-ns",
			targetNamespace: "target-ns",
			wantErr:         true,
			errContains:     "selector must be specified",
		},
		{
			name: "From=Selector with non-existent namespace returns error",
			allowedNs: &AllowedNamespaces{
				From: NamespacesFromSelector,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"shared-access": "true",
					},
				},
			},
			sourceNamespace: "non-existent-ns",
			targetNamespace: "target-ns",
			wantErr:         true,
			errContains:     "failed to get namespace",
		},
		{
			name: "Unknown From value returns error",
			allowedNs: &AllowedNamespaces{
				From: "Unknown",
			},
			sourceNamespace: "source-ns",
			targetNamespace: "target-ns",
			wantErr:         true,
			errContains:     "unknown from value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed, err := tt.allowedNs.AllowsNamespace(ctx, fakeClient, tt.sourceNamespace, tt.targetNamespace)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantAllowed, allowed)
		})
	}
}
