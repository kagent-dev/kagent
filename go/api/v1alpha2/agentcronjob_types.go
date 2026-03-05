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
)

const (
	AgentCronJobConditionTypeAccepted = "Accepted"
	AgentCronJobConditionTypeReady    = "Ready"
)

// AgentCronJobSpec defines the desired state of AgentCronJob.
type AgentCronJobSpec struct {
	// Schedule in standard cron format (5-field: minute hour day month weekday).
	// +kubebuilder:validation:MinLength=1
	Schedule string `json:"schedule"`

	// Prompt is the static user message sent to the agent on each run.
	// +kubebuilder:validation:MinLength=1
	Prompt string `json:"prompt"`

	// AgentRef is the name of the Agent CR to invoke. Must be in the same namespace.
	// +kubebuilder:validation:MinLength=1
	AgentRef string `json:"agentRef"`
}

// AgentCronJobStatus defines the observed state of AgentCronJob.
type AgentCronJobStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`

	// LastRunTime is the timestamp of the most recent execution.
	// +optional
	LastRunTime *metav1.Time `json:"lastRunTime,omitempty"`

	// NextRunTime is the calculated timestamp of the next execution.
	// +optional
	NextRunTime *metav1.Time `json:"nextRunTime,omitempty"`

	// LastRunResult is the result of the most recent execution: "Success" or "Failed".
	// +optional
	LastRunResult string `json:"lastRunResult,omitempty"`

	// LastRunMessage contains error details when LastRunResult is "Failed".
	// +optional
	LastRunMessage string `json:"lastRunMessage,omitempty"`

	// LastSessionID is the session ID created by the most recent execution.
	// +optional
	LastSessionID string `json:"lastSessionID,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Schedule",type="string",JSONPath=".spec.schedule",description="Cron schedule expression."
// +kubebuilder:printcolumn:name="Agent",type="string",JSONPath=".spec.agentRef",description="Referenced Agent CR name."
// +kubebuilder:printcolumn:name="LastRun",type="date",JSONPath=".status.lastRunTime",description="Time of the last execution."
// +kubebuilder:printcolumn:name="NextRun",type="date",JSONPath=".status.nextRunTime",description="Time of the next scheduled execution."
// +kubebuilder:printcolumn:name="LastResult",type="string",JSONPath=".status.lastRunResult",description="Result of the last execution."
// +kubebuilder:storageversion

// AgentCronJob is the Schema for the agentcronjobs API.
type AgentCronJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentCronJobSpec   `json:"spec,omitempty"`
	Status AgentCronJobStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AgentCronJobList contains a list of AgentCronJob.
type AgentCronJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentCronJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AgentCronJob{}, &AgentCronJobList{})
}
