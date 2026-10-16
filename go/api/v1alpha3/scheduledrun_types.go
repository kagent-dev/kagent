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
// +kubebuilder:resource:path=scheduledruns,singular=scheduledrun,categories=kagent
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Agent",type="string",JSONPath=".spec.agentRef.name"
// +kubebuilder:printcolumn:name="Schedule",type="string",JSONPath=".spec.schedule"
// +kubebuilder:printcolumn:name="Paused",type="boolean",JSONPath=".spec.paused"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ScheduledRun sends a prompt to an Agent on a cron schedule. Each occurrence
// creates a fresh Session owned by the ScheduledRun rather than by a human user.
// Task state and history belong to upstream A2A.
type ScheduledRun struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +required
	Spec ScheduledRunSpec `json:"spec"`
	// +optional
	Status ScheduledRunStatus `json:"status,omitempty"`
}

// ScheduledRunSpec describes when and what to send. The Agent reference is
// local to the ScheduledRun's namespace.
type ScheduledRunSpec struct {
	// AgentRef names the Agent that receives each occurrence. It is immutable:
	// reserved occurrences resolve the schedule's Agent when they start, so a
	// change could send an earlier prompt to a different Agent. Recreate the
	// ScheduledRun to target another Agent.
	// +kubebuilder:validation:XValidation:rule="has(self.name) && self.name != ''",message="agentRef.name must not be empty"
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="agentRef is immutable"
	// +required
	AgentRef corev1.LocalObjectReference `json:"agentRef"`
	// Schedule is a standard five-field cron expression evaluated in TimeZone.
	// Macros such as @hourly and seconds fields are not accepted.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:XValidation:rule="self.split(' ').filter(f, f != '').size() == 5",message="schedule must contain five cron fields"
	// +required
	Schedule string `json:"schedule"`
	// TimeZone is an IANA time zone name such as Europe/Paris. Omitted means UTC.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +optional
	TimeZone *string `json:"timeZone,omitempty"`
	// Prompt is the user message sent at each occurrence.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=32768
	// +kubebuilder:validation:Pattern=`\S`
	// +required
	Prompt string `json:"prompt"`
	// Paused keeps the schedule and its history without reserving new
	// occurrences. Omitted means false.
	// +kubebuilder:default=false
	// +optional
	Paused *bool `json:"paused,omitempty"`
	// ExecutionTimeout bounds one occurrence, including queueing and runtime
	// provisioning. Omitted means 15 minutes.
	// +kubebuilder:default="15m"
	// +kubebuilder:validation:XValidation:rule="duration(self) > duration('0s') && duration(self) <= duration('9223372036s')",message="executionTimeout must be positive and at most 9223372036s"
	// +optional
	ExecutionTimeout *metav1.Duration `json:"executionTimeout,omitempty"`
}

const (
	// ScheduledRunConditionAccepted reports whether the spec compiles to a
	// valid schedule: cron syntax, time zone, prompt, and timeout bounds.
	ScheduledRunConditionAccepted = "Accepted"
	// ScheduledRunConditionResolvedRefs reports whether the referenced Agent
	// exists. Occurrences of a schedule with a missing Agent fail at run time.
	ScheduledRunConditionResolvedRefs = "ResolvedRefs"
	// ScheduledRunConditionReady reports whether the schedule is persisted in
	// the control plane and will reserve occurrences unless paused.
	ScheduledRunConditionReady = "Ready"
)

// ScheduledRunStatus reports control-plane persistence of a ScheduledRun.
type ScheduledRunStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +kubebuilder:validation:MaxItems=3
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true

// ScheduledRunList contains ScheduledRun resources.
type ScheduledRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ScheduledRun `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, &ScheduledRun{}, &ScheduledRunList{})
		return nil
	})
}
