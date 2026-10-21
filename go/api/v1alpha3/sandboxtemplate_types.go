// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package v1alpha3

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const SandboxTemplateKind = "SandboxTemplate"

// SandboxTemplateWorkload identifies available software without selecting an agent
// or guest entrypoint. The consumer owns startup.
type SandboxTemplateWorkload struct {
	// Image is an OCI image reference pinned by sha256 digest.
	// +kubebuilder:validation:Pattern=`^[^[:space:]@]+@sha256:[a-f0-9]{64}$`
	// +required
	Image string `json:"image"`
}

// SandboxTemplateSpec defines standalone sandbox environment configuration.
type SandboxTemplateSpec struct {
	// Workload selects the immutable runtime image.
	// +required
	Workload SandboxTemplateWorkload `json:"workload"`

	// Env supplies runtime environment defaults.
	// Credential references do not grant permission to read the referenced Secret.
	// +optional
	// +kubebuilder:validation:MaxItems=100
	// +listType=map
	// +listMapKey=name
	Env []RuntimeEnvVar `json:"env,omitempty"`

	// Substrate configures compute placement and snapshot storage. References are
	// resolved in this template's namespace.
	// +required
	Substrate RuntimeSubstratePolicy `json:"substrate"`

	// EgressPolicyRefs grants additional access in this template's namespace.
	// Changes update the access of existing sandboxes without restarting them.
	// +optional
	// +kubebuilder:validation:MaxItems=16
	// +kubebuilder:validation:XValidation:rule="self.all(ref, has(ref.name) && ref.name != '')",message="egressPolicyRefs requires non-empty names"
	// +listType=map
	// +listMapKey=name
	EgressPolicyRefs []corev1.LocalObjectReference `json:"egressPolicyRefs,omitempty"`
}

// SandboxTemplateConditionEgressPoliciesApplied reports grant admission independently of Ready.
const SandboxTemplateConditionEgressPoliciesApplied = "EgressPoliciesApplied"

// SandboxTemplateStatus reports preparation of the current template inputs.
type SandboxTemplateStatus struct {
	// ObservedGeneration is the generation considered by preparation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions report runtime readiness and independent egress grant admission.
	// EgressPoliciesApplied is False for EgressPolicyNotFound, InvalidEgressPolicy,
	// or EgressGrantConflict; Applied means all grants are admitted. Grant issues
	// do not affect Ready.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=sandboxtemplates,singular=sandboxtemplate,scope=Namespaced,categories=kagent
// +kubebuilder:printcolumn:name="Image",type="string",JSONPath=".spec.workload.image"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// SandboxTemplate defines standalone sandbox configuration. Creating one does not
// allocate a user sandbox. The controller prepares its reusable runtime.
type SandboxTemplate struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Spec SandboxTemplateSpec `json:"spec"`

	// +optional
	Status SandboxTemplateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SandboxTemplateList contains SandboxTemplate resources.
type SandboxTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SandboxTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, &SandboxTemplate{}, &SandboxTemplateList{})
		return nil
	})
}
