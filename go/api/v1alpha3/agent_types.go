/*
Copyright 2026.

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

package v1alpha3

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=agents,singular=agent,categories=kagent
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Agent is a runnable definition with an explicit template and harness.
type Agent struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +required
	Spec AgentSpec `json:"spec"`
	// +optional
	Status AgentStatus `json:"status,omitempty"`
}

// AgentSpec pairs portable behavior with a platform-managed runtime. References
// are local to the Agent's namespace, including references nested in inline
// template specs.
type AgentSpec struct {
	// Template selects the portable behavior this Agent exposes.
	// +required
	Template AgentTemplateSource `json:"template"`
	// HarnessRef selects the platform-managed runtime configuration that executes
	// the behavior.
	// +kubebuilder:validation:XValidation:rule="has(self.name) && self.name != ''",message="harnessRef.name must not be empty"
	// +required
	HarnessRef corev1.LocalObjectReference `json:"harnessRef"`

	// EgressPolicyRefs grants additional access to the whole runtime, including
	// shared subagents. Policies resolve in this Agent's namespace. Integration
	// access inferred from models and tools is always included. Changes update
	// existing Sessions without a new invocation or runtime restart.
	// +optional
	// +kubebuilder:validation:MaxItems=16
	// +kubebuilder:validation:XValidation:rule="self.all(ref, has(ref.name) && ref.name != '')",message="egressPolicyRefs requires non-empty names"
	// +listType=map
	// +listMapKey=name
	EgressPolicyRefs []corev1.LocalObjectReference `json:"egressPolicyRefs,omitempty"`
}

// AgentTemplateSource selects portable behavior inline or by local reference.
// +kubebuilder:validation:ExactlyOneOf=inline;ref
type AgentTemplateSource struct {
	// Inline defines the Agent's portable behavior directly. It cannot be set
	// together with Ref.
	// +optional
	Inline *AgentTemplateSpec `json:"inline,omitempty"`
	// Ref names an AgentTemplate in the Agent's namespace. It cannot be set
	// together with Inline.
	// +kubebuilder:validation:XValidation:rule="has(self.name) && self.name != ''",message="ref.name must not be empty"
	// +optional
	Ref *corev1.LocalObjectReference `json:"ref,omitempty"`
}

const (
	AgentConditionAccepted              = "Accepted"
	AgentConditionEgressPoliciesApplied = "EgressPoliciesApplied"
	AgentConditionResolvedRefs          = "ResolvedRefs"
	AgentConditionCompatible            = "Compatible"
	AgentConditionReady                 = "Ready"
)

// AgentStatus reports compilation and preparation of an Agent.
type AgentStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +kubebuilder:validation:MinLength=1
	// +optional
	DesiredRevision string `json:"desiredRevision,omitempty"`
	// +kubebuilder:validation:MinLength=1
	// +optional
	LatestSuccessfulRevision string `json:"latestSuccessfulRevision,omitempty"`
	// WorkloadImage is the digest-pinned image selected for the desired revision.
	// Existing Sessions may still use an older revision and image.
	// +kubebuilder:validation:Pattern=`^[^[:space:]@]+@sha256:[a-f0-9]{64}$`
	// +optional
	WorkloadImage *string `json:"workloadImage,omitempty"`
	// Warnings reports non-blocking compatibility decisions made while compiling
	// this Agent.
	// +kubebuilder:validation:MaxItems=100
	// +listType=set
	// +optional
	Warnings []string `json:"warnings,omitempty"`
	// Conditions report runtime readiness and independent egress grant admission.
	// EgressPoliciesApplied is False for EgressPolicyNotFound, InvalidEgressPolicy,
	// or EgressGrantConflict; Applied means all grants are admitted. Grant issues
	// do not affect Ready.
	// +kubebuilder:validation:MaxItems=5
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true

// AgentList contains Agent resources.
type AgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Agent `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, &Agent{}, &AgentList{})
		return nil
	})
}
