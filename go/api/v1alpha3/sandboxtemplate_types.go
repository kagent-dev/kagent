// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package v1alpha3

import (
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

	// Env supplies environment defaults using the existing Harness value contract.
	// Credential references do not grant permission to read the referenced Secret.
	// +optional
	// +kubebuilder:validation:MaxItems=100
	// +listType=map
	// +listMapKey=name
	Env []HarnessEnvVar `json:"env,omitempty"`

	// Substrate configures compute placement and snapshot storage. References are
	// resolved in this template's namespace.
	// +required
	Substrate HarnessSubstratePolicy `json:"substrate"`
}

// SandboxTemplateStatus reports preparation of the current template inputs.
type SandboxTemplateStatus struct {
	// ObservedGeneration is the generation considered by preparation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions describe whether a prepared revision is available.
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
