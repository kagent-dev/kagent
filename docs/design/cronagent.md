# CronAgent Design Document

**Status**: Draft
**Created**: 2026-03-04
**Author**: Design Interview with User

## Overview

CronAgent is a new CRD that enables scheduled agent execution in kagent. It provides CronJob-like functionality for running AI agents on a schedule, supporting both one-shot tasks and persistent conversation threads.

## Goals

- Enable scheduled execution of agents (e.g., daily reports, periodic health checks)
- Leverage native Kubernetes CronJob for reliable scheduling
- Support both ephemeral and persistent conversation threads
- Maintain compatibility with existing Agent patterns
- Provide clear history and debugging capabilities

## Non-Goals

- Real-time agent orchestration (use regular Agent CRD)
- Complex multi-agent scheduling dependencies
- Advanced scheduling beyond cron expressions

## Architecture

### High-Level Flow

```
┌─────────────┐     creates      ┌──────────────┐     creates     ┌─────────┐
│ CronAgent   │──────────────────▶│  K8s CronJob │────────────────▶│  Job    │
│     CR      │                   │              │   (on schedule) │         │
└─────────────┘                   └──────────────┘                 └─────────┘
       │                                                                  │
       │ stores config                                                   │ creates
       ▼                                                                  ▼
┌─────────────┐                                                   ┌─────────┐
│  Database   │◀──────────────────────────────────────────────────│   Pod   │
│             │        queries thread, stores messages            │ (Agent) │
└─────────────┘                                                   └─────────┘
```

### Component Responsibilities

**CronAgent Controller**:
- Watches CronAgent CRs
- Creates/updates K8s CronJob resources
- Manages history cleanup (old Jobs)
- Updates CronAgent status with run history
- Handles manual triggers (annotation-based and API)

**K8s CronJob**:
- Schedules Job creation based on cron expression
- Handles concurrency policy, deadlines, timezone

**Job/Pod (Agent Runtime)**:
- Reads initial task from config (env var/ConfigMap)
- Queries database for session (persistent mode) or creates new session (per-run mode)
- Executes task, stores events (messages) in database
- Exits when task completes or requires user input

**Database**:
- Stores CronAgent configuration in `Agent` table (type=cronagent)
- Stores run history in `Agent` table (type=cronagent-run)
- Manages sessions via existing `Session.AgentID` field
- Persists conversation events (messages) in `Event` table

## CRD Specification

### CronAgentSpec

```go
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
    ConcurrencyPolicy ConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`

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
type ThreadPolicy string

const (
    ThreadPolicyPerRun     ThreadPolicy = "PerRun"     // Create new session for each run
    ThreadPolicyPersistent ThreadPolicy = "Persistent" // Reuse same session across runs
)

// ConcurrencyPolicy specifies how to handle concurrent CronAgent executions
type ConcurrencyPolicy string

const (
    ConcurrencyPolicyAllow   ConcurrencyPolicy = "Allow"   // Allow concurrent jobs
    ConcurrencyPolicyForbid  ConcurrencyPolicy = "Forbid"  // Skip if previous is running
    ConcurrencyPolicyReplace ConcurrencyPolicy = "Replace" // Cancel old, start new
)
```

### CronAgentStatus

```go
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
```

### Example CronAgent YAML

```yaml
apiVersion: kagent.dev/v1alpha2
kind: CronAgent
metadata:
  name: daily-report
  namespace: default
spec:
  # Run at 9 AM EST every weekday
  schedule: "0 9 * * 1-5"
  timezone: "America/New_York"

  # Task to execute on each run
  initialTask: |
    Generate a daily report of all agent activity from the past 24 hours.
    Include metrics on successful runs, failures, and resource usage.

  # Create fresh session for each run
  threadPolicy: PerRun

  # Don't allow concurrent runs
  concurrencyPolicy: Forbid

  # Keep last 7 successful and 3 failed runs
  successfulJobsHistoryLimit: 7
  failedJobsHistoryLimit: 3

  # Agent configuration
  agentTemplate:
    modelConfig:
      provider: openAI
      model: gpt-4
      temperature: 0.7

    skills:
      - name: prometheus-query
        enabled: true
      - name: slack-notify
        enabled: true

    resources:
      limits:
        memory: "512Mi"
        cpu: "500m"
```

### Example Persistent Session CronAgent

```yaml
apiVersion: kagent.dev/v1alpha2
kind: CronAgent
metadata:
  name: continuous-monitor
  namespace: default
spec:
  # Check every hour
  schedule: "0 * * * *"

  initialTask: |
    Check the system health metrics and continue tracking any issues
    from previous runs. Alert if any new problems are detected.

  # Maintain conversation context across runs (shared session)
  threadPolicy: Persistent

  agentTemplate:
    modelConfig:
      provider: anthropic
      model: claude-sonnet-4
```

## Implementation Details

### Task Delivery (Option A: Baked-In)

The initial task is provided to the agent pod via environment variable or ConfigMap mount:

```go
// In translator when creating CronJob from CronAgent
func (t *CronAgentTranslator) TranslateToCronJob(cronAgent *CronAgent) *batchv1.CronJob {
    cronJob := &batchv1.CronJob{
        ObjectMeta: metav1.ObjectMeta{
            Name:      cronAgent.Name,
            Namespace: cronAgent.Namespace,
            // OwnerReference ensures automatic cleanup when CronAgent is deleted
            OwnerReferences: []metav1.OwnerReference{
                *metav1.NewControllerRef(cronAgent, v1alpha2.GroupVersion.WithKind("CronAgent")),
            },
        },
        Spec: batchv1.CronJobSpec{
            Schedule:                   cronAgent.Spec.CronAgentConfig.Schedule,
            TimeZone:                   cronAgent.Spec.CronAgentConfig.Timezone,
            ConcurrencyPolicy:          batchv1.ConcurrencyPolicy(cronAgent.Spec.CronAgentConfig.ConcurrencyPolicy),
            StartingDeadlineSeconds:    cronAgent.Spec.CronAgentConfig.StartingDeadlineSeconds,
            SuccessfulJobsHistoryLimit: cronAgent.Spec.CronAgentConfig.SuccessfulJobsHistoryLimit,
            FailedJobsHistoryLimit:     cronAgent.Spec.CronAgentConfig.FailedJobsHistoryLimit,
            Suspend:                    cronAgent.Spec.CronAgentConfig.Suspend,
            JobTemplate: batchv1.JobTemplateSpec{
                Spec: t.translateToJobSpec(cronAgent),
            },
        },
    }
    return cronJob
}

// In translator when CronJob creates individual Jobs
func (t *CronAgentTranslator) translateToJobSpec(cronAgent *CronAgent) batchv1.JobSpec {
    jobSpec := batchv1.JobSpec{
        Template: corev1.PodTemplateSpec{
            ObjectMeta: metav1.ObjectMeta{
                Annotations: map[string]string{
                    "kagent.dev/cronagent-name": cronAgent.Name,
                    "kagent.dev/thread-policy":  string(cronAgent.Spec.ThreadPolicy),
                },
            },
            Spec: corev1.PodSpec{
                Containers: []corev1.Container{
                    {
                        Name: "agent",
                        Env: []corev1.EnvVar{
                            {
                                Name:  "KAGENT_INITIAL_TASK",
                                Value: cronAgent.Spec.InitialTask,
                            },
                            {
                                Name:  "KAGENT_CRONAGENT_NAME",
                                Value: cronAgent.Name,
                            },
                            {
                                Name:  "KAGENT_THREAD_POLICY",
                                Value: string(cronAgent.Spec.ThreadPolicy),
                            },
                            // ... other env vars from agentTemplate
                        },
                    },
                },
            },
        },
    }

    // Reuse Agent translator for the rest of the Job spec
    t.agentTranslator.ApplyAgentSpecToJobSpec(&jobSpec, &cronAgent.Spec.AgentTemplate)

    return jobSpec
}
```

### Session Management

Agent runtime queries the database on startup using the existing `Session.AgentID` field:

```go
// In agent runtime (Go ADK or Python ADK)
func (a *Agent) Initialize(ctx context.Context) error {
    cronAgentName := os.Getenv("KAGENT_CRONAGENT_NAME")
    threadPolicy := os.Getenv("KAGENT_THREAD_POLICY")
    userID := os.Getenv("KAGENT_USER_ID") // System user for cron runs

    if cronAgentName != "" {
        // This is a CronAgent run
        var session *Session
        var err error

        if threadPolicy == "Persistent" {
            // Use cronAgentName as AgentID to share session across all runs
            session, err = a.DB.GetOrCreateSession(ctx, cronAgentName, userID)
        } else {
            // Use unique run ID as AgentID for isolated session
            runID := fmt.Sprintf("cronagent-%s-%d", cronAgentName, time.Now().Unix())
            session, err = a.DB.CreateSession(ctx, runID, userID)
        }

        if err != nil {
            return fmt.Errorf("failed to get/create session: %w", err)
        }
        a.SessionID = session.ID

        // Read initial task and execute
        initialTask := os.Getenv("KAGENT_INITIAL_TASK")
        return a.ExecuteTask(ctx, initialTask)
    }

    // Regular agent - wait for HTTP requests
    return a.StartHTTPServer(ctx)
}
```

### Controller Reconciliation Logic

```go
func (r *CronAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    cronAgent := &v1alpha2.CronAgent{}
    if err := r.Get(ctx, req.NamespacedName, cronAgent); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // Check for manual trigger annotation
    if cronAgent.Annotations["cronagent.kagent.dev/trigger"] != "" {
        if err := r.triggerManualRun(ctx, cronAgent); err != nil {
            return ctrl.Result{}, err
        }
        // Remove trigger annotation
        delete(cronAgent.Annotations, "cronagent.kagent.dev/trigger")
        return ctrl.Result{}, r.Update(ctx, cronAgent)
    }

    // Create or update CronJob
    cronJob := r.translator.TranslateToCronJob(cronAgent)
    if err := r.createOrUpdateCronJob(ctx, cronAgent, cronJob); err != nil {
        return ctrl.Result{}, err
    }

    // Update status by watching Jobs
    if err := r.updateStatus(ctx, cronAgent); err != nil {
        return ctrl.Result{}, err
    }

    // Cleanup old Jobs based on history limits
    if err := r.cleanupOldJobs(ctx, cronAgent); err != nil {
        return ctrl.Result{}, err
    }

    return ctrl.Result{}, nil
}

func (r *CronAgentReconciler) cleanupOldJobs(ctx context.Context, cronAgent *v1alpha2.CronAgent) error {
    jobList := &batchv1.JobList{}
    if err := r.List(ctx, jobList,
        client.InNamespace(cronAgent.Namespace),
        client.MatchingLabels{"cronagent.kagent.dev/name": cronAgent.Name},
    ); err != nil {
        return err
    }

    // Separate successful and failed jobs
    var successful, failed []*batchv1.Job
    for i := range jobList.Items {
        job := &jobList.Items[i]
        if job.Status.Succeeded > 0 {
            successful = append(successful, job)
        } else if job.Status.Failed > 0 {
            failed = append(failed, job)
        }
    }

    // Sort by creation timestamp (newest first)
    sort.Slice(successful, func(i, j int) bool {
        return successful[i].CreationTimestamp.After(successful[j].CreationTimestamp.Time)
    })
    sort.Slice(failed, func(i, j int) bool {
        return failed[i].CreationTimestamp.After(failed[j].CreationTimestamp.Time)
    })

    // Delete jobs exceeding limits
    successLimit := int32(3)
    if cronAgent.Spec.SuccessfulJobsHistoryLimit != nil {
        successLimit = *cronAgent.Spec.SuccessfulJobsHistoryLimit
    }

    failedLimit := int32(1)
    if cronAgent.Spec.FailedJobsHistoryLimit != nil {
        failedLimit = *cronAgent.Spec.FailedJobsHistoryLimit
    }

    for i := int(successLimit); i < len(successful); i++ {
        if err := r.Delete(ctx, successful[i]); err != nil {
            return err
        }
    }

    for i := int(failedLimit); i < len(failed); i++ {
        if err := r.Delete(ctx, failed[i]); err != nil {
            return err
        }
    }

    return nil
}
```

### Manual Trigger Support

#### Annotation-Based Trigger

Users can add/update an annotation to trigger an immediate run:

```bash
# Trigger a run immediately
kubectl annotate cronagent daily-report \
  cronagent.kagent.dev/trigger="$(date +%s)" --overwrite
```

The controller watches for this annotation and creates a Job immediately, then removes the annotation.

#### API Endpoint

Add a new HTTP endpoint for programmatic triggering:

```go
// POST /api/cronagents/{namespace}/{name}/trigger
func (h *CronAgentHandler) TriggerRun(w http.ResponseWriter, r *http.Request) {
    namespace := chi.URLParam(r, "namespace")
    name := chi.URLParam(r, "name")

    cronAgent := &v1alpha2.CronAgent{}
    if err := h.client.Get(r.Context(), types.NamespacedName{
        Namespace: namespace,
        Name:      name,
    }, cronAgent); err != nil {
        http.Error(w, err.Error(), http.StatusNotFound)
        return
    }

    // Create Job immediately
    timestamp := strconv.FormatInt(time.Now().Unix(), 10)
    job := h.translator.TranslateToJob(cronAgent, timestamp)

    if err := h.client.Create(r.Context(), job); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusCreated)
    json.NewEncoder(w).Encode(map[string]string{
        "jobName": job.Name,
        "message": "Run triggered successfully",
    })
}
```

## Database Schema

**Note on Terminology**: Kagent uses `Session` to represent a conversation/thread, and `Event` to represent individual messages within a session. The CRD field is named `threadPolicy` for user-facing clarity (matching common AI terminology), but internally it manages `Session` objects.

**Note on Types Location**: The enum types (`AgentType`, `ThreadPolicy`, `ConcurrencyPolicy`) and `CronAgentConfig` struct should be defined in `go/api/database/models.go` alongside the existing `Agent` struct. The CRD types in `go/api/v1alpha2/cronagent_types.go` will use the same string values for consistency.

**Session Management**: No changes needed to the existing `Session` table! We leverage the existing `Session.AgentID` field:
- **Persistent mode**: `AgentID = cronagent-name` (shared session across all runs)
- **PerRun mode**: `AgentID = cronagent-name-timestamp` (unique session per run)

### Agent Table Enhancement

Reuse the existing `Agent` table with a new field for CronAgent-specific controller configuration:

```go
// AgentType represents the type of agent stored in the database
type AgentType string

const (
    AgentTypeRegular   AgentType = "agent"          // Regular interactive agent
    AgentTypeCronAgent AgentType = "cronagent"      // CronAgent configuration
    AgentTypeCronRun   AgentType = "cronagent-run"  // Individual CronAgent run
)

// ThreadPolicy determines session/conversation behavior across CronAgent runs
type ThreadPolicy string

const (
    ThreadPolicyPerRun     ThreadPolicy = "PerRun"     // Create new session for each run
    ThreadPolicyPersistent ThreadPolicy = "Persistent" // Reuse same session across runs
)

// ConcurrencyPolicy specifies how to handle concurrent CronAgent executions
type ConcurrencyPolicy string

const (
    ConcurrencyPolicyAllow   ConcurrencyPolicy = "Allow"   // Allow concurrent jobs
    ConcurrencyPolicyForbid  ConcurrencyPolicy = "Forbid"  // Skip if previous is running
    ConcurrencyPolicyReplace ConcurrencyPolicy = "Replace" // Cancel old, start new
)

// CronAgentConfig contains controller-level configuration for scheduled agents
type CronAgentConfig struct {
    Schedule                   string             `json:"schedule"`
    Timezone                   *string            `json:"timezone,omitempty"`
    InitialTask                string             `json:"initial_task"`
    ThreadPolicy               ThreadPolicy       `json:"thread_policy"`
    ConcurrencyPolicy          *ConcurrencyPolicy `json:"concurrency_policy,omitempty"`
    StartingDeadlineSeconds    *int64             `json:"starting_deadline_seconds,omitempty"`
    SuccessfulJobsHistoryLimit *int32             `json:"successful_jobs_history_limit,omitempty"`
    FailedJobsHistoryLimit     *int32             `json:"failed_jobs_history_limit,omitempty"`
    Suspend                    *bool              `json:"suspend,omitempty"`
}

// Updated Agent struct with CronAgentConfig field
type Agent struct {
    ID        string         `gorm:"primaryKey" json:"id"`
    CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
    UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
    DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`

    Type   AgentType        `gorm:"not null" json:"type"`
    Config *adk.AgentConfig `gorm:"type:json" json:"config"` // Runtime configuration

    // Controller-level config for scheduled agents (only populated for type="cronagent")
    CronAgentConfig *CronAgentConfig `gorm:"type:json" json:"cronagent_config,omitempty"`
}

// Usage examples:

// 1. CronAgent CR stored with type=AgentTypeCronAgent
// {
//   "id": "cronagent-daily-report",
//   "type": "cronagent",
//   "config": {
//     "model": { "type": "openai", "model": "gpt-4" },
//     "instruction": "You are a reporting assistant",
//     ...
//   },
//   "cronagent_config": {
//     "schedule": "0 9 * * 1-5",
//     "timezone": "America/New_York",
//     "initial_task": "Generate daily report...",
//     "thread_policy": "PerRun",
//     "concurrency_policy": "Forbid",
//     "successful_jobs_history_limit": 3,
//     "failed_jobs_history_limit": 1
//   }
// }

// 2. CronAgent run stored with type=AgentTypeCronRun
// {
//   "id": "cronagent-daily-report-1709568000",
//   "type": "cronagent-run",
//   "config": {
//     "model": { "type": "openai", "model": "gpt-4" },
//     "instruction": "You are a reporting assistant",
//     ...
//   },
//   "cronagent_config": null  // Not needed for individual runs
// }

// 3. Regular agent stored with type=AgentTypeRegular
// {
//   "id": "my-agent",
//   "type": "agent",
//   "config": {
//     "model": { "type": "anthropic", "model": "claude-sonnet-4" },
//     ...
//   },
//   "cronagent_config": null
// }
```

### Database Client Methods

Add the following methods to the `database.Client` interface in `go/api/database/client.go`:

```go
// CronAgent methods
StoreCronAgent(cronAgent *Agent) error
GetCronAgent(name string) (*Agent, error)
ListCronAgents() ([]Agent, error)
DeleteCronAgent(name string) error
ListCronAgentRuns(cronAgentName string, limit int) ([]Agent, error)

// Note: Session management reuses existing methods:
// - GetOrCreateSession(ctx, agentID, userID) for Persistent mode
// - CreateSession(ctx, agentID, userID) for PerRun mode
```

Example implementation in `go/core/internal/database/database.go`:

```go
func (d *Database) StoreCronAgent(cronAgent *Agent) error {
    if cronAgent.Type != AgentTypeCronAgent {
        return fmt.Errorf("invalid agent type: %s, expected %s", cronAgent.Type, AgentTypeCronAgent)
    }
    return d.db.Save(cronAgent).Error
}

func (d *Database) GetCronAgent(name string) (*Agent, error) {
    var agent Agent
    result := d.db.Where("id = ? AND type = ?", name, AgentTypeCronAgent).First(&agent)
    if result.Error != nil {
        return nil, result.Error
    }
    return &agent, nil
}

func (d *Database) ListCronAgents() ([]Agent, error) {
    var agents []Agent
    result := d.db.Where("type = ?", AgentTypeCronAgent).Find(&agents)
    if result.Error != nil {
        return nil, result.Error
    }
    return agents, nil
}

func (d *Database) DeleteCronAgent(name string) error {
    return d.db.Where("id = ? AND type = ?", name, AgentTypeCronAgent).Delete(&Agent{}).Error
}

func (d *Database) ListCronAgentRuns(cronAgentName string, limit int) ([]Agent, error) {
    var agents []Agent
    query := d.db.Where("type = ? AND id LIKE ?", AgentTypeCronRun, cronAgentName+"-%").
        Order("created_at DESC")

    if limit > 0 {
        query = query.Limit(limit)
    }

    result := query.Find(&agents)
    if result.Error != nil {
        return nil, result.Error
    }
    return agents, nil
}
```

## UI Changes

### New Section: Scheduled Agents

Add a new navigation item and page for CronAgents:

```
Navigation:
├── Agents
├── Scheduled Agents (NEW)
├── Models
├── Tools
└── Settings
```

### CronAgent List View

```typescript
// ui/src/components/cronagents/CronAgentList.tsx
interface CronAgentListItem {
  name: string;
  namespace: string;
  schedule: string;
  timezone?: string;
  threadPolicy: 'PerRun' | 'Persistent';
  suspend: boolean;
  lastScheduleTime?: string;
  lastSuccessfulRun?: JobRunReference;
  lastFailedRun?: JobRunReference;
  activeRuns: JobRunReference[];
  nextRunTime?: string; // Calculated from schedule
}

// Columns:
// - Name
// - Schedule (cron expression + timezone)
// - Next Run (calculated)
// - Last Run (timestamp + status icon)
// - Active Runs (count + link)
// - Actions (Trigger, Suspend/Resume, Edit, Delete)
```

### CronAgent Detail View

Show:
- Configuration (schedule, timezone, thread policy, task)
- Run History (expandable table of past runs)
  - Timestamp
  - Status (success/failed/running)
  - Session ID (link to conversation)
  - Duration
  - Logs
- Manual Trigger button
- Edit/Delete actions

### Run History Table

Each run links to its conversation session, allowing users to:
- View the agent's work in that run
- See events/messages and tool calls
- Debug failures

## Testing Strategy

### Unit Tests

1. **CRD Validation**: Test field validation, defaults
2. **Translator**: Test CronJob and Job generation
3. **Thread Management**: Test database queries for persistent/per-run threads
4. **Controller Logic**: Test reconciliation, cleanup, manual triggers

### E2E Tests

```go
// go/core/test/e2e/cronagent_test.go

func TestCronAgentBasicScheduling(t *testing.T) {
    // Create CronAgent with "* * * * *" (every minute)
    // Wait for Job to be created
    // Verify Job has correct env vars
    // Verify Job pod runs successfully
    // Check status is updated
}

func TestCronAgentPersistentSession(t *testing.T) {
    // Create CronAgent with threadPolicy: Persistent
    // Trigger two runs manually
    // Verify both runs use same session ID
    // Check events are in same conversation session
}

func TestCronAgentPerRunSession(t *testing.T) {
    // Create CronAgent with threadPolicy: PerRun
    // Trigger two runs manually
    // Verify each run has different session ID
}

func TestCronAgentHistoryCleanup(t *testing.T) {
    // Create CronAgent with successfulJobsHistoryLimit: 2
    // Trigger 5 successful runs
    // Verify only 2 Jobs remain
}

func TestCronAgentManualTrigger(t *testing.T) {
    // Create CronAgent
    // Add trigger annotation
    // Verify Job is created immediately
    // Verify annotation is removed
}

func TestCronAgentSuspend(t *testing.T) {
    // Create CronAgent
    // Set suspend: true
    // Verify CronJob is updated with suspend: true
    // Verify no new Jobs are created
}

func TestCronAgentConcurrencyPolicies(t *testing.T) {
    // Test Allow, Forbid, Replace policies
    // Trigger overlapping runs
    // Verify behavior matches policy
}
```

### Integration Tests

Test interaction between:
- CronAgent controller and K8s CronJob
- Agent runtime and database thread queries
- UI and API endpoints

## Migration Path

### Phase 1: Core Implementation (MVP)
- [ ] Add database model enhancements to `go/api/database/models.go`:
  - [ ] Add `AgentType`, `ThreadPolicy`, `ConcurrencyPolicy` enum types
  - [ ] Add `CronAgentConfig` struct
  - [ ] Update `Agent` struct with `CronAgentConfig` field and typed `Type` field
  - [ ] No changes to `Session` struct - reuse existing `AgentID` field
- [ ] Add database client methods to `go/api/database/client.go`:
  - [ ] `StoreCronAgent`, `GetCronAgent`, `ListCronAgents`, `DeleteCronAgent`, `ListCronAgentRuns`
  - [ ] Reuse existing `GetOrCreateSession` and `CreateSession` for session management
- [ ] Define CronAgent CRD types in `go/api/v1alpha2/cronagent_types.go`
- [ ] Implement CronAgent controller in `go/core/internal/controller/cronagent_controller.go`
- [ ] Create translator for CronJob and Job generation in `go/core/internal/controller/translator/`
- [ ] Update agent runtime (Python/Go ADK) to support initial task execution via env vars
- [ ] Add unit tests for controller, translator, and database methods

### Phase 2: Manual Triggers & API
- [ ] Implement annotation-based manual trigger
- [ ] Add `/api/cronagents/{name}/trigger` endpoint
- [ ] Add CronAgent CRUD endpoints for UI

### Phase 3: UI
- [ ] Add "Scheduled Agents" navigation item
- [ ] Implement CronAgent list view
- [ ] Implement CronAgent detail view with run history
- [ ] Add create/edit CronAgent forms

### Phase 4: Advanced Features
- [ ] Implement timezone support (requires K8s 1.27+)
- [ ] Add Helm chart configuration for CronAgents
- [ ] Add observability (metrics, tracing)
- [ ] Support for custom Job templates

### Phase 5: Future Enhancements (Post-MVP)
- [ ] Hybrid start mode support (baked-in + HTTP request)
- [ ] Session rotation strategies (e.g., monthly session reset for persistent mode)
- [ ] Advanced scheduling (dependencies, windows)
- [ ] CronAgent templates/presets

## Open Questions

1. **UserID for CronAgent Sessions**: Sessions require a `UserID` field. For CronAgent runs (system-triggered), what should we use?
   - Option A: System user like `"system"` or `"cronagent"`
   - Option B: Allow CronAgent spec to specify a userID to run as
   - Option C: Use the creator/owner of the CronAgent CR (from K8s metadata)
2. **Resource Management**: Should we add resource quotas specifically for CronAgent runs to prevent runaway scheduling?
3. **Notifications**: Should we support built-in notifications for failed runs (Slack, email)?
4. **Metrics**: What Prometheus metrics should we expose? (runs_total, run_duration, active_runs)
5. **RBAC**: Should CronAgents have separate RBAC permissions from regular Agents?

## References

- [Kubernetes CronJob Documentation](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/)
- [Kubernetes Job Documentation](https://kubernetes.io/docs/concepts/workloads/controllers/job/)
- [Current Agent CRD](../../go/api/v1alpha2/agent_types.go)
- [Agent Translator](../../go/core/internal/controller/translator/)

---

**Next Steps**: Review this design, address open questions, and proceed with Phase 1 implementation.
