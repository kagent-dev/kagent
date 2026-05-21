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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// AnnotationCreatedBy records the user identity that created a ScheduledRun.
// The scheduler uses this value as the session userID so the user who created
// the schedule can read the resulting session in the UI.
const AnnotationCreatedBy = "kagent.dev/created-by"

// DispatchStatus reflects whether the A2A SendMessage call to the agent pod
// succeeded. It says nothing about the LLM result — that lives in [RunOutcome].
// +kubebuilder:validation:Enum=Dispatched;DispatchFailed
type DispatchStatus string

const (
	// DispatchStatusDispatched means the A2A SendMessage call returned without
	// error. The session was created and the prompt was accepted by the agent
	// pod. The model invocation result is recorded separately in Outcome.
	DispatchStatusDispatched DispatchStatus = "Dispatched"
	// DispatchStatusFailed means dispatch itself failed: session create
	// error, A2A client error, agent pod 5xx, or panic in the dispatch path.
	DispatchStatusFailed DispatchStatus = "DispatchFailed"
)

// RunOutcome reflects the terminal state of the agent run, resolved
// asynchronously by polling the session's task state after dispatch returns.
// "Pending" means polling is still in progress (or was abandoned because the
// controller restarted before the session terminated).
// +kubebuilder:validation:Enum=Pending;Succeeded;Failed;Timeout
type RunOutcome string

const (
	// RunOutcomePending means the run was dispatched but no terminal task
	// state has been observed yet. Either polling is in progress, or the
	// controller restarted before resolution and the entry is now orphaned.
	RunOutcomePending RunOutcome = "Pending"
	// RunOutcomeSucceeded means the session's last task reached
	// TaskStateCompleted.
	RunOutcomeSucceeded RunOutcome = "Succeeded"
	// RunOutcomeFailed means the session's last task reached a non-success
	// terminal state (failed, canceled, rejected).
	RunOutcomeFailed RunOutcome = "Failed"
	// RunOutcomeTimeout means polling exceeded the configured budget without
	// observing a terminal state.
	RunOutcomeTimeout RunOutcome = "Timeout"
)

// AgentReference holds a reference to an Agent resource. AgentRef.Namespace
// may name a namespace different from the ScheduledRun's own — operators are
// responsible for ensuring the cross-namespace reference is intended (the
// controller does not enforce namespace boundaries).
type AgentReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"` // +optional
}

// ScheduledRunSpec defines the desired state of ScheduledRun.
type ScheduledRunSpec struct {
	// Schedule is a cron expression defining when to run the agent. Standard
	// 5-field cron syntax (minute hour day-of-month month day-of-week).
	// +kubebuilder:validation:MinLength=1
	Schedule string `json:"schedule"`

	// TimeZone is an IANA time zone name (e.g. "America/Los_Angeles") used
	// to interpret Schedule. If empty, the controller process's local time
	// zone (typically UTC in-cluster) is used.
	// +optional
	TimeZone string `json:"timeZone,omitempty"`

	// AgentRef is a reference to the Agent to execute. If Namespace is empty
	// it defaults to the ScheduledRun's namespace.
	AgentRef AgentReference `json:"agentRef"`

	// Prompt is the text prompt to send to the agent on each run.
	// +kubebuilder:validation:MinLength=1
	Prompt string `json:"prompt"`

	// Suspend pauses cron-driven scheduling when set to true. Manual triggers
	// via the API still execute; Suspend only gates the cron tick path.
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

// RunHistoryEntry records one execution of a scheduled run. DispatchStatus
// is set synchronously when the A2A call returns; Outcome is set
// asynchronously by polling the session.
type RunHistoryEntry struct {
	StartTime       metav1.Time    `json:"startTime"`
	CompletionTime  *metav1.Time   `json:"completionTime,omitempty"`
	DispatchStatus  DispatchStatus `json:"dispatchStatus"`
	DispatchMessage string         `json:"dispatchMessage,omitempty"`
	SessionID       string         `json:"sessionId,omitempty"`
	Outcome         RunOutcome     `json:"outcome,omitempty"`
	OutcomeMessage  string         `json:"outcomeMessage,omitempty"`
	OutcomeTime     *metav1.Time   `json:"outcomeTime,omitempty"`
}

// ScheduledRunStatus defines the observed state of ScheduledRun.
type ScheduledRunStatus struct {
	LastRunTime        *metav1.Time       `json:"lastRunTime,omitempty"`
	NextRunTime        *metav1.Time       `json:"nextRunTime,omitempty"`
	RunHistory         []RunHistoryEntry  `json:"runHistory,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
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
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ScheduledRunSpec   `json:"spec,omitempty"`
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
