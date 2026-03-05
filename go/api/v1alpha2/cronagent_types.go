package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CronAgentSpec defines the desired state of CronAgent
type CronAgentSpec struct {
	// Schedule in cron format (required)
	// +kubebuilder:validation:Required
	Schedule string `json:"schedule"`

	// Timezone for the cron schedule (optional, default: UTC)
	// Uses IANA timezone database names (e.g., "America/New_York")
	// Requires Kubernetes 1.27+
	// +optional
	Timezone *string `json:"timezone,omitempty"`

	// InitialTask is the task/prompt to execute on each run (required)
	// +kubebuilder:validation:Required
	InitialTask string `json:"initialTask"`

	// ThreadPolicy determines session/conversation behavior across runs
	// PerRun (default): Create new session for each run (isolated conversations)
	// Persistent: Reuse the same session across all runs (continuous conversation)
	// +kubebuilder:validation:Enum=PerRun;Persistent
	// +kubebuilder:default=PerRun
	// +optional
	ThreadPolicy ThreadPolicy `json:"threadPolicy,omitempty"`

	// AgentTemplate defines the agent configuration for each run
	// Embedded AgentSpec ensures full compatibility with Agent features
	// +kubebuilder:validation:Required
	AgentTemplate AgentSpec `json:"agentTemplate"`

	// ConcurrencyPolicy specifies how to handle concurrent executions
	// Allow (default): Allow concurrent jobs
	// Forbid: Skip new run if previous is still running
	// Replace: Cancel running job and start new one
	// +kubebuilder:validation:Enum=Allow;Forbid;Replace
	// +kubebuilder:default=Allow
	// +optional
	ConcurrencyPolicy *ConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`

	// StartingDeadlineSeconds is the deadline in seconds for starting the job
	// if it misses scheduled time for any reason. Missed jobs executions will
	// be counted as failed ones. If not specified, jobs have no deadline.
	// +optional
	StartingDeadlineSeconds *int64 `json:"startingDeadlineSeconds,omitempty"`

	// SuccessfulJobsHistoryLimit is the number of successful finished jobs to retain
	// Defaults to 3
	// +kubebuilder:default=3
	// +optional
	SuccessfulJobsHistoryLimit *int32 `json:"successfulJobsHistoryLimit,omitempty"`

	// FailedJobsHistoryLimit is the number of failed finished jobs to retain
	// Defaults to 1
	// +kubebuilder:default=1
	// +optional
	FailedJobsHistoryLimit *int32 `json:"failedJobsHistoryLimit,omitempty"`

	// Suspend tells the controller to suspend subsequent executions
	// Defaults to false
	// +optional
	Suspend *bool `json:"suspend,omitempty"`
}

// ThreadPolicy determines session/conversation behavior across CronAgent runs
// +kubebuilder:validation:Enum=PerRun;Persistent
type ThreadPolicy string

const (
	ThreadPolicyPerRun     ThreadPolicy = "PerRun"     // Create new session for each run
	ThreadPolicyPersistent ThreadPolicy = "Persistent" // Reuse same session across runs
)

// ConcurrencyPolicy specifies how to handle concurrent CronAgent executions
// +kubebuilder:validation:Enum=Allow;Forbid;Replace
type ConcurrencyPolicy string

const (
	ConcurrencyPolicyAllow   ConcurrencyPolicy = "Allow"   // Allow concurrent jobs
	ConcurrencyPolicyForbid  ConcurrencyPolicy = "Forbid"  // Skip if previous is running
	ConcurrencyPolicyReplace ConcurrencyPolicy = "Replace" // Cancel old, start new
)

// CronAgentStatus defines the observed state of CronAgent
type CronAgentStatus struct {
	// LastScheduleTime is the last time the job was successfully scheduled
	// +optional
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`

	// LastSuccessfulRun contains information about the last successful run
	// +optional
	LastSuccessfulRun *JobRunReference `json:"lastSuccessfulRun,omitempty"`

	// LastFailedRun contains information about the last failed run
	// +optional
	LastFailedRun *JobRunReference `json:"lastFailedRun,omitempty"`

	// ActiveRuns is a list of currently running jobs
	// +optional
	ActiveRuns []JobRunReference `json:"activeRuns,omitempty"`

	// Conditions represent the latest available observations of the CronAgent's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// JobRunReference contains information about a specific CronAgent job run
type JobRunReference struct {
	// Name is the name of the Job
	Name string `json:"name"`

	// StartTime is when the job started
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the job completed
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// SessionID is the conversation session ID used by this run
	// +optional
	SessionID string `json:"sessionID,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=ca
// +kubebuilder:printcolumn:name="Schedule",type=string,JSONPath=`.spec.schedule`
// +kubebuilder:printcolumn:name="Suspend",type=boolean,JSONPath=`.spec.suspend`
// +kubebuilder:printcolumn:name="Active",type=integer,JSONPath=`.status.activeRuns`
// +kubebuilder:printcolumn:name="Last Schedule",type=date,JSONPath=`.status.lastScheduleTime`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// CronAgent is the Schema for the cronagents API
type CronAgent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CronAgentSpec   `json:"spec,omitempty"`
	Status CronAgentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CronAgentList contains a list of CronAgent
type CronAgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CronAgent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CronAgent{}, &CronAgentList{})
}
