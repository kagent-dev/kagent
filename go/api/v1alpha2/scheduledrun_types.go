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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// DefaultScheduledRunTimeZone is used when spec.timeZone is omitted.
const DefaultScheduledRunTimeZone = "UTC"

// DefaultScheduledRunRecentExecutionsLimit is used when spec.recentExecutionsLimit is omitted.
const DefaultScheduledRunRecentExecutionsLimit int32 = 10

// DefaultScheduledRunExecutionTimeout is used when spec.executionTimeout is omitted.
const DefaultScheduledRunExecutionTimeout = 15 * time.Minute

// MaxScheduledRunStatusMessageLength is the maximum number of characters
// retained in an execution status message.
const MaxScheduledRunStatusMessageLength = 32768

const (
	// ScheduledRunTargetAPIGroup is the API group for built-in ScheduledRun targets.
	ScheduledRunTargetAPIGroup = "kagent.dev"
	// ScheduledRunTargetKindAgent is the Agent target kind.
	ScheduledRunTargetKindAgent = "Agent"
	// ScheduledRunTargetKindSandboxAgent is the SandboxAgent target kind.
	ScheduledRunTargetKindSandboxAgent = "SandboxAgent"
)

// ScheduledRunExecutionStatus is the current lifecycle state of one execution.
//
// Lifecycle:
//   - DispatchFailed: the dispatch could not be started or did not return a valid response.
//   - InProgress: the target accepted the dispatch and has not reached a terminal state.
//   - Succeeded: the execution completed successfully.
//   - Failed: the execution failed, was canceled, or was rejected.
//   - TimedOut: the execution did not complete within spec.executionTimeout.
//
// +kubebuilder:validation:Enum=DispatchFailed;InProgress;Succeeded;Failed;TimedOut
type ScheduledRunExecutionStatus string

const (
	ScheduledRunExecutionStatus_DispatchFailed ScheduledRunExecutionStatus = "DispatchFailed"
	ScheduledRunExecutionStatus_InProgress     ScheduledRunExecutionStatus = "InProgress"
	ScheduledRunExecutionStatus_Succeeded      ScheduledRunExecutionStatus = "Succeeded"
	ScheduledRunExecutionStatus_Failed         ScheduledRunExecutionStatus = "Failed"
	ScheduledRunExecutionStatus_TimedOut       ScheduledRunExecutionStatus = "TimedOut"
)

// ScheduledRunExecutionTrigger identifies how an execution was started.
// +kubebuilder:validation:Enum=Scheduled;Manual
type ScheduledRunExecutionTrigger string

const (
	ScheduledRunExecutionTrigger_Scheduled ScheduledRunExecutionTrigger = "Scheduled"
	ScheduledRunExecutionTrigger_Manual    ScheduledRunExecutionTrigger = "Manual"
)

// ScheduledRunSpec defines the desired state of ScheduledRun.
// +kubebuilder:validation:XValidation:rule="self.targetRef.name.size() <= 253 && self.targetRef.name.matches('^([a-z0-9]([-a-z0-9]*[a-z0-9])?)([.][a-z0-9]([-a-z0-9]*[a-z0-9])?)*$')",message="targetRef.name must be a valid DNS subdomain"
// +kubebuilder:validation:XValidation:rule="has(self.targetRef.kind) && self.targetRef.kind in ['Agent', 'SandboxAgent']",message="targetRef.kind must be Agent or SandboxAgent"
// +kubebuilder:validation:XValidation:rule="has(self.targetRef.apiGroup) && self.targetRef.apiGroup == 'kagent.dev'",message="targetRef.apiGroup must be kagent.dev"
// +kubebuilder:validation:XValidation:rule="!has(self.executionTimeout) || duration(self.executionTimeout) > duration('0s')",message="executionTimeout must be greater than zero"
// +kubebuilder:validation:XValidation:rule="self.targetRef == oldSelf.targetRef",message="targetRef is immutable"
type ScheduledRunSpec struct {
	// Schedule is a cron expression defining when to dispatch an execution. Standard
	// 5-field cron syntax (minute hour day-of-month month day-of-week). Each
	// tick starts an independent execution, so executions may overlap. Ticks
	// missed while the scheduler is unavailable are not replayed.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^\S+(\s+\S+){4}$`
	Schedule string `json:"schedule"`

	// TimeZone is an IANA time zone name (e.g. "America/Los_Angeles") used
	// to interpret Schedule. Defaults to UTC.
	// +optional
	// +kubebuilder:default=UTC
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^\S+$`
	TimeZone *string `json:"timeZone,omitempty"`

	// TargetRef identifies a resource in the same namespace to dispatch to. APIGroup,
	// Kind, and Name are required; the scheduler currently supports kagent.dev
	// Agent and SandboxAgent resources. TargetRef is immutable after creation so
	// execution recovery and historical session links always use the original target.
	// +required
	TargetRef corev1.TypedLocalObjectReference `json:"targetRef"`

	// Prompt is the text prompt to send to the agent for each execution.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=32768
	// +kubebuilder:validation:Pattern=`\S`
	Prompt string `json:"prompt"`

	// Suspended pauses automatic scheduling when set to true. Manual triggers
	// are still allowed.
	// +optional
	// +kubebuilder:default=false
	Suspended *bool `json:"suspended,omitempty"`

	// AllowSessionInteraction allows users who can read this ScheduledRun to
	// continue sessions created by it. Defaults to false.
	// +optional
	// +kubebuilder:default=false
	AllowSessionInteraction *bool `json:"allowSessionInteraction,omitempty"`

	// ExecutionTimeout is the maximum duration allowed for one execution, including
	// dispatch and asynchronous task polling. Defaults to 15 minutes.
	// +optional
	// +kubebuilder:default="15m"
	ExecutionTimeout *metav1.Duration `json:"executionTimeout,omitempty"`

	// RecentExecutionsLimit is the maximum number of recently completed
	// executions included with the ScheduledRun. In-progress and older
	// executions remain available in execution history.
	// +optional
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	RecentExecutionsLimit *int32 `json:"recentExecutionsLimit,omitempty"`
}

// ScheduledRunExecution describes one dispatch to a ScheduledRun target.
type ScheduledRunExecution struct {
	// ID uniquely identifies this execution independently of its session or task.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	ID string `json:"id"`
	// StartTime is when the dispatch began.
	// +required
	StartTime metav1.Time `json:"startTime"`
	// CompletionTime is when the execution reached a terminal state.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
	// Trigger identifies whether the execution was started by the schedule or
	// by a manual request.
	// +required
	Trigger ScheduledRunExecutionTrigger `json:"trigger"`
	// SessionID identifies the conversation session created for this execution.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	SessionID *string `json:"sessionID,omitempty"`
	// TaskID identifies an asynchronous task created for this execution. It is
	// retained after completion for correlation and recovery.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	TaskID *string `json:"taskID,omitempty"`
	// Status is the current lifecycle state of this execution.
	// +required
	Status ScheduledRunExecutionStatus `json:"status"`
	// StatusMessage provides human-readable details when dispatch fails or an
	// execution fails or times out.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=32768
	StatusMessage *string `json:"statusMessage,omitempty"`
}

// ScheduledRunConditionTypeAccepted reports whether controller validation succeeded.
const ScheduledRunConditionTypeAccepted = "Accepted"

// ScheduledRunStatus defines the observed state of ScheduledRun.
type ScheduledRunStatus struct {
	// LastExecutionTime is when the most recent scheduled or manual execution started.
	// +optional
	LastExecutionTime *metav1.Time `json:"lastExecutionTime,omitempty"`
	// NextExecutionTime is the next automatic execution time. It is omitted while the
	// ScheduledRun is suspended or rejected.
	// +optional
	NextExecutionTime *metav1.Time `json:"nextExecutionTime,omitempty"`
	// RecentExecutions contains in-progress executions and the most recently
	// completed executions selected by spec.recentExecutionsLimit.
	// +optional
	RecentExecutions []ScheduledRunExecution `json:"recentExecutions,omitempty"`
	// Conditions describe whether the ScheduledRun configuration is accepted.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the metadata generation evaluated by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Schedule",type="string",JSONPath=".spec.schedule"
// +kubebuilder:printcolumn:name="Suspended",type="boolean",JSONPath=".spec.suspended"
// +kubebuilder:printcolumn:name="Last Execution",type="date",JSONPath=".status.lastExecutionTime"
// +kubebuilder:printcolumn:name="Next Execution",type="date",JSONPath=".status.nextExecutionTime"
// +kubebuilder:storageversion

// ScheduledRun is the Schema for the scheduledruns API.
type ScheduledRun struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Spec ScheduledRunSpec `json:"spec"`
	// +optional
	Status ScheduledRunStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ScheduledRunList contains a list of ScheduledRun.
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
