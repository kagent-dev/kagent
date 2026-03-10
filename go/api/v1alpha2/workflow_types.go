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

// StepType represents the step execution mode.
// +kubebuilder:validation:Enum=action;agent
type StepType string

const (
	StepTypeAction StepType = "action"
	StepTypeAgent  StepType = "agent"
)

// ParamType represents the parameter type.
// +kubebuilder:validation:Enum=string;number;boolean
type ParamType string

const (
	ParamTypeString  ParamType = "string"
	ParamTypeNumber  ParamType = "number"
	ParamTypeBoolean ParamType = "boolean"
)

// StepPhase represents the execution phase of a step.
// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed;Skipped
type StepPhase string

const (
	StepPhasePending   StepPhase = "Pending"
	StepPhaseRunning   StepPhase = "Running"
	StepPhaseSucceeded StepPhase = "Succeeded"
	StepPhaseFailed    StepPhase = "Failed"
	StepPhaseSkipped   StepPhase = "Skipped"
)

// WorkflowRunPhase represents the overall phase of a workflow run.
const (
	WorkflowRunPhasePending   = "Pending"
	WorkflowRunPhaseRunning   = "Running"
	WorkflowRunPhaseSucceeded = "Succeeded"
	WorkflowRunPhaseFailed    = "Failed"
	WorkflowRunPhaseCancelled = "Cancelled"
)

// Condition types for WorkflowTemplate and WorkflowRun.
const (
	WorkflowTemplateConditionAccepted = "Accepted"

	WorkflowRunConditionAccepted  = "Accepted"
	WorkflowRunConditionRunning   = "Running"
	WorkflowRunConditionSucceeded = "Succeeded"
)

// Finalizer for WorkflowRun temporal cleanup.
const WorkflowRunFinalizer = "kagent.dev/temporal-cleanup"

// ParamSpec declares an input parameter for a workflow template.
type ParamSpec struct {
	// Name is the parameter name.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^[a-zA-Z_][a-zA-Z0-9_]*$`
	Name string `json:"name"`

	// Description of the parameter.
	// +optional
	Description string `json:"description,omitempty"`

	// Type is the parameter type.
	// +kubebuilder:validation:Enum=string;number;boolean
	// +kubebuilder:default=string
	// +optional
	Type ParamType `json:"type,omitempty"`

	// Default value for the parameter.
	// +optional
	Default *string `json:"default,omitempty"`

	// Enum restricts the parameter to a set of allowed values.
	// +optional
	Enum []string `json:"enum,omitempty"`
}

// Param provides a value for a template parameter.
type Param struct {
	// Name of the parameter.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Value of the parameter.
	// +kubebuilder:validation:Required
	Value string `json:"value"`
}

// StepOutput configures how step results are stored in workflow context.
type StepOutput struct {
	// As stores the full step result at context.<alias>.
	// Defaults to step name if omitted.
	// +optional
	As string `json:"as,omitempty"`

	// Keys maps selected output fields to top-level context keys.
	// +optional
	Keys map[string]string `json:"keys,omitempty"`
}

// StepPolicy overrides workflow-level defaults for a step.
type StepPolicy struct {
	// Retry configures retry behavior.
	// +optional
	Retry *WorkflowRetryPolicy `json:"retry,omitempty"`

	// Timeout configures timeout behavior.
	// +optional
	Timeout *WorkflowTimeoutPolicy `json:"timeout,omitempty"`
}

// WorkflowRetryPolicy maps directly to Temporal's RetryPolicy.
type WorkflowRetryPolicy struct {
	// MaxAttempts is the maximum number of attempts.
	// +kubebuilder:default=3
	// +optional
	MaxAttempts int32 `json:"maxAttempts,omitempty"`

	// InitialInterval is the initial retry delay.
	// +kubebuilder:default="1s"
	// +optional
	InitialInterval metav1.Duration `json:"initialInterval,omitempty"`

	// MaximumInterval is the maximum retry delay.
	// +kubebuilder:default="60s"
	// +optional
	MaximumInterval metav1.Duration `json:"maximumInterval,omitempty"`

	// BackoffCoefficient is the multiplier for retry delays.
	// Serialized as string to avoid float precision issues across languages.
	// +kubebuilder:default="2.0"
	// +optional
	BackoffCoefficient string `json:"backoffCoefficient,omitempty"`

	// NonRetryableErrors lists error types that should not be retried.
	// +optional
	NonRetryableErrors []string `json:"nonRetryableErrors,omitempty"`
}

// WorkflowTimeoutPolicy maps to Temporal activity timeout fields.
type WorkflowTimeoutPolicy struct {
	// StartToClose is the max time for a single attempt.
	// +kubebuilder:default="5m"
	// +optional
	StartToClose metav1.Duration `json:"startToClose,omitempty"`

	// ScheduleToClose is the max total time including retries.
	// +optional
	ScheduleToClose *metav1.Duration `json:"scheduleToClose,omitempty"`

	// Heartbeat is the max time between heartbeats.
	// +optional
	Heartbeat *metav1.Duration `json:"heartbeat,omitempty"`
}

// StepPolicyDefaults defines default policies applied to steps.
type StepPolicyDefaults struct {
	// Retry default policy.
	// +optional
	Retry *WorkflowRetryPolicy `json:"retry,omitempty"`

	// Timeout default policy.
	// +optional
	Timeout *WorkflowTimeoutPolicy `json:"timeout,omitempty"`
}

// RetentionPolicy controls run history cleanup.
type RetentionPolicy struct {
	// SuccessfulRunsHistoryLimit is the max number of successful runs to keep.
	// +kubebuilder:default=10
	// +optional
	SuccessfulRunsHistoryLimit *int32 `json:"successfulRunsHistoryLimit,omitempty"`

	// FailedRunsHistoryLimit is the max number of failed runs to keep.
	// +kubebuilder:default=5
	// +optional
	FailedRunsHistoryLimit *int32 `json:"failedRunsHistoryLimit,omitempty"`
}

// StepSpec defines a single step in the workflow DAG.
type StepSpec struct {
	// Name uniquely identifies this step within the workflow.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^[a-z][a-z0-9-]*$`
	Name string `json:"name"`

	// Type is the step execution mode.
	// +kubebuilder:validation:Enum=action;agent
	Type StepType `json:"type"`

	// Action is the registered activity name (for type=action).
	// +optional
	Action string `json:"action,omitempty"`

	// AgentRef is the kagent Agent name (for type=agent).
	// +optional
	AgentRef string `json:"agentRef,omitempty"`

	// Prompt is a template rendered before agent invocation (for type=agent).
	// Supports ${{ params.* }} and ${{ context.* }} interpolation.
	// +optional
	Prompt string `json:"prompt,omitempty"`

	// With provides input key-value pairs for the step.
	// Values support ${{ }} expression interpolation.
	// +optional
	With map[string]string `json:"with,omitempty"`

	// DependsOn lists step names that must complete before this step runs.
	// +optional
	DependsOn []string `json:"dependsOn,omitempty"`

	// Output configures how step results are stored in context.
	// +optional
	Output *StepOutput `json:"output,omitempty"`

	// Policy overrides workflow-level defaults for this step.
	// +optional
	Policy *StepPolicy `json:"policy,omitempty"`

	// OnFailure determines behavior when this step fails.
	// +kubebuilder:validation:Enum=stop;continue
	// +kubebuilder:default=stop
	// +optional
	OnFailure string `json:"onFailure,omitempty"`
}

// StepStatus tracks the execution status of a single step.
type StepStatus struct {
	// Name of the step.
	Name string `json:"name"`

	// Phase is the current execution phase.
	Phase StepPhase `json:"phase"`

	// StartTime is when the step started executing.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the step finished executing.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Message provides additional detail about the step status.
	// +optional
	Message string `json:"message,omitempty"`

	// Retries is the number of retry attempts made.
	// +optional
	Retries int32 `json:"retries,omitempty"`

	// SessionID is the child workflow session ID for agent steps.
	// +optional
	SessionID string `json:"sessionID,omitempty"`
}

// --- WorkflowTemplate ---

// WorkflowTemplateSpec defines the desired state of a WorkflowTemplate.
type WorkflowTemplateSpec struct {
	// Description of the workflow.
	// +optional
	Description string `json:"description,omitempty"`

	// Params declares input parameters.
	// +optional
	Params []ParamSpec `json:"params,omitempty"`

	// Steps defines the workflow DAG.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=200
	Steps []StepSpec `json:"steps"`

	// Defaults for step policies when not specified per-step.
	// +optional
	Defaults *StepPolicyDefaults `json:"defaults,omitempty"`

	// Retention controls run history cleanup.
	// +optional
	Retention *RetentionPolicy `json:"retention,omitempty"`
}

// WorkflowTemplateStatus defines the observed state of a WorkflowTemplate.
type WorkflowTemplateStatus struct {
	// ObservedGeneration is the most recent generation observed.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// StepCount is the number of steps in the template.
	StepCount int32 `json:"stepCount,omitempty"`

	// Validated indicates the template passed DAG and reference validation.
	Validated bool `json:"validated,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Steps",type=integer,JSONPath=`.status.stepCount`
// +kubebuilder:printcolumn:name="Validated",type=boolean,JSONPath=`.status.validated`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// WorkflowTemplate is the Schema for the workflowtemplates API.
type WorkflowTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkflowTemplateSpec   `json:"spec,omitempty"`
	Status WorkflowTemplateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WorkflowTemplateList contains a list of WorkflowTemplate.
type WorkflowTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkflowTemplate `json:"items"`
}

// --- WorkflowRun ---

// WorkflowRunSpec defines the desired state of a WorkflowRun.
type WorkflowRunSpec struct {
	// WorkflowTemplateRef is the name of the WorkflowTemplate.
	// +kubebuilder:validation:Required
	WorkflowTemplateRef string `json:"workflowTemplateRef"`

	// Params provides values for template parameters.
	// +optional
	Params []Param `json:"params,omitempty"`

	// TTLSecondsAfterFinished controls automatic deletion after completion.
	// +optional
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`
}

// WorkflowRunStatus defines the observed state of a WorkflowRun.
type WorkflowRunStatus struct {
	// ObservedGeneration is the most recent generation observed.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase is a derived summary: Pending, Running, Succeeded, Failed, Cancelled.
	// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed;Cancelled
	// +optional
	Phase string `json:"phase,omitempty"`

	// ResolvedSpec is the snapshot of the template at run creation.
	// +optional
	ResolvedSpec *WorkflowTemplateSpec `json:"resolvedSpec,omitempty"`

	// TemplateGeneration tracks which generation of the template was used.
	TemplateGeneration int64 `json:"templateGeneration,omitempty"`

	// TemporalWorkflowID is the Temporal workflow execution ID.
	// +optional
	TemporalWorkflowID string `json:"temporalWorkflowID,omitempty"`

	// StartTime is when the Temporal workflow started.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the workflow finished.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Steps tracks per-step execution status.
	// +optional
	Steps []StepStatus `json:"steps,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Template",type=string,JSONPath=`.spec.workflowTemplateRef`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// WorkflowRun is the Schema for the workflowruns API.
type WorkflowRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkflowRunSpec   `json:"spec,omitempty"`
	Status WorkflowRunStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WorkflowRunList contains a list of WorkflowRun.
type WorkflowRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkflowRun `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&WorkflowTemplate{}, &WorkflowTemplateList{},
		&WorkflowRun{}, &WorkflowRunList{},
	)
}
