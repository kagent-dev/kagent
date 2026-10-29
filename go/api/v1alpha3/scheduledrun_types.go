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

const (
	ScheduledRunKind           = "ScheduledRun"
	ScheduledRunConditionReady = "Ready"
)

// ScheduledRunSpec defines unattended invocations of an Agent. Accepted
// executions retain their inputs when the resource changes or is deleted.
type ScheduledRunSpec struct {
	// AgentRef names the Agent to invoke in the ScheduledRun's namespace.
	// +required
	// +kubebuilder:validation:XValidation:rule="has(self.name) && self.name.size() > 0",message="agentRef.name is required"
	AgentRef corev1.LocalObjectReference `json:"agentRef"`
	// Schedule is a five-field cron expression.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:Pattern=`^\S+\s+\S+\s+\S+\s+\S+\s+\S+$`
	Schedule string `json:"schedule"`
	// TimeZone is an IANA time zone. Local is not supported.
	// +optional
	// +kubebuilder:default=UTC
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:XValidation:rule="self != 'Local'",message="timeZone must not depend on the controller host"
	TimeZone *string `json:"timeZone,omitempty"`
	// Prompt is the message submitted for each occurrence.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=32768
	// +kubebuilder:validation:Pattern=`\S`
	Prompt string `json:"prompt"`
	// Paused stops new cron reservations. Manual triggers remain available.
	// +optional
	// +kubebuilder:default=false
	Paused *bool `json:"paused,omitempty"`
	// ExecutionTimeout includes queueing and provisioning.
	// +optional
	// +kubebuilder:default="15m"
	// +kubebuilder:validation:XValidation:rule="duration(self) >= duration('1s') && duration(self) <= duration('2562047h')",message="executionTimeout must be between one second and 2562047 hours"
	ExecutionTimeout *metav1.Duration `json:"executionTimeout,omitempty"`
}

// ScheduledRunStatus reports reconciliation of the desired schedule. Execution
// history is retained separately and is available through ScheduledRunService.
type ScheduledRunStatus struct {
	// ObservedGeneration is the generation reflected in the scheduling projection.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions describe whether the resource is ready to reserve executions.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ScheduledRun configures invocations using the scheduler's authenticated
// identity. Deletion stops reservations once reconciled; accepted executions
// and their history survive deletion. A recreated name has a new UID.
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=sr
// +kubebuilder:printcolumn:name="Agent",type=string,JSONPath=`.spec.agentRef.name`
// +kubebuilder:printcolumn:name="Schedule",type=string,JSONPath=`.spec.schedule`
// +kubebuilder:printcolumn:name="Paused",type=boolean,JSONPath=`.spec.paused`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type ScheduledRun struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +required
	Spec ScheduledRunSpec `json:"spec"`
	// +optional
	Status ScheduledRunStatus `json:"status,omitempty"`
}

// ScheduledRunList contains ScheduledRun resources.
// +kubebuilder:object:root=true
type ScheduledRunList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// +required
	Items []ScheduledRun `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, &ScheduledRun{}, &ScheduledRunList{})
		return nil
	})
}
