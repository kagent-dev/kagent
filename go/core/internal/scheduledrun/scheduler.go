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
	"github.com/kagent-dev/kagent/go/core/internal/metrics"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
)

var schedulerLog = ctrl.Log.WithName("scheduledrun-scheduler")

const (
	// drainTimeout bounds how long Start() waits for in-flight runs to finish
	// after the manager context is cancelled. Should be less than the pod's
	// terminationGracePeriodSeconds.
	drainTimeout = 25 * time.Second
	// statusWriteTimeout bounds the apiserver write that records a run's
	// outcome.
	statusWriteTimeout = 10 * time.Second
)

// Poll cadence is exposed as vars so tests can shrink them without waiting on
// the production 5s/15m schedule. Production code never reassigns these.
var (
	// outcomePollInterval is the interval between outcome polls when
	// resolving the run's terminal RunStatus.
	outcomePollInterval = 5 * time.Second
	// outcomePollTimeout is the maximum total time spent polling for a
	// task terminal state before recording Status=Timeout.
	outcomePollTimeout = 15 * time.Minute
)

var (
	errScheduledRunNotFound  = errors.New("scheduled run not found")
	errScheduledRunSuspended = errors.New("scheduled run is suspended")
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

	entriesMu sync.Mutex
	entries   map[types.NamespacedName]cron.EntryID

	// runCtx is the manager's runtime context, stored once in Start. Dispatch
	// derives from it so in-flight A2A calls cancel cleanly when the
	// controller is shutting down. Nil before Start; baseCtx falls back to
	// context.Background() in that window.
	runCtx atomic.Pointer[context.Context]

	// pollersWG tracks outcome-polling goroutines so Start can drain them on
	// shutdown alongside cron jobs.
	pollersWG sync.WaitGroup

	// dispatchHook is the agent invocation; tests override it so they don't
	// need a real A2A server to verify the cron-to-record-result flow.
	dispatchHook func(ctx context.Context, sr *v1alpha2.ScheduledRun, sessionID string) (a2atype.SendMessageResult, error)
	// outcomePollerHook resolves an asynchronous task to a terminal RunStatus.
	// Tests can replace or disable it to keep history writes deterministic.
	outcomePollerHook func(ctx context.Context, routeKey, taskID string) (v1alpha2.RunStatus, string)
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
		cronEngine: cron.New(cron.WithChain(cron.Recover(cronLogger))),
		entries:    make(map[types.NamespacedName]cron.EntryID),
	}
	s.dispatchHook = s.runAgentCall
	s.outcomePollerHook = s.pollRunOutcome
	return s, nil
}

func (s *ScheduledRunScheduler) NeedLeaderElection() bool {
	return true
}

func (s *ScheduledRunScheduler) Start(ctx context.Context) error {
	schedulerLog.Info("Starting scheduled run scheduler")
	s.runCtx.Store(&ctx)

	s.cronEngine.Start()
	<-ctx.Done()

	schedulerLog.Info("Stopping scheduled run scheduler, draining in-flight runs")
	stopCtx := s.cronEngine.Stop()
	select {
	case <-stopCtx.Done():
		schedulerLog.Info("All in-flight cron runs drained")
	case <-time.After(drainTimeout):
		schedulerLog.Info("Drain timeout exceeded, abandoning in-flight runs",
			"timeout", drainTimeout)
	}

	// Wait for outcome pollers (already context-cancelled) to return.
	pollersDone := make(chan struct{})
	go func() {
		s.pollersWG.Wait()
		close(pollersDone)
	}()
	select {
	case <-pollersDone:
	case <-time.After(drainTimeout):
	}
	return nil
}

// baseCtx returns the manager's runtime context (set in Start) so dispatch
// and apiserver calls cancel cleanly when the controller stops. Returns
// context.Background() before Start has run (e.g. unit tests that don't
// drive the full manager lifecycle).
func (s *ScheduledRunScheduler) baseCtx() context.Context {
	if ctx := s.runCtx.Load(); ctx != nil {
		return *ctx
	}
	return context.Background()
}

// ScheduleSpecForCron builds the cron expression handed to robfig/cron,
// embedding the SR's TimeZone via the parser-supported CRON_TZ= prefix
// (parser.go:95 in robfig/cron v3).
func ScheduleSpecForCron(sr *v1alpha2.ScheduledRun) string {
	return "CRON_TZ=" + ScheduledRunTimeZone(sr) + " " + strings.TrimSpace(sr.Spec.Schedule)
}

func ScheduledRunTimeZone(sr *v1alpha2.ScheduledRun) string {
	if timeZone := strings.TrimSpace(sr.Spec.TimeZone); timeZone != "" {
		return timeZone
	}
	return v1alpha2.DefaultScheduledRunTimeZone
}

func routeKeyForScheduledRunTarget(kind string, key types.NamespacedName) (string, error) {
	switch kind {
	case v1alpha2.ScheduledRunTargetKindAgent:
		return a2a.RouteKeyForAgent(key.Namespace, key.Name), nil
	case v1alpha2.ScheduledRunTargetKindSandboxAgent:
		return a2a.RouteKeyForSandboxAgent(key.Namespace, key.Name), nil
	default:
		return "", fmt.Errorf("unsupported targetRef.kind %q", kind)
	}
}

func (s *ScheduledRunScheduler) UpdateSchedule(sr *v1alpha2.ScheduledRun) error {
	s.entriesMu.Lock()
	defer s.entriesMu.Unlock()

	key := types.NamespacedName{Name: sr.Name, Namespace: sr.Namespace}

	if existingID, ok := s.entries[key]; ok {
		s.cronEngine.Remove(existingID)
		delete(s.entries, key)
	}

	if sr.Spec.Suspend {
		metrics.SetActiveSchedules(len(s.entries))
		return nil
	}

	entryID, err := s.cronEngine.AddFunc(ScheduleSpecForCron(sr), func() {
		if _, err := s.runOnce(key, false); err != nil &&
			!errors.Is(err, errScheduledRunNotFound) &&
			!errors.Is(err, errScheduledRunSuspended) {
			schedulerLog.Error(err, "Scheduled run tick failed", "scheduledRun", key)
		}
	})
	if err != nil {
		metrics.SetActiveSchedules(len(s.entries))
		return fmt.Errorf("failed to add cron schedule for %s: %w", key, err)
	}

	s.entries[key] = entryID
	metrics.SetActiveSchedules(len(s.entries))
	return nil
}

func (s *ScheduledRunScheduler) RemoveSchedule(key types.NamespacedName) {
	s.entriesMu.Lock()
	defer s.entriesMu.Unlock()

	if existingID, ok := s.entries[key]; ok {
		s.cronEngine.Remove(existingID)
		delete(s.entries, key)
	}
	metrics.SetActiveSchedules(len(s.entries))
}

func (s *ScheduledRunScheduler) HasSchedule(key types.NamespacedName) bool {
	s.entriesMu.Lock()
	defer s.entriesMu.Unlock()
	_, ok := s.entries[key]
	return ok
}

// TriggerManualRun fires a run synchronously through the same dispatch/history
// path as the cron tick and returns the recorded RunHistoryEntry. A suspended
// schedule may still be manually triggered; suspend only pauses automatic ticks.
func (s *ScheduledRunScheduler) TriggerManualRun(key types.NamespacedName) (*v1alpha2.RunHistoryEntry, error) {
	return s.runOnce(key, true)
}

// runOnce performs a single target invocation, records its immediate A2A result,
// and starts a background poller only when the agent returns a non-terminal Task.
func (s *ScheduledRunScheduler) runOnce(key types.NamespacedName, manual bool) (*v1alpha2.RunHistoryEntry, error) {
	log := schedulerLog.WithValues("scheduledRun", key)
	ctx := s.baseCtx()

	var sr v1alpha2.ScheduledRun
	if err := s.kube.Get(ctx, key, &sr); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %s", errScheduledRunNotFound, key)
		}
		log.Error(err, "Failed to fetch ScheduledRun")
		return nil, fmt.Errorf("failed to fetch ScheduledRun %s: %w", key, err)
	}
	if sr.Spec.Suspend && !manual {
		log.Info("Skipping suspended ScheduledRun")
		writeCtx, cancel := context.WithTimeout(context.Background(), statusWriteTimeout)
		defer cancel()
		if err := s.updateStatusWithRetry(writeCtx, key, func(latest *v1alpha2.ScheduledRun) {
			latest.Status.NextRunTime = nil
		}); err != nil {
			log.Error(err, "Failed to clear next run time for suspended ScheduledRun")
		}
		return nil, fmt.Errorf("%w: %s", errScheduledRunSuspended, key)
	}

	sessionID := a2atype.NewContextID()
	startTime := metav1.Now()
	dispatchStart := time.Now()

	dispatchResult, dispatchErr := s.dispatchHook(ctx, &sr, sessionID)

	completionTime := metav1.Now()
	entry := v1alpha2.RunHistoryEntry{
		StartTime: startTime,
		Status:    v1alpha2.RunStatusInProgress,
	}
	var outcome sendMessageOutcome
	if dispatchErr != nil {
		log.Error(dispatchErr, "Scheduled run failed")
		entry.Status = v1alpha2.RunStatusDispatchFailed
		entry.Message = dispatchErr.Error()
		entry.EndTime = &completionTime
	} else {
		entry.SessionID = sessionID
		classified, err := classifySendMessageResult(dispatchResult)
		if err != nil {
			entry.SessionID = ""
			entry.Status = v1alpha2.RunStatusDispatchFailed
			entry.Message = err.Error()
			entry.EndTime = &completionTime
		} else {
			outcome = classified
			entry.Status = outcome.status
			entry.Message = outcome.message
			if outcome.terminal {
				entry.EndTime = &completionTime
			}
		}
	}

	metrics.ObserveScheduledRunDispatch(
		string(entry.Status),
		time.Since(dispatchStart).Seconds(),
	)

	// Status writes use a fresh bounded ctx so the outcome is recorded even
	// when the manager ctx has been cancelled (graceful shutdown path).
	writeCtx, cancel := context.WithTimeout(context.Background(), statusWriteTimeout)
	defer cancel()
	if err := s.updateStatusWithRetry(writeCtx, key, func(latest *v1alpha2.ScheduledRun) {
		latest.Status.LastRunTime = &startTime
		latest.Status.RunHistory = append(latest.Status.RunHistory, entry)
		trimRunHistory(latest)
		// Advance NextRunTime here so it doesn't sit stale at the value
		// computed by the last reconcile (which may now be in the past).
		if latest.Spec.Suspend {
			latest.Status.NextRunTime = nil
		} else if sched, err := cron.ParseStandard(ScheduleSpecForCron(latest)); err == nil {
			next := metav1.NewTime(sched.Next(completionTime.Time))
			latest.Status.NextRunTime = &next
		}
	}); err != nil {
		// Status write failed: the entry is not in RunHistory, so don't
		// spawn the outcome poller (it would never find a matching SessionID
		// to update) and surface the error to the caller. Manual triggers
		// must not report success when the run was never recorded.
		log.Error(err, "Failed to record run outcome")
		return nil, fmt.Errorf("failed to record run outcome for %s: %w", key, err)
	}

	if entry.Status == v1alpha2.RunStatusInProgress {
		// Tests can disable async outcome polling by clearing the hook so
		// RunHistory entries stay deterministic at Status=InProgress.
		if s.outcomePollerHook != nil {
			routeKey, err := routeKeyForScheduledRunTarget(TargetKind(sr.Spec.TargetRef), TargetKey(sr.Namespace, sr.Spec.TargetRef))
			if err != nil {
				log.Error(err, "Unable to poll scheduled run task")
			} else {
				s.spawnOutcomePoller(key, entry.SessionID, routeKey, outcome.taskID)
			}
		}
	}
	// DispatchFailed runs are already terminal in metrics terms: the
	// dispatch counter above carried the status label, so no separate
	// outcome counter is needed.

	return &entry, nil
}

// spawnOutcomePoller resolves an asynchronous task and updates its history entry.
func (s *ScheduledRunScheduler) spawnOutcomePoller(key types.NamespacedName, sessionID, routeKey, taskID string) {
	s.pollersWG.Add(1)
	//nolint:modernize // Explicit Add/Done is intentional per review feedback.
	go func() {
		defer s.pollersWG.Done()

		log := schedulerLog.WithValues("scheduledRun", key, "sessionID", sessionID, "taskID", taskID)

		pollCtx, cancel := context.WithTimeout(s.baseCtx(), outcomePollTimeout)
		defer cancel()

		status, msg := s.outcomePollerHook(pollCtx, routeKey, taskID)

		now := metav1.Now()
		writeCtx, writeCancel := context.WithTimeout(context.Background(), statusWriteTimeout)
		defer writeCancel()
		if err := s.updateStatusWithRetry(writeCtx, key, func(latest *v1alpha2.ScheduledRun) {
			for i := range latest.Status.RunHistory {
				if latest.Status.RunHistory[i].SessionID == sessionID {
					latest.Status.RunHistory[i].Status = status
					latest.Status.RunHistory[i].Message = msg
					latest.Status.RunHistory[i].EndTime = &now
					break
				}
			}
		}); err != nil {
			log.Error(err, "Failed to write outcome")
		}
		metrics.ObserveScheduledRunOutcome(string(status))
	}()
}

// pollRunOutcome polls the A2A task until it reaches a terminal state.
func (s *ScheduledRunScheduler) pollRunOutcome(ctx context.Context, routeKey, taskID string) (v1alpha2.RunStatus, string) {
	pollCtx := pkgauth.AuthSessionTo(ctx, scheduledRunSession{
		principal: pkgauth.Principal{User: pkgauth.User{ID: SessionUserID}},
	})
	t := time.NewTicker(outcomePollInterval)
	defer t.Stop()
	for {
		task, err := s.agentClients.GetTaskFromRoute(pollCtx, routeKey, &a2atype.GetTaskRequest{ID: a2atype.TaskID(taskID)})
		if err == nil {
			if status, msg, terminal := runStatusForTask(task); terminal {
				return status, msg
			}
		}

		select {
		case <-ctx.Done():
			return v1alpha2.RunStatusTimeout, "polling deadline exceeded"
		case <-t.C:
		}
	}
}

type sendMessageOutcome struct {
	status   v1alpha2.RunStatus
	message  string
	taskID   string
	terminal bool
}

func classifySendMessageResult(result a2atype.SendMessageResult) (sendMessageOutcome, error) {
	switch typed := result.(type) {
	case *a2atype.Message:
		return sendMessageOutcome{
			status:   v1alpha2.RunStatusSucceeded,
			terminal: true,
		}, nil
	case *a2atype.Task:
		if typed == nil {
			return sendMessageOutcome{}, fmt.Errorf("agent invocation returned no result")
		}
		status, message, terminal := runStatusForTask(typed)
		taskID := string(typed.ID)
		if !terminal && taskID == "" {
			return sendMessageOutcome{}, fmt.Errorf("agent invocation returned an asynchronous task without an ID")
		}
		return sendMessageOutcome{
			status:   status,
			message:  message,
			taskID:   taskID,
			terminal: terminal,
		}, nil
	case nil:
		return sendMessageOutcome{}, fmt.Errorf("agent invocation returned no result")
	default:
		return sendMessageOutcome{}, fmt.Errorf("agent invocation returned unsupported result %T", result)
	}
}

func runStatusForTask(task *a2atype.Task) (v1alpha2.RunStatus, string, bool) {
	if task == nil {
		return v1alpha2.RunStatusInProgress, "", false
	}
	switch task.Status.State {
	case a2atype.TaskStateCompleted:
		return v1alpha2.RunStatusSucceeded, "", true
	case a2atype.TaskStateFailed, a2atype.TaskStateCanceled, a2atype.TaskStateRejected:
		return v1alpha2.RunStatusFailed, taskStatusMessage(task), true
	default:
		return v1alpha2.RunStatusInProgress, "", false
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

// runAgentCall is the production dispatchHook: resolve the target, persist the
// session, and send the prompt through the target's A2A route.
func (s *ScheduledRunScheduler) runAgentCall(ctx context.Context, sr *v1alpha2.ScheduledRun, sessionID string) (a2atype.SendMessageResult, error) {
	if s.dbClient == nil {
		return nil, fmt.Errorf("database client is not configured")
	}

	target, err := GetTarget(ctx, s.kube, sr.Namespace, sr.Spec.TargetRef)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve target: %w", err)
	}
	if err := ValidateTargetNamespaceAccess(ctx, s.kube, sr.Namespace, target); err != nil {
		return nil, err
	}

	targetKind := TargetKind(sr.Spec.TargetRef)
	targetKey := TargetKey(sr.Namespace, sr.Spec.TargetRef)
	agentRouteKey, err := routeKeyForScheduledRunTarget(targetKind, targetKey)
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
		return nil, fmt.Errorf("agent invocation failed: %w", err)
	}
	return result, nil
}

type scheduledRunSession struct {
	principal pkgauth.Principal
}

func (s scheduledRunSession) Principal() pkgauth.Principal {
	return s.principal
}

// updateStatusWithRetry refetches the SR and applies mutate, retrying on
// conflict. Status fields are written by both this scheduler (run history,
// timing) and the SR controller (Accepted condition); without retry the
// loser's update is silently dropped.
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

// trimRunHistory keeps the most recent MaxRunHistory entries. The CRD default
// (10) is applied by the apiserver in production, but unit tests with the
// fake client construct SR objects directly without admission, so we keep
// the runtime fallback to the same value.
func trimRunHistory(sr *v1alpha2.ScheduledRun) {
	maxHistory := sr.Spec.MaxRunHistory
	if maxHistory <= 0 {
		maxHistory = v1alpha2.DefaultScheduledRunMaxRunHistory
	}
	if len(sr.Status.RunHistory) > maxHistory {
		sr.Status.RunHistory = sr.Status.RunHistory[len(sr.Status.RunHistory)-maxHistory:]
	}
}
