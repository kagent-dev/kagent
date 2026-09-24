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

// KagentHarness configures the kagent runtime adapter.
type KagentHarness struct {
	// Memory enables long-term memory for agents using this Harness.
	// +optional
	Memory *KagentHarnessMemory `json:"memory,omitempty"`

	// Compaction summarizes older session events so the prompt stays small as
	// a conversation grows. Omitted leaves the history uncompacted.
	// +optional
	Compaction *KagentHarnessCompaction `json:"compaction,omitempty"`
}

// KagentHarnessCompaction selects the compaction strategies and the model that
// writes the summaries. The sliding window (compactionInterval, overlapSize)
// summarizes each group of completed invocations; tail retention
// (tokenThreshold, eventRetentionSize) bounds the prompt by summarizing
// everything but the most recent events once the prompt grows past a token
// count. At least one strategy must be configured.
// +kubebuilder:validation:XValidation:rule="has(self.compactionInterval) || has(self.tokenThreshold)",message="compactionInterval or tokenThreshold must be specified"
// +kubebuilder:validation:XValidation:rule="has(self.tokenThreshold) == has(self.eventRetentionSize)",message="tokenThreshold and eventRetentionSize must be specified together"
// +kubebuilder:validation:XValidation:rule="!has(self.overlapSize) || has(self.compactionInterval)",message="overlapSize requires compactionInterval"
type KagentHarnessCompaction struct {
	// CompactionInterval is the number of new user-initiated invocations that,
	// once fully represented in the session, triggers a sliding-window
	// compaction of those invocations.
	// +kubebuilder:validation:Minimum=1
	// +optional
	CompactionInterval *int `json:"compactionInterval,omitempty"`
	// OverlapSize is the number of already-compacted invocations pulled back
	// into the next sliding window so consecutive summaries overlap.
	// +kubebuilder:validation:Minimum=0
	// +optional
	OverlapSize *int `json:"overlapSize,omitempty"`
	// TokenThreshold is the prompt token count at which tail-retention
	// compaction summarizes the history before the next model call.
	// +kubebuilder:validation:Minimum=1
	// +optional
	TokenThreshold *int `json:"tokenThreshold,omitempty"`
	// EventRetentionSize is the number of most recent events that tail
	// retention keeps uncompacted.
	// +kubebuilder:validation:Minimum=1
	// +optional
	EventRetentionSize *int `json:"eventRetentionSize,omitempty"`
	// Summarizer selects the model and prompt that write the summaries.
	// Omitted summarizes with the agent's own model and the runtime's default
	// prompt.
	// +optional
	Summarizer *KagentHarnessSummarizer `json:"summarizer,omitempty"`
}

// KagentHarnessSummarizer configures the model that summarizes compacted events.
// +kubebuilder:validation:XValidation:rule="!has(self.promptTemplate) || self.promptTemplate.contains('{conversation_history}')",message="promptTemplate must contain {conversation_history}"
type KagentHarnessSummarizer struct {
	// ModelConfigRef references the ModelConfig in the Harness namespace that
	// writes the summaries. Omitted uses the agent's own model.
	// +kubebuilder:validation:XValidation:rule="has(self.name) && self.name != ''",message="name must not be empty"
	// +optional
	ModelConfigRef *corev1.LocalObjectReference `json:"modelConfigRef,omitempty"`
	// PromptTemplate replaces the runtime's default summarization prompt. It
	// must contain {conversation_history}, which the runtime replaces with the
	// rendered events.
	// +kubebuilder:validation:MinLength=1
	// +optional
	PromptTemplate string `json:"promptTemplate,omitempty"`
}

// KagentHarnessMemory configures kagent's long-term memory service.
// +kubebuilder:validation:XValidation:rule="self.modelConfigRef.name.size() > 0",message="modelConfigRef name must not be empty"
type KagentHarnessMemory struct {
	// ModelConfigRef references the embedding ModelConfig in the Harness namespace.
	// +required
	ModelConfigRef corev1.LocalObjectReference `json:"modelConfigRef"`

	// TTLDays controls how many days a stored memory entry remains valid.
	// +kubebuilder:validation:Minimum=1
	// +optional
	TTLDays int `json:"ttlDays,omitempty"`
}

// CodexHarness selects the Codex runtime adapter.
type CodexHarness struct{}

// ClaudeHarness selects the Claude runtime adapter.
type ClaudeHarness struct{}

// BYOHarness selects an image that implements kagent's private A2A contract.
type BYOHarness struct {
	// Port is the TCP port the image serves its A2A endpoint on. The
	// compiler advertises it in the agent card and injects it as the PORT
	// environment variable so the card and the listener always agree.
	// Omitted defaults to 80.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port *int32 `json:"port,omitempty"`
}

// HarnessWorkload identifies the immutable runtime image used by a Harness.
type HarnessWorkload struct {
	// Image is an OCI image reference pinned by sha256 digest.
	// +kubebuilder:validation:Pattern=`^[^[:space:]@]+@sha256:[a-f0-9]{64}$`
	// +required
	Image string `json:"image"`

	// Command overrides the image entrypoint when set.
	// +kubebuilder:validation:MaxItems=32
	// +optional
	Command []string `json:"command,omitempty"`

	// Args overrides the image command arguments when set.
	// +kubebuilder:validation:MaxItems=64
	// +optional
	Args []string `json:"args,omitempty"`
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
// +kubebuilder:validation:XValidation:rule="(has(self.kagent) ? 1 : 0) + (has(self.codex) ? 1 : 0) + (has(self.claude) ? 1 : 0) + (has(self.byo) ? 1 : 0) == 1",message="exactly one of kagent, codex, claude, or byo must be specified"
// +kubebuilder:validation:XValidation:rule="!has(self.byo) || size(self.workload.command) > 0",message="BYO harnesses must specify workload.command"
type HarnessSpec struct {
	// +optional
	Kagent *KagentHarness `json:"kagent,omitempty"`

	// +optional
	Codex *CodexHarness `json:"codex,omitempty"`

	// +optional
	Claude *ClaudeHarness `json:"claude,omitempty"`

	// +optional
	BYO *BYOHarness `json:"byo,omitempty"`

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
	// +required
	Version string `json:"version"`

	// +required
	NativeAgentTools bool `json:"nativeAgentTools"`

	// +kubebuilder:validation:Minimum=0
	// +required
	MaxNativeAgentDepth int32 `json:"maxNativeAgentDepth"`

	// +required
	DedicatedAgentTools bool `json:"dedicatedAgentTools"`
	// +required
	MCPInjection bool `json:"mcpInjection"`
	// +required
	Streaming bool `json:"streaming"`
	// +required
	Interruption bool `json:"interruption"`
	// +required
	InputRequired bool `json:"inputRequired"`
	// +required
	Approvals bool `json:"approvals"`
	// StructuredOutput reports whether the pinned adapter can enforce a root
	// JSON output contract and emit it as an A2A DataPart.
	// +required
	StructuredOutput bool `json:"structuredOutput"`

	// +kubebuilder:validation:MaxItems=16
	// +listType=set
	// +optional
	InputModalities []string `json:"inputModalities,omitempty"`

	// +kubebuilder:validation:MaxItems=16
	// +listType=set
	// +optional
	OutputModalities []string `json:"outputModalities,omitempty"`

	// +required
	Resume bool `json:"resume"`
	// +required
	Checkpoint bool `json:"checkpoint"`
}

// HarnessConditionType enumerates the condition types a Harness may report.
const (
	HarnessConditionTypeReady = "Ready"
)

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

// +genclient
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
