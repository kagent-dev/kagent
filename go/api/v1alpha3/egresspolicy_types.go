// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package v1alpha3

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EgressPolicySpec grants additional HTTP(S) access to workloads in its namespace.
type EgressPolicySpec struct {
	// Destinations are additional origins the workload may reach.
	// +required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	// +listType=map
	// +listMapKey=origin
	Destinations []EgressDestination `json:"destinations"`
}

// EgressDestination grants access to one HTTP(S) origin, including all its paths.
// +kubebuilder:validation:XValidation:rule="!has(self.headers) || !self.origin.contains('*')",message="credential headers require an exact HTTP(S) origin"
type EgressDestination struct {
	// Origin is a scheme, DNS hostname, and optional port. A wildcard replaces
	// exactly one leftmost DNS label, for example https://*.example.com.
	// +required
	// +kubebuilder:validation:MaxLength=270
	// +kubebuilder:validation:Pattern=`^https?://(\*\.([a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?\.)+|([a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?\.)*)[a-z]([-a-z0-9]{0,61}[a-z0-9])?(:([1-9][0-9]{0,3}|[1-5][0-9]{4}|6[0-4][0-9]{3}|65[0-4][0-9]{2}|655[0-2][0-9]|6553[0-5]))?$`
	Origin string `json:"origin"`
	// Headers sets credential headers for the declared endpoint.
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:XValidation:rule="self.all(a, self.filter(b, a.name.lowerAscii() == b.name.lowerAscii()).size() == 1)",message="credential header names must be unique ignoring case"
	Headers []Header `json:"headers,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=egresspolicies,singular=egresspolicy,scope=Namespaced,categories=kagent

// EgressPolicy is a reusable grant of additional network access. Workloads in its
// namespace select it explicitly; changes update their existing runtime Actors.
type EgressPolicy struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +required
	Spec EgressPolicySpec `json:"spec"`
}

// +kubebuilder:object:root=true

// EgressPolicyList contains EgressPolicy resources.
type EgressPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EgressPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, &EgressPolicy{}, &EgressPolicyList{})
		return nil
	})
}

// Reasons for the independent EgressPoliciesApplied condition on Agents and
// SandboxTemplates. Grant resolution and admission never block runtime readiness.
const (
	EgressPoliciesReasonApplied             = "Applied"
	EgressPoliciesReasonNotFound            = "EgressPolicyNotFound"
	EgressPoliciesReasonInvalidEgressPolicy = "InvalidEgressPolicy"
	EgressPoliciesReasonConflict            = "EgressGrantConflict"
)
