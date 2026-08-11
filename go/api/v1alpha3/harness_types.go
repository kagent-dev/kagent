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

// KagentHarness selects the kagent runtime adapter.
type KagentHarness struct{}

// CodexHarness selects the Codex runtime adapter.
type CodexHarness struct{}

// ClaudeHarness selects the Claude runtime adapter.
type ClaudeHarness struct{}

// HarnessWorkload identifies the immutable runtime image used by a Harness.
type HarnessWorkload struct {
	// Image is an OCI image reference pinned by sha256 digest.
	// +kubebuilder:validation:Pattern=`^[^[:space:]@]+@sha256:[a-f0-9]{64}$`
	// +required
	Image string `json:"image"`
}

// HarnessEnvVar configures one runtime environment variable.
//
// +kubebuilder:validation:XValidation:rule="has(self.value) != has(self.credentialRef)",message="exactly one of value or credentialRef must be specified"
// +kubebuilder:validation:XValidation:rule="!has(self.credentialRef) || self.credentialRef.name.size() > 0",message="credentialRef name must not be empty"
type HarnessEnvVar struct {
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`

	// Value is a literal value, including an empty string.
	// +optional
	Value *string `json:"value,omitempty"`

	// CredentialRef references a key in a same-namespace Secret.
	// +optional
	CredentialRef *corev1.SecretKeySelector `json:"credentialRef,omitempty"`
}

// HarnessSnapshotPolicy configures storage for Substrate snapshots.
type HarnessSnapshotPolicy struct {
	// Location is the snapshot storage location used by Substrate.
	// +kubebuilder:validation:Pattern=`^[^[:space:]]+$`
	// +required
	Location string `json:"location"`
}

// HarnessSubstratePolicy contains the Substrate policy shared by all runtime variants.
//
// +kubebuilder:validation:XValidation:rule="self.workerPoolRef.name.size() > 0",message="workerPoolRef name must not be empty"
type HarnessSubstratePolicy struct {
	// WorkerPoolRef references a WorkerPool in the Harness namespace.
	// +required
	WorkerPoolRef corev1.LocalObjectReference `json:"workerPoolRef"`

	// SnapshotPolicy configures runtime snapshot storage.
	// +required
	SnapshotPolicy HarnessSnapshotPolicy `json:"snapshotPolicy"`
}

// HarnessAgentTemplateAdmission selects AgentTemplates that this Harness admits.
// An omitted admission accepts no AgentTemplates.
type HarnessAgentTemplateAdmission struct {
	// Selector selects admitted AgentTemplates in the Harness namespace.
	// +required
	Selector metav1.LabelSelector `json:"selector"`
}

// HarnessSpec defines a reusable runtime and its infrastructure policy.
//
// +kubebuilder:validation:XValidation:rule="(has(self.kagent) ? 1 : 0) + (has(self.codex) ? 1 : 0) + (has(self.claude) ? 1 : 0) == 1",message="exactly one of kagent, codex, or claude must be specified"
type HarnessSpec struct {
	// +optional
	Kagent *KagentHarness `json:"kagent,omitempty"`

	// +optional
	Codex *CodexHarness `json:"codex,omitempty"`

	// +optional
	Claude *ClaudeHarness `json:"claude,omitempty"`

	// +required
	Workload HarnessWorkload `json:"workload"`

	// +optional
	// +kubebuilder:validation:MaxItems=100
	// +listType=map
	// +listMapKey=name
	Env []HarnessEnvVar `json:"env,omitempty"`

	// +required
	Substrate HarnessSubstratePolicy `json:"substrate"`

	// AllowedAgentTemplates selects AgentTemplates this Harness admits.
	// When omitted, the Harness admits none.
	// +optional
	AllowedAgentTemplates *HarnessAgentTemplateAdmission `json:"allowedAgentTemplates,omitempty"`
}

// HarnessCapabilities records behavior proven for a pinned adapter and runtime.
// It is populated by the controller and is not user-authored configuration.
type HarnessCapabilities struct {
	// Version identifies the controller capability catalog entry.
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`

	NativeAgentTools bool `json:"nativeAgentTools"`

	// +kubebuilder:validation:Minimum=0
	MaxNativeAgentDepth int32 `json:"maxNativeAgentDepth"`

	DedicatedAgentTools bool `json:"dedicatedAgentTools"`
	MCPInjection        bool `json:"mcpInjection"`
	Streaming           bool `json:"streaming"`
	Interruption        bool `json:"interruption"`
	InputRequired       bool `json:"inputRequired"`
	Approvals           bool `json:"approvals"`

	// +kubebuilder:validation:MaxItems=16
	// +listType=set
	InputModalities []string `json:"inputModalities,omitempty"`

	// +kubebuilder:validation:MaxItems=16
	// +listType=set
	OutputModalities []string `json:"outputModalities,omitempty"`

	Resume     bool `json:"resume"`
	Checkpoint bool `json:"checkpoint"`
}

// HarnessStatus reports controller-derived capabilities and current health.
type HarnessStatus struct {
	// ObservedGeneration is the latest Harness generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Capabilities is the single capability record for the selected runtime.
	// +optional
	Capabilities *HarnessCapabilities `json:"capabilities,omitempty"`

	// Conditions report adapter and dependency health.
	// +optional
	// +kubebuilder:validation:MaxItems=8
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=harnesses,singular=harness,categories=kagent
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Harness defines a reusable agent runtime and infrastructure policy.
type Harness struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Spec HarnessSpec `json:"spec"`

	// +optional
	Status HarnessStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// HarnessList contains a list of Harness resources.
type HarnessList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Harness `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, &Harness{}, &HarnessList{})
		return nil
	})
}
