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

package controller

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
	scheduledrunutil "github.com/kagent-dev/kagent/go/core/internal/scheduledrun"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
)

var schedulerLog = ctrl.Log.WithName("scheduledrun-scheduler")

const (
	// messageMaxBytes caps RunHistoryEntry.Message so a flood of long error
	// strings cannot blow past the apiserver's status size limit.
	messageMaxBytes = 1024
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
	// outcomePollInterval is the interval between session-state polls when
	// resolving the run's terminal RunStatus.
	outcomePollInterval = 5 * time.Second
	// outcomePollTimeout is the maximum total time spent polling for a
	// session's terminal state before giving up and recording Outcome=Timeout.
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
	// need a real A2A server to verify the cron→record-result flow.
	dispatchHook func(ctx context.Context, sr *v1alpha2.ScheduledRun, sessionID string) error
	// outcomePollerHook resolves a session to a terminal RunStatus; tests
	// override it (or set it to nil) so they don't need a populated database
	// and so async writes are deterministic.
	outcomePollerHook func(ctx context.Context, sessionID, userID string) (v1alpha2.RunStatus, string, error)
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
	s.outcomePollerHook = s.pollSessionOutcome
	return s, nil
}

func (s *ScheduledRunScheduler) NeedLeaderElection() bool {
	return true
}

func (s *ScheduledRunScheduler) Start(ctx context.Context) error {
	schedulerLog.Info("Starting scheduled run scheduler")
	s.runCtx.Store(&ctx)

	s.cronEngine.Start()
	s.resumePendingPollers(ctx)
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

// resumePendingPollers re-spawns outcome pollers for any Pending RunHistory
// entries left behind by a previous controller instance. Without this, a
// restart between dispatch and terminal resolution leaves entries stuck at
// Pending forever — the in-memory poller goroutine died with the pod.
func (s *ScheduledRunScheduler) resumePendingPollers(ctx context.Context) {
	if s.outcomePollerHook == nil {
		return
	}
	var list v1alpha2.ScheduledRunList
	if err := s.kube.List(ctx, &list); err != nil {
		schedulerLog.Error(err, "Failed to list ScheduledRuns for poller resume")
		return
	}
	for i := range list.Items {
		sr := &list.Items[i]
		key := types.NamespacedName{Namespace: sr.Namespace, Name: sr.Name}
		userID := sessionUserID(sr)
		for _, entry := range sr.Status.RunHistory {
			if entry.Status == v1alpha2.RunStatusPending && entry.SessionID != "" && entry.EndTime == nil {
				s.spawnOutcomePoller(key, entry.SessionID, userID)
			}
		}
	}
}

// scheduleSpecForCron builds the cron expression handed to robfig/cron,
// embedding the SR's TimeZone via the parser-supported CRON_TZ= prefix
// (parser.go:95 in robfig/cron v3).
func scheduleSpecForCron(sr *v1alpha2.ScheduledRun) string {
	return "CRON_TZ=" + scheduledRunTimeZone(sr) + " " + strings.TrimSpace(sr.Spec.Schedule)
}

func scheduledRunTimeZone(sr *v1alpha2.ScheduledRun) string {
	if timeZone := strings.TrimSpace(sr.Spec.TimeZone); timeZone != "" {
		return timeZone
	}
	return v1alpha2.DefaultScheduledRunTimeZone
}

func routeKeyForScheduledRunTarget(kind v1alpha2.AgentReferenceKind, key types.NamespacedName) (string, error) {
	switch kind {
	case "", v1alpha2.AgentReferenceKindAgent:
		return a2a.RouteKeyForAgent(key.Namespace, key.Name), nil
	case v1alpha2.AgentReferenceKindSandboxAgent:
		return a2a.RouteKeyForSandboxAgent(key.Namespace, key.Name), nil
	default:
		return "", fmt.Errorf("unsupported agentRef.kind %q", kind)
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

	entryID, err := s.cronEngine.AddFunc(scheduleSpecForCron(sr), func() {
		if _, err := s.runOnce(key); err != nil &&
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

// TriggerManualRun fires a run synchronously through the same code path as
// the cron tick and returns the recorded RunHistoryEntry. Suspended schedules
// cannot be triggered manually.
func (s *ScheduledRunScheduler) TriggerManualRun(key types.NamespacedName) (*v1alpha2.RunHistoryEntry, error) {
	entry, err := s.runOnce(key)
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// runOnce performs a single agent invocation: read the SR, send the prompt,
// append the outcome to RunHistory, and (for successful dispatches) spawn a
// background poller that resolves the session's terminal state into Outcome.
func (s *ScheduledRunScheduler) runOnce(key types.NamespacedName) (*v1alpha2.RunHistoryEntry, error) {
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
	if sr.Spec.Suspend {
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

	// Recover from panics inside dispatchHook so the run still ends up in
	// RunHistory as a Failed entry instead of vanishing into the cron
	// engine's recovery handler.
	var dispatchErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				dispatchErr = fmt.Errorf("dispatch panic: %v", r)
				log.Error(dispatchErr, "Recovered from dispatch panic")
			}
		}()
		dispatchErr = s.dispatchHook(ctx, &sr, sessionID)
	}()

	completionTime := metav1.Now()
	entry := v1alpha2.RunHistoryEntry{
		StartTime: startTime,
		SessionID: sessionID,
		Status:    v1alpha2.RunStatusPending,
	}
	if dispatchErr != nil {
		log.Error(dispatchErr, "Scheduled run failed")
		entry.Status = v1alpha2.RunStatusDispatchFailed
		entry.Message = truncate(dispatchErr.Error(), messageMaxBytes)
		entry.EndTime = &completionTime
	}

	metrics.ObserveScheduledRunDispatch(
		key.Namespace, key.Name, string(entry.Status),
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
		} else if sched, err := cron.ParseStandard(scheduleSpecForCron(latest)); err == nil {
			next := metav1.NewTime(sched.Next(completionTime.Time))
			latest.Status.NextRunTime = &next
		}
	}); err != nil {
		// Status write failed — the entry is not in RunHistory, so don't
		// spawn the outcome poller (it would never find a matching SessionID
		// to update) and surface the error to the caller. Manual triggers
		// must not report success when the run was never recorded.
		log.Error(err, "Failed to record run outcome")
		return nil, fmt.Errorf("failed to record run outcome for %s: %w", key, err)
	}

	if entry.Status == v1alpha2.RunStatusPending {
		// Tests can disable async outcome polling by clearing the hook so
		// RunHistory entries stay deterministic at Status=Pending.
		if s.outcomePollerHook != nil {
			s.spawnOutcomePoller(key, sessionID, sessionUserID(&sr))
		}
	}
	// DispatchFailed runs are already terminal in metrics terms: the
	// dispatch counter above carried the status label, so no separate
	// outcome counter is needed.

	return &entry, nil
}

// spawnOutcomePoller resolves the session's terminal state asynchronously and
// updates the matching RunHistoryEntry. Match is by SessionID, not by index,
// because RunHistory may be trimmed before polling completes.
func (s *ScheduledRunScheduler) spawnOutcomePoller(key types.NamespacedName, sessionID, userID string) {
	s.pollersWG.Go(func() {
		log := schedulerLog.WithValues("scheduledRun", key, "sessionID", sessionID)

		pollCtx, cancel := context.WithTimeout(s.baseCtx(), outcomePollTimeout)
		defer cancel()

		status, msg, err := s.outcomePollerHook(pollCtx, sessionID, userID)
		if err != nil {
			log.Error(err, "Outcome polling failed")
			status = v1alpha2.RunStatusTimeout
			msg = err.Error()
		}

		now := metav1.Now()
		writeCtx, writeCancel := context.WithTimeout(context.Background(), statusWriteTimeout)
		defer writeCancel()
		if err := s.updateStatusWithRetry(writeCtx, key, func(latest *v1alpha2.ScheduledRun) {
			for i := range latest.Status.RunHistory {
				if latest.Status.RunHistory[i].SessionID == sessionID {
					latest.Status.RunHistory[i].Status = status
					latest.Status.RunHistory[i].Message = truncate(msg, messageMaxBytes)
					latest.Status.RunHistory[i].EndTime = &now
					break
				}
			}
		}); err != nil {
			log.Error(err, "Failed to write outcome")
		}
		metrics.ObserveScheduledRunOutcome(key.Namespace, key.Name, string(status))
	})
}

// pollSessionOutcome polls the session's task list until a terminal state
// is observed. Returns Succeeded for completed, Failed for the negative
// terminal states, and Timeout if the deadline elapses first.
func (s *ScheduledRunScheduler) pollSessionOutcome(ctx context.Context, sessionID, userID string) (v1alpha2.RunStatus, string, error) {
	if s.dbClient == nil {
		return v1alpha2.RunStatusTimeout, "", fmt.Errorf("database client is not configured")
	}

	t := time.NewTicker(outcomePollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return v1alpha2.RunStatusTimeout, "polling deadline exceeded", nil
		case <-t.C:
		}
		tasks, err := s.dbClient.ListTasksForSession(ctx, sessionID)
		if err != nil {
			// Session row may not exist yet (race with StoreSession commit) —
			// keep polling rather than treating transient errors as terminal.
			continue
		}
		for _, task := range tasks {
			switch task.Status.State {
			case a2atype.TaskStateCompleted:
				return v1alpha2.RunStatusSucceeded, "", nil
			case a2atype.TaskStateFailed, a2atype.TaskStateCanceled, a2atype.TaskStateRejected:
				msg := ""
				if task.Status.Message != nil {
					for _, p := range task.Status.Message.Parts {
						if text := p.Text(); text != "" {
							msg = text
							break
						}
					}
				}
				return v1alpha2.RunStatusFailed, msg, nil
			}
		}
	}
}

// runAgentCall is the production dispatchHook: persist the session, resolve
// the agent's A2A endpoint, and send the prompt.
func (s *ScheduledRunScheduler) runAgentCall(ctx context.Context, sr *v1alpha2.ScheduledRun, sessionID string) error {
	if err := scheduledrunutil.ValidateSameNamespace(sr.Namespace, sr.Spec.AgentRef); err != nil {
		return err
	}
	agentKey := scheduledrunutil.TargetKey(sr.Namespace, sr.Spec.AgentRef)
	agentKind := scheduledrunutil.TargetKind(sr.Spec.AgentRef)
	agentID := utils.ConvertToPythonIdentifier(utils.ResourceRefString(agentKey.Namespace, agentKey.Name))

	userID := sessionUserID(sr)
	if s.dbClient == nil {
		return fmt.Errorf("database client is not configured")
	}

	storeCtx, storeCancel := context.WithTimeout(ctx, 30*time.Second)
	defer storeCancel()
	if err := s.dbClient.StoreSession(storeCtx, &database.Session{
		ID:      sessionID,
		UserID:  userID,
		AgentID: &agentID,
	}); err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	if _, err := scheduledrunutil.GetTarget(ctx, s.kube, sr.Namespace, sr.Spec.AgentRef); err != nil {
		return fmt.Errorf("failed to fetch %s: %w", agentKind, err)
	}
	agentRouteKey, err := routeKeyForScheduledRunTarget(agentKind, agentKey)
	if err != nil {
		return err
	}

	ctx = pkgauth.AuthSessionTo(ctx, scheduledRunSession{
		principal: pkgauth.Principal{
			User: pkgauth.User{ID: userID},
		},
	})

	message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(sr.Spec.Prompt))
	message.ContextID = sessionID
	if _, err := s.agentClients.SendMessageToRoute(ctx, agentRouteKey, &a2atype.SendMessageRequest{Message: message}); err != nil {
		return fmt.Errorf("agent invocation failed: %w", err)
	}
	return nil
}

type scheduledRunSession struct {
	principal pkgauth.Principal
}

func (s scheduledRunSession) Principal() pkgauth.Principal {
	return s.principal
}

func sessionUserID(sr *v1alpha2.ScheduledRun) string {
	if v := sr.Annotations[v1alpha2.AnnotationCreatedBy]; v != "" {
		return v
	}
	return "scheduled-run"
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
		maxHistory = 10
	}
	if len(sr.Status.RunHistory) > maxHistory {
		sr.Status.RunHistory = sr.Status.RunHistory[len(sr.Status.RunHistory)-maxHistory:]
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…(truncated)"
}
