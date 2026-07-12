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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// DefaultScheduledRunTimeZone is used when spec.timeZone is omitted.
const DefaultScheduledRunTimeZone = "UTC"

// DefaultScheduledRunMaxRunHistory is used when spec.maxRunHistory is omitted.
const DefaultScheduledRunMaxRunHistory = 10

const (
	// ScheduledRunTargetAPIGroup is the API group for built-in ScheduledRun targets.
	ScheduledRunTargetAPIGroup = "kagent.dev"
	// ScheduledRunTargetKindAgent is the Agent target kind.
	ScheduledRunTargetKindAgent = "Agent"
	// ScheduledRunTargetKindSandboxAgent is the SandboxAgent target kind.
	ScheduledRunTargetKindSandboxAgent = "SandboxAgent"
)

// RunStatus reflects the lifecycle state of a single scheduled run. It folds
// together the synchronous dispatch outcome and the asynchronous session
// terminal state into one field — readers only need to look at one value to
// answer "did this run succeed".
//
// Lifecycle:
//   - DispatchFailed: terminal, the A2A SendMessage call never reached the agent.
//   - Pending:        dispatched, terminal state not yet observed.
//   - Succeeded:      terminal, session's last task reached TaskStateCompleted.
//   - Failed:         terminal, session's last task reached failed/canceled/rejected.
//   - Timeout:        terminal, polling exceeded the configured budget.
//
// +kubebuilder:validation:Enum=DispatchFailed;Pending;Succeeded;Failed;Timeout
type RunStatus string

const (
	RunStatusDispatchFailed RunStatus = "DispatchFailed"
	RunStatusPending        RunStatus = "Pending"
	RunStatusSucceeded      RunStatus = "Succeeded"
	RunStatusFailed         RunStatus = "Failed"
	RunStatusTimeout        RunStatus = "Timeout"
)

// ScheduledRunSpec defines the desired state of ScheduledRun.
type ScheduledRunSpec struct {
	// Schedule is a cron expression defining when to run the agent. Standard
	// 5-field cron syntax (minute hour day-of-month month day-of-week).
	// +required
	// +kubebuilder:validation:MinLength=1
	Schedule string `json:"schedule"`

	// TimeZone is an IANA time zone name (e.g. "America/Los_Angeles") used
	// to interpret Schedule. Defaults to UTC.
	// +optional
	// +kubebuilder:default=UTC
	TimeZone string `json:"timeZone,omitempty"`

	// TargetRef is a local reference to the Agent or SandboxAgent to execute.
	// The target must live in the same namespace as the ScheduledRun.
	// +required
	TargetRef corev1.TypedLocalObjectReference `json:"targetRef"`

	// Prompt is the text prompt to send to the agent on each run.
	// +required
	// +kubebuilder:validation:MinLength=1
	Prompt string `json:"prompt"`

	// Suspend pauses automatic scheduling when set to true. Manual triggers
	// are still allowed.
	// +optional
	// +kubebuilder:default=false
	Suspend bool `json:"suspend,omitempty"`

	// MaxRunHistory is the maximum number of run history entries to retain.
	// +optional
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	MaxRunHistory int `json:"maxRunHistory,omitempty"`
}

// RunHistoryEntry records one execution of a scheduled run. Status starts as
// either DispatchFailed (terminal) or Pending, then transitions to a terminal
// state once outcome polling resolves.
type RunHistoryEntry struct {
	// +optional
	StartTime metav1.Time `json:"startTime"`
	// EndTime is set when Status reaches a terminal state — either
	// immediately on DispatchFailed, or when outcome polling resolves.
	// +optional
	EndTime *metav1.Time `json:"endTime,omitempty"`
	// +optional
	SessionID string `json:"sessionId,omitempty"`
	// +optional
	Status RunStatus `json:"status,omitempty"`
	// Message carries the dispatch error on DispatchFailed, or the agent's
	// terminal status message on Failed/Timeout.
	// +optional
	Message string `json:"message,omitempty"`
}

// ScheduledRunStatus defines the observed state of ScheduledRun.
type ScheduledRunStatus struct {
	// +optional
	LastRunTime *metav1.Time `json:"lastRunTime,omitempty"`
	// +optional
	NextRunTime *metav1.Time `json:"nextRunTime,omitempty"`
	// +optional
	RunHistory []RunHistoryEntry `json:"runHistory,omitempty"`
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Schedule",type="string",JSONPath=".spec.schedule"
// +kubebuilder:printcolumn:name="Suspend",type="boolean",JSONPath=".spec.suspend"
// +kubebuilder:printcolumn:name="Last Run",type="date",JSONPath=".status.lastRunTime"
// +kubebuilder:printcolumn:name="Next Run",type="string",JSONPath=".status.nextRunTime"
// +kubebuilder:storageversion

// ScheduledRun is the Schema for the scheduledruns API.
type ScheduledRun struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec ScheduledRunSpec `json:"spec,omitempty"`
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
