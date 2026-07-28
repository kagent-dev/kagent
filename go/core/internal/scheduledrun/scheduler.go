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

package scheduledrun

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/go-logr/logr"
	"github.com/robfig/cron/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/a2a"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
)

var schedulerLog = ctrl.Log.WithName("scheduledrun-scheduler")

const (
	// drainTimeout bounds how long Start() waits for in-flight executions to finish
	// after the manager context is cancelled. Should be less than the pod's
	// terminationGracePeriodSeconds.
	drainTimeout = 25 * time.Second
	// statusWriteTimeout bounds the apiserver write that records an execution's
	// outcome.
	statusWriteTimeout = 10 * time.Second
)

// outcomePollInterval is a var so tests can shrink the production five-second
// poll cadence.
var outcomePollInterval = 5 * time.Second

var (
	errScheduledRunSuspended = errors.New("scheduled run is suspended")
	// ErrSchedulerNotActive indicates that this replica has not started, or is
	// already shutting down, its ScheduledRun lifecycle.
	ErrSchedulerNotActive = errors.New("scheduled run scheduler is not active")
)

// cronLoggerAdapter bridges logr.Logger to robfig/cron's logger interface.
type cronLoggerAdapter struct{ l logr.Logger }

func (a cronLoggerAdapter) Info(msg string, keysAndValues ...any) {
	a.l.Info(msg, keysAndValues...)
}
func (a cronLoggerAdapter) Error(err error, msg string, keysAndValues ...any) {
	a.l.Error(err, msg, keysAndValues...)
}

type ScheduledRunScheduler struct {
	kube         client.Client
	dbClient     database.Client
	agentClients *a2a.AgentClientRegistry
	cronEngine   *cron.Cron

	cronEntriesMu sync.Mutex
	cronEntries   map[types.NamespacedName]cron.EntryID

	// managerCtx is the context passed to Start by the controller manager. It is
	// the parent of every cron dispatch and outcome poller, so cancelling it on
	// shutdown unwinds all in-flight work. Leader election is intentionally not
	// used; a multi-replica deployment would dispatch duplicate executions.
	managerCtx atomic.Pointer[context.Context]
	active     atomic.Bool

	// pollersWG tracks outcome-polling goroutines so Start can drain them on
	// shutdown alongside scheduled executions.
	pollersMu       sync.Mutex
	pollersStopping bool
	pollers         map[string]struct{}
	pollersWG       sync.WaitGroup

	// dispatchHook sends the initial A2A request; tests override it so they don't
	// need a real A2A server to verify the cron-to-record-result flow.
	dispatchHook func(ctx context.Context, sr *v1alpha2.ScheduledRun, sessionID string) (a2atype.SendMessageResult, error)
	// outcomePollerHook resolves an asynchronous task to a terminal status.
	// Tests can replace or disable it to keep history writes deterministic.
	outcomePollerHook func(ctx context.Context, routeKey, taskID string) (v1alpha2.ScheduledRunExecutionStatus, string, error)
}

// NewScheduledRunScheduler constructs a scheduler.
func NewScheduledRunScheduler(kube client.Client, dbClient database.Client, agentClients *a2a.AgentClientRegistry) (*ScheduledRunScheduler, error) {
	if agentClients == nil {
		return nil, fmt.Errorf("agentClients must not be nil")
	}
	cronLogger := cronLoggerAdapter{l: schedulerLog}
	s := &ScheduledRunScheduler{
		kube:         kube,
		dbClient:     dbClient,
		agentClients: agentClients,
		// Recover protects the engine: a panic inside any one job no longer
		// kills the whole cron loop.
		cronEngine:  cron.New(cron.WithChain(cron.Recover(cronLogger))),
		cronEntries: make(map[types.NamespacedName]cron.EntryID),
		pollers:     make(map[string]struct{}),
	}
	s.dispatchHook = s.dispatchToTarget
	s.outcomePollerHook = s.pollExecutionOutcome
	return s, nil
}

// Start operates the scheduler as a single controller-manager Runnable: it resumes
// pollers for executions left InProgress by a prior process, starts the cron
// engine, and blocks until the manager context is cancelled, then drains
// in-flight scheduled executions and outcome pollers within drainTimeout.
func (s *ScheduledRunScheduler) Start(ctx context.Context) error {
	schedulerLog.Info("Starting Scheduled Run scheduler")
	s.managerCtx.Store(&ctx)
	s.pollersMu.Lock()
	s.pollersStopping = false
	s.pollersMu.Unlock()
	s.active.Store(true)

	if err := s.resumeInProgressPollers(ctx); err != nil {
		schedulerLog.Error(err, "Failed to resume in-progress executions")
	}
	s.cronEngine.Start()
	<-ctx.Done()

	schedulerLog.Info("Stopping Scheduled Run scheduler, draining in-flight executions")
	s.active.Store(false)
	s.pollersMu.Lock()
	s.pollersStopping = true
	s.pollersMu.Unlock()

	cronStopCtx := s.cronEngine.Stop()
	drained := make(chan struct{})
	go func() {
		<-cronStopCtx.Done()
		s.pollersWG.Wait()
		close(drained)
	}()
	select {
	case <-drained:
		schedulerLog.Info("All in-flight executions and outcome pollers drained")
	case <-time.After(drainTimeout):
		schedulerLog.Info("Drain timeout exceeded, abandoning in-flight work",
			"timeout", drainTimeout)
	}

	return nil
}

// managerContext returns the manager context installed by Start. Tests that do
// not start the scheduler fall back to context.Background().
func (s *ScheduledRunScheduler) managerContext() context.Context {
	if ctx := s.managerCtx.Load(); ctx != nil {
		return *ctx
	}
	return context.Background()
}

// CronSpecForSchedule builds the cron expression handed to robfig/cron,
// embedding the SR's TimeZone via the parser-supported CRON_TZ= prefix
// (parser.go:95 in robfig/cron v3).
func CronSpecForSchedule(sr *v1alpha2.ScheduledRun) string {
	return "CRON_TZ=" + ScheduledRunTimeZone(sr) + " " + strings.TrimSpace(sr.Spec.Schedule)
}

func ScheduledRunTimeZone(sr *v1alpha2.ScheduledRun) string {
	if sr.Spec.TimeZone != nil {
		if timeZone := strings.TrimSpace(*sr.Spec.TimeZone); timeZone != "" {
			return timeZone
		}
	}
	return v1alpha2.DefaultScheduledRunTimeZone
}

// IsSuspended reports the defaulted value of spec.suspended.
func IsSuspended(sr *v1alpha2.ScheduledRun) bool {
	return sr.Spec.Suspended != nil && *sr.Spec.Suspended
}

// AllowsSessionInteraction reports whether users may continue sessions created
// by this ScheduledRun. The secure default is read-only.
func AllowsSessionInteraction(sr *v1alpha2.ScheduledRun) bool {
	return sr.Spec.AllowSessionInteraction != nil && *sr.Spec.AllowSessionInteraction
}

// ExecutionTimeout returns the configured total execution timeout.
func ExecutionTimeout(sr *v1alpha2.ScheduledRun) time.Duration {
	if sr.Spec.ExecutionTimeout != nil && sr.Spec.ExecutionTimeout.Duration > 0 {
		return sr.Spec.ExecutionTimeout.Duration
	}
	return v1alpha2.DefaultScheduledRunExecutionTimeout
}

// RecentExecutionsLimit returns the configured number of recently completed
// executions included with the ScheduledRun.
func RecentExecutionsLimit(sr *v1alpha2.ScheduledRun) int {
	if sr.Spec.RecentExecutionsLimit != nil && *sr.Spec.RecentExecutionsLimit > 0 {
		return int(*sr.Spec.RecentExecutionsLimit)
	}
	return int(v1alpha2.DefaultScheduledRunRecentExecutionsLimit)
}

func (s *ScheduledRunScheduler) beginOutcomePoller(pollerID string) bool {
	s.pollersMu.Lock()
	defer s.pollersMu.Unlock()
	if s.pollersStopping {
		return false
	}
	if _, exists := s.pollers[pollerID]; exists {
		return false
	}
	s.pollers[pollerID] = struct{}{}
	s.pollersWG.Add(1)
	return true
}

func (s *ScheduledRunScheduler) endOutcomePoller(pollerID string) {
	s.pollersMu.Lock()
	delete(s.pollers, pollerID)
	s.pollersMu.Unlock()
	s.pollersWG.Done()
}

func routeKeyForScheduledRunTarget(kind string, key types.NamespacedName) (string, error) {
	switch kind {
	case TargetKindAgent:
		return a2a.RouteKeyForAgent(key.Namespace, key.Name), nil
	case TargetKindSandboxAgent:
		return a2a.RouteKeyForSandboxAgent(key.Namespace, key.Name), nil
	default:
		return "", fmt.Errorf("unsupported targetRef.kind %q", kind)
	}
}

func (s *ScheduledRunScheduler) UpdateCronEntry(sr *v1alpha2.ScheduledRun) error {
	// Reconciliation is also a recovery path once Start has installed the
	// manager context. This handles a transient list failure during Start and
	// is deduplicated against already-running pollers.
	if s.managerCtx.Load() != nil {
		s.resumeOutcomePollersForScheduledRun(sr)
	}

	s.cronEntriesMu.Lock()
	defer s.cronEntriesMu.Unlock()

	key := types.NamespacedName{Name: sr.Name, Namespace: sr.Namespace}

	if existingCronEntryID, ok := s.cronEntries[key]; ok {
		s.cronEngine.Remove(existingCronEntryID)
		delete(s.cronEntries, key)
	}

	if IsSuspended(sr) {
		return nil
	}

	cronEntryID, err := s.cronEngine.AddFunc(CronSpecForSchedule(sr), func() {
		if _, err := s.executeOnce(s.managerContext(), key, false); err != nil &&
			!apierrors.IsNotFound(err) &&
			!errors.Is(err, errScheduledRunSuspended) {
			schedulerLog.Error(err, "Scheduled Run cron tick failed", "scheduledRun", key)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to add cron entry for %s: %w", key, err)
	}

	s.cronEntries[key] = cronEntryID
	return nil
}

func (s *ScheduledRunScheduler) RemoveCronEntry(key types.NamespacedName) {
	s.cronEntriesMu.Lock()
	defer s.cronEntriesMu.Unlock()

	if existingCronEntryID, ok := s.cronEntries[key]; ok {
		s.cronEngine.Remove(existingCronEntryID)
		delete(s.cronEntries, key)
	}
}

func (s *ScheduledRunScheduler) HasCronEntry(key types.NamespacedName) bool {
	s.cronEntriesMu.Lock()
	defer s.cronEntriesMu.Unlock()
	_, ok := s.cronEntries[key]
	return ok
}

// TriggerManualExecution starts an execution through the same dispatch and persistence
// path as an automatic tick. A suspended schedule may still be manually triggered.
func (s *ScheduledRunScheduler) TriggerManualExecution(ctx context.Context, key types.NamespacedName) (*v1alpha2.ScheduledRunExecution, error) {
	if !s.active.Load() {
		return nil, ErrSchedulerNotActive
	}
	return s.executeOnce(ctx, key, true)
}

// executeOnce performs a single execution, records its immediate dispatch result,
// and starts a background poller only when the agent returns a non-terminal Task.
func (s *ScheduledRunScheduler) executeOnce(ctx context.Context, key types.NamespacedName, manual bool) (*v1alpha2.ScheduledRunExecution, error) {
	log := schedulerLog.WithValues("scheduledRun", key)

	var sr v1alpha2.ScheduledRun
	if err := s.kube.Get(ctx, key, &sr); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("get Scheduled Run %s: %w", key, err)
		}
		log.Error(err, "Failed to fetch Scheduled Run")
		return nil, fmt.Errorf("failed to fetch Scheduled Run %s: %w", key, err)
	}
	if IsSuspended(&sr) && !manual {
		log.Info("Skipping suspended Scheduled Run")
		writeCtx, cancel := context.WithTimeout(context.Background(), statusWriteTimeout)
		defer cancel()
		if err := s.updateStatusWithRetry(writeCtx, key, func(latest *v1alpha2.ScheduledRun) {
			latest.Status.NextExecutionTime = nil
		}); err != nil {
			log.Error(err, "Failed to clear next execution time for suspended Scheduled Run")
		}
		return nil, fmt.Errorf("%w: %s", errScheduledRunSuspended, key)
	}

	sessionID := a2atype.NewContextID()
	executionID := string(a2atype.NewContextID())
	startTime := metav1.Now()
	executionTimeout := ExecutionTimeout(&sr)

	dispatchCtx, dispatchCancel := context.WithTimeout(ctx, executionTimeout)
	dispatchResult, dispatchErr := s.dispatchHook(dispatchCtx, &sr, sessionID)
	dispatchCancel()

	completionTime := metav1.Now()
	trigger := v1alpha2.ScheduledRunExecutionTrigger_Scheduled
	if manual {
		trigger = v1alpha2.ScheduledRunExecutionTrigger_Manual
	}
	execution := buildExecution(executionID, startTime, completionTime, trigger, sessionID, dispatchResult, dispatchErr)
	if dispatchErr != nil {
		log.Error(dispatchErr, "Scheduled Run dispatch failed")
	}

	persistenceErr := s.storeExecution(context.Background(), key, string(sr.UID), execution)
	if persistenceErr != nil {
		log.Error(persistenceErr, "Failed to persist execution")
	}

	// Status writes use a fresh bounded ctx so the outcome is recorded even
	// when the manager ctx has been cancelled (graceful shutdown path).
	writeCtx, cancel := context.WithTimeout(context.Background(), statusWriteTimeout)
	defer cancel()
	statusErr := s.updateStatusWithRetry(writeCtx, key, func(latest *v1alpha2.ScheduledRun) {
		latest.Status.LastExecutionTime = &startTime
		latest.Status.RecentExecutions = append(latest.Status.RecentExecutions, execution)
		trimRecentExecutions(latest)
		// Advance NextExecutionTime here so it doesn't sit stale at the value
		// computed by the last reconcile (which may now be in the past).
		if IsSuspended(latest) {
			latest.Status.NextExecutionTime = nil
		} else if sched, err := cron.ParseStandard(CronSpecForSchedule(latest)); err == nil {
			next := metav1.NewTime(sched.Next(completionTime.Time))
			latest.Status.NextExecutionTime = &next
		}
	})
	if statusErr != nil {
		log.Error(statusErr, "Failed to record execution outcome")
	}

	if execution.Status == v1alpha2.ScheduledRunExecutionStatus_InProgress && s.outcomePollerHook != nil {
		s.resumeOutcomePoller(key, &sr, execution)
	}
	if persistenceErr != nil || statusErr != nil {
		return &execution, fmt.Errorf("failed to record execution outcome for %s: %w", key, errors.Join(persistenceErr, statusErr))
	}

	return &execution, nil
}

// buildExecution maps the immediate dispatch result into an execution.
// A non-terminal Task yields an InProgress execution carrying a TaskID for the
// outcome poller; every other case is terminal and carries a CompletionTime.
func buildExecution(
	executionID string,
	startTime, completionTime metav1.Time,
	trigger v1alpha2.ScheduledRunExecutionTrigger,
	sessionID string,
	dispatchResult a2atype.SendMessageResult,
	dispatchErr error,
) v1alpha2.ScheduledRunExecution {
	execution := v1alpha2.ScheduledRunExecution{
		ID:        executionID,
		StartTime: startTime,
		Trigger:   trigger,
		Status:    v1alpha2.ScheduledRunExecutionStatus_InProgress,
	}

	if dispatchErr != nil {
		var persistedSessionErr *scheduledRunDispatchError
		if errors.As(dispatchErr, &persistedSessionErr) && persistedSessionErr.sessionCreated {
			// Keep the session reachable for diagnostics. A transport error can
			// occur after the target accepted the request, so deleting it here
			// would risk orphaning tasks and events created by the target.
			execution.SessionID = optionalString(sessionID)
		}
		if errors.Is(dispatchErr, context.DeadlineExceeded) {
			execution.Status = v1alpha2.ScheduledRunExecutionStatus_TimedOut
		} else {
			execution.Status = v1alpha2.ScheduledRunExecutionStatus_DispatchFailed
		}
		execution.StatusMessage = executionStatusMessage(dispatchErr.Error())
		execution.CompletionTime = &completionTime
		return execution
	}

	execution.SessionID = optionalString(sessionID)
	dispatch, err := classifyDispatchResult(dispatchResult)
	if err != nil {
		execution.Status = v1alpha2.ScheduledRunExecutionStatus_DispatchFailed
		execution.StatusMessage = executionStatusMessage(err.Error())
		execution.CompletionTime = &completionTime
		return execution
	}

	execution.Status = dispatch.status
	execution.StatusMessage = executionStatusMessage(dispatch.message)
	if dispatch.terminal {
		execution.CompletionTime = &completionTime
	} else {
		execution.TaskID = optionalString(dispatch.taskID)
	}
	return execution
}

func (s *ScheduledRunScheduler) resumeInProgressPollers(ctx context.Context) error {
	var list v1alpha2.ScheduledRunList
	if err := s.kube.List(ctx, &list); err != nil {
		return fmt.Errorf("list Scheduled Runs: %w", err)
	}
	scheduledRuns := make(map[types.NamespacedName]*v1alpha2.ScheduledRun, len(list.Items))
	for i := range list.Items {
		sr := &list.Items[i]
		scheduledRuns[client.ObjectKeyFromObject(sr)] = sr
		s.resumeOutcomePollersForScheduledRun(sr)
	}
	if s.dbClient == nil {
		return nil
	}
	inProgressExecutions, err := s.dbClient.ListInProgressScheduledRunExecutions(ctx)
	if err != nil {
		return fmt.Errorf("list durable in-progress executions: %w", err)
	}
	for i := range inProgressExecutions {
		record := &inProgressExecutions[i]
		key := types.NamespacedName{
			Namespace: record.ScheduledRunNamespace,
			Name:      record.ScheduledRunName,
		}
		sr, exists := scheduledRuns[key]
		if !exists || string(sr.UID) != record.ScheduledRunUID ||
			record.SessionID == nil || record.TaskID == nil {
			continue
		}
		execution := v1alpha2.ScheduledRunExecution{
			ID:        record.ID,
			StartTime: metav1.NewTime(record.StartTime),
			Trigger:   record.Trigger,
			SessionID: record.SessionID,
			TaskID:    record.TaskID,
			Status:    record.Status,
		}
		execution.StatusMessage = record.StatusMessage
		s.resumeOutcomePoller(key, sr, execution)
	}
	return nil
}

func (s *ScheduledRunScheduler) resumeOutcomePollersForScheduledRun(sr *v1alpha2.ScheduledRun) {
	key := client.ObjectKeyFromObject(sr)
	for _, execution := range sr.Status.RecentExecutions {
		if execution.Status == v1alpha2.ScheduledRunExecutionStatus_InProgress && execution.SessionID != nil && execution.TaskID != nil {
			s.resumeOutcomePoller(key, sr, execution)
		}
	}
}

func (s *ScheduledRunScheduler) resumeOutcomePoller(key types.NamespacedName, sr *v1alpha2.ScheduledRun, execution v1alpha2.ScheduledRunExecution) {
	if s.outcomePollerHook == nil || execution.SessionID == nil || execution.TaskID == nil {
		return
	}
	routeKey, err := routeKeyForScheduledRunTarget(sr.Spec.TargetRef.Kind, TargetKey(sr.Namespace, sr.Spec.TargetRef))
	if err != nil {
		schedulerLog.Error(err, "Unable to poll Scheduled Run task", "scheduledRun", key, "sessionID", execution.SessionID)
		return
	}
	s.spawnOutcomePoller(key, string(sr.UID), execution, routeKey, ExecutionTimeout(sr))
}

// spawnOutcomePoller resolves an asynchronous task and updates its execution.
func (s *ScheduledRunScheduler) spawnOutcomePoller(
	key types.NamespacedName,
	scheduledRunUID string,
	execution v1alpha2.ScheduledRunExecution,
	routeKey string,
	executionTimeout time.Duration,
) {
	pollerID := fmt.Sprintf("%s/%s/%s", key.Namespace, key.Name, execution.ID)
	if !s.beginOutcomePoller(pollerID) {
		schedulerLog.V(1).Info("Scheduled Run outcome poller already active or scheduler stopping",
			"scheduledRun", key, "sessionID", execution.SessionID, "taskID", execution.TaskID)
		return
	}
	go func() {
		defer s.endOutcomePoller(pollerID)

		log := schedulerLog.WithValues("scheduledRun", key, "sessionID", execution.SessionID, "taskID", execution.TaskID)

		// Budget the poll against time already elapsed since the execution started so
		// a resumed poller (after a restart) doesn't grant a fresh full timeout.
		// An already-exhausted budget uses a minimal positive timeout so the
		// poller does one lookup and then records TimedOut rather than skipping.
		pollTimeout := executionTimeout
		if !execution.StartTime.IsZero() {
			remaining := executionTimeout - time.Since(execution.StartTime.Time)
			if remaining <= 0 {
				pollTimeout = time.Nanosecond
			} else {
				pollTimeout = remaining
			}
		}
		pollCtx, cancel := context.WithTimeout(s.managerContext(), pollTimeout)
		defer cancel()

		status, message, pollErr := s.outcomePollerHook(pollCtx, routeKey, *execution.TaskID)
		if pollErr != nil {
			if errors.Is(pollErr, context.Canceled) {
				log.Info("Outcome polling interrupted; leaving execution InProgress for restart recovery")
			} else {
				log.Error(pollErr, "Outcome polling failed; leaving execution InProgress for retry")
			}
			return
		}

		now := metav1.Now()
		execution.Status = status
		execution.StatusMessage = executionStatusMessage(message)
		execution.CompletionTime = &now
		if err := s.storeExecution(context.Background(), key, scheduledRunUID, execution); err != nil {
			log.Error(err, "Failed to persist execution outcome")
			// Durable execution history is authoritative. Keep the CRD execution InProgress
			// with its TaskID so a restart can retry the terminal write.
			return
		}
		if err := s.writeOutcomeStatusUntilSuccess(key, func(latest *v1alpha2.ScheduledRun) {
			found := false
			for i := range latest.Status.RecentExecutions {
				if latest.Status.RecentExecutions[i].ID == execution.ID {
					latest.Status.RecentExecutions[i].Status = status
					latest.Status.RecentExecutions[i].StatusMessage = executionStatusMessage(message)
					latest.Status.RecentExecutions[i].CompletionTime = &now
					found = true
					break
				}
			}
			if !found {
				latest.Status.RecentExecutions = append(latest.Status.RecentExecutions, execution)
				latest.Status.LastExecutionTime = &execution.StartTime
			}
			trimRecentExecutions(latest)
		}); err != nil {
			log.Error(err, "Failed to write outcome")
		}
	}()
}

// writeOutcomeStatusUntilSuccess keeps a durably stored terminal outcome from
// getting stranded behind a transient apiserver failure. Each attempt is
// bounded, and retries stop on shutdown or a permanent API error.
func (s *ScheduledRunScheduler) writeOutcomeStatusUntilSuccess(
	key types.NamespacedName,
	mutate func(*v1alpha2.ScheduledRun),
) error {
	for {
		writeCtx, cancel := context.WithTimeout(context.Background(), statusWriteTimeout)
		err := s.updateStatusWithRetry(writeCtx, key, mutate)
		cancel()
		if err == nil {
			return nil
		}
		if apierrors.IsNotFound(err) || apierrors.IsForbidden(err) || apierrors.IsInvalid(err) {
			return err
		}
		select {
		case <-s.managerContext().Done():
			return errors.Join(err, s.managerContext().Err())
		case <-time.After(time.Second):
		}
	}
}

// pollExecutionOutcome polls the A2A task until it reaches a terminal state.
func (s *ScheduledRunScheduler) pollExecutionOutcome(
	ctx context.Context,
	routeKey,
	taskID string,
) (v1alpha2.ScheduledRunExecutionStatus, string, error) {
	pollCtx := pkgauth.AuthSessionTo(ctx, scheduledRunSession{
		principal: pkgauth.Principal{User: pkgauth.User{ID: SessionUserID}},
	})
	t := time.NewTicker(outcomePollInterval)
	defer t.Stop()
	var lastLookupErr error
	for {
		task, err := s.agentClients.GetTaskFromRoute(pollCtx, routeKey, &a2atype.GetTaskRequest{ID: a2atype.TaskID(taskID)})
		if err == nil {
			if status, message, terminal := executionStatusForTask(task); terminal {
				return status, message, nil
			}
		} else {
			lastLookupErr = err
			schedulerLog.V(1).Info("Scheduled Run task lookup failed; retrying",
				"route", routeKey, "taskID", taskID, "error", err)
		}

		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				message := "task did not reach a terminal state before executionTimeout"
				if lastLookupErr != nil {
					message = fmt.Sprintf("%s; last task lookup failed: %v", message, lastLookupErr)
				}
				return v1alpha2.ScheduledRunExecutionStatus_TimedOut, message, nil
			}
			return "", "", ctx.Err()
		case <-t.C:
		}
	}
}

type dispatchClassification struct {
	status   v1alpha2.ScheduledRunExecutionStatus
	message  string
	taskID   string
	terminal bool
}

func classifyDispatchResult(result a2atype.SendMessageResult) (dispatchClassification, error) {
	switch typed := result.(type) {
	case *a2atype.Message:
		return dispatchClassification{
			status:   v1alpha2.ScheduledRunExecutionStatus_Succeeded,
			terminal: true,
		}, nil
	case *a2atype.Task:
		if typed == nil {
			return dispatchClassification{}, fmt.Errorf("agent dispatch returned no result")
		}
		status, message, terminal := executionStatusForTask(typed)
		taskID := string(typed.ID)
		if !terminal && taskID == "" {
			return dispatchClassification{}, fmt.Errorf("agent dispatch returned an asynchronous task without an ID")
		}
		return dispatchClassification{
			status:   status,
			message:  message,
			taskID:   taskID,
			terminal: terminal,
		}, nil
	case nil:
		return dispatchClassification{}, fmt.Errorf("agent dispatch returned no result")
	default:
		return dispatchClassification{}, fmt.Errorf("agent dispatch returned unsupported result %T", result)
	}
}

func executionStatusForTask(task *a2atype.Task) (v1alpha2.ScheduledRunExecutionStatus, string, bool) {
	if task == nil {
		return v1alpha2.ScheduledRunExecutionStatus_InProgress, "", false
	}
	switch task.Status.State {
	case a2atype.TaskStateCompleted:
		return v1alpha2.ScheduledRunExecutionStatus_Succeeded, "", true
	case a2atype.TaskStateFailed, a2atype.TaskStateCanceled, a2atype.TaskStateRejected:
		return v1alpha2.ScheduledRunExecutionStatus_Failed, taskStatusMessage(task), true
	default:
		return v1alpha2.ScheduledRunExecutionStatus_InProgress, "", false
	}
}

func taskStatusMessage(task *a2atype.Task) string {
	if task == nil || task.Status.Message == nil {
		return ""
	}
	for _, part := range task.Status.Message.Parts {
		if text := part.Text(); text != "" {
			return text
		}
	}
	return ""
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return new(value)
}

func executionStatusMessage(value string) *string {
	if value == "" {
		return nil
	}
	runes := []rune(value)
	if len(runes) > v1alpha2.MaxScheduledRunStatusMessageLength {
		value = string(runes[:v1alpha2.MaxScheduledRunStatusMessageLength])
	}
	return &value
}

type scheduledRunDispatchError struct {
	err            error
	sessionCreated bool
}

func (e *scheduledRunDispatchError) Error() string {
	return e.err.Error()
}

func (e *scheduledRunDispatchError) Unwrap() error {
	return e.err
}

// dispatchToTarget is the production dispatchHook: resolve the target, persist the
// session, and send the prompt through the target's A2A route.
func (s *ScheduledRunScheduler) dispatchToTarget(ctx context.Context, sr *v1alpha2.ScheduledRun, sessionID string) (a2atype.SendMessageResult, error) {
	if s.dbClient == nil {
		return nil, fmt.Errorf("database client is not configured")
	}

	_, err := GetTarget(ctx, s.kube, sr.Namespace, sr.Spec.TargetRef)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve target: %w", err)
	}
	targetKey := TargetKey(sr.Namespace, sr.Spec.TargetRef)
	agentRouteKey, err := routeKeyForScheduledRunTarget(sr.Spec.TargetRef.Kind, targetKey)
	if err != nil {
		return nil, err
	}

	userID := SessionUserID
	agentID := utils.ConvertToPythonIdentifier(utils.ResourceRefString(targetKey.Namespace, targetKey.Name))
	storeCtx, storeCancel := context.WithTimeout(ctx, 30*time.Second)
	defer storeCancel()
	if err := s.dbClient.StoreSession(storeCtx, &database.Session{
		ID:      sessionID,
		UserID:  userID,
		AgentID: &agentID,
	}); err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	ctx = pkgauth.AuthSessionTo(ctx, scheduledRunSession{
		principal: pkgauth.Principal{
			User: pkgauth.User{ID: userID},
		},
	})

	message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(sr.Spec.Prompt))
	message.ContextID = sessionID
	result, err := s.agentClients.SendMessageToRoute(ctx, agentRouteKey, &a2atype.SendMessageRequest{Message: message})
	if err != nil {
		return nil, &scheduledRunDispatchError{
			err:            fmt.Errorf("agent dispatch failed: %w", err),
			sessionCreated: true,
		}
	}
	return result, nil
}

func (s *ScheduledRunScheduler) storeExecution(
	ctx context.Context,
	key types.NamespacedName,
	scheduledRunUID string,
	execution v1alpha2.ScheduledRunExecution,
) error {
	if s.dbClient == nil {
		return nil
	}
	var completionTime *time.Time
	if execution.CompletionTime != nil {
		completed := execution.CompletionTime.Time
		completionTime = &completed
	}
	storeCtx, cancel := context.WithTimeout(ctx, statusWriteTimeout)
	defer cancel()
	return s.dbClient.StoreScheduledRunExecution(storeCtx, &database.ScheduledRunExecutionRecord{
		ID:                    execution.ID,
		ScheduledRunNamespace: key.Namespace,
		ScheduledRunName:      key.Name,
		ScheduledRunUID:       scheduledRunUID,
		StartTime:             execution.StartTime.Time,
		CompletionTime:        completionTime,
		Trigger:               execution.Trigger,
		SessionID:             execution.SessionID,
		TaskID:                execution.TaskID,
		Status:                execution.Status,
		StatusMessage:         execution.StatusMessage,
	})
}

type scheduledRunSession struct {
	principal pkgauth.Principal
}

func (s scheduledRunSession) Principal() pkgauth.Principal {
	return s.principal
}

// updateStatusWithRetry refetches the SR and applies mutate, retrying on
// conflict. Status fields are written by both this scheduler (recent
// executions, timing) and the SR controller (Accepted condition); without retry
// the loser's update is silently dropped.
func (s *ScheduledRunScheduler) updateStatusWithRetry(
	ctx context.Context,
	key types.NamespacedName,
	mutate func(*v1alpha2.ScheduledRun),
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var latest v1alpha2.ScheduledRun
		if err := s.kube.Get(ctx, key, &latest); err != nil {
			return err
		}
		mutate(&latest)
		return s.kube.Status().Update(ctx, &latest)
	})
}

// trimRecentExecutions keeps all in-progress executions so their pollers can resume after a
// restart, plus the configured number of completed summary executions. Executions are
// appended in chronological order, so dropping the leading (oldest) completed
// executions retains the most recent ones.
func trimRecentExecutions(sr *v1alpha2.ScheduledRun) {
	limit := RecentExecutionsLimit(sr)
	completed := 0
	for _, execution := range sr.Status.RecentExecutions {
		if execution.Status != v1alpha2.ScheduledRunExecutionStatus_InProgress {
			completed++
		}
	}
	dropCompleted := completed - limit
	if dropCompleted <= 0 {
		return
	}
	kept := sr.Status.RecentExecutions[:0]
	for _, execution := range sr.Status.RecentExecutions {
		if execution.Status != v1alpha2.ScheduledRunExecutionStatus_InProgress && dropCompleted > 0 {
			dropCompleted--
			continue
		}
		kept = append(kept, execution)
	}
	sr.Status.RecentExecutions = kept
}
