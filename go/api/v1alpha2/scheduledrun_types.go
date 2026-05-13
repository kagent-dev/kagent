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

// ConcurrencyPolicy describes how the ScheduledRun controller handles concurrent runs.
// +kubebuilder:validation:Enum=Forbid;Allow;Replace
type ConcurrencyPolicy string

const (
	ConcurrencyPolicyForbid  ConcurrencyPolicy = "Forbid"
	ConcurrencyPolicyAllow   ConcurrencyPolicy = "Allow"
	ConcurrencyPolicyReplace ConcurrencyPolicy = "Replace"
)

// RunStatus represents the outcome of a scheduled run.
// +kubebuilder:validation:Enum=Succeeded;Failed;Running
type RunStatus string

const (
	RunStatusSucceeded RunStatus = "Succeeded"
	RunStatusFailed    RunStatus = "Failed"
	RunStatusRunning   RunStatus = "Running"
)

// AgentReference holds a reference to an Agent resource.
type AgentReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"` // +optional
}

// ScheduledRunSpec defines the desired state of ScheduledRun.
type ScheduledRunSpec struct {
	// Schedule is a cron expression defining when to run the agent.
	// +kubebuilder:validation:MinLength=1
	Schedule string `json:"schedule"`

	// AgentRef is a reference to the Agent to execute.
	AgentRef AgentReference `json:"agentRef"`

	// Prompt is the text prompt to send to the agent on each run.
	// +kubebuilder:validation:MinLength=1
	Prompt string `json:"prompt"`

	// Suspend pauses scheduling when set to true.
	// +optional
	// +kubebuilder:default=false
	Suspend bool `json:"suspend,omitempty"`

	// ConcurrencyPolicy specifies how to treat concurrent runs.
	// +optional
	// +kubebuilder:default=Forbid
	ConcurrencyPolicy ConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`

	// MaxRunHistory is the maximum number of run history entries to retain.
	// +optional
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	MaxRunHistory int `json:"maxRunHistory,omitempty"`
}

// RunHistoryEntry records one execution of a scheduled run.
type RunHistoryEntry struct {
	StartTime      metav1.Time  `json:"startTime"`
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
	Status         RunStatus    `json:"status"`
	SessionID      string       `json:"sessionId,omitempty"`
	Message        string       `json:"message,omitempty"`
}

// ScheduledRunStatus defines the observed state of ScheduledRun.
type ScheduledRunStatus struct {
	LastRunTime        *metav1.Time       `json:"lastRunTime,omitempty"`
	NextRunTime        *metav1.Time       `json:"nextRunTime,omitempty"`
	Active             int                `json:"active"`
	RunHistory         []RunHistoryEntry  `json:"runHistory,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Schedule",type="string",JSONPath=".spec.schedule"
// +kubebuilder:printcolumn:name="Suspend",type="boolean",JSONPath=".spec.suspend"
// +kubebuilder:printcolumn:name="Active",type="integer",JSONPath=".status.active"
// +kubebuilder:printcolumn:name="Last Run",type="date",JSONPath=".status.lastRunTime"
// +kubebuilder:printcolumn:name="Next Run",type="date",JSONPath=".status.nextRunTime"
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
