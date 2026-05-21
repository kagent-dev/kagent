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
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/robfig/cron/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	agenttranslator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
	"github.com/kagent-dev/kagent/go/core/internal/metrics"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
)

var schedulerLog = ctrl.Log.WithName("scheduledrun-scheduler")

const (
	// messageMaxBytes caps RunHistoryEntry.{Dispatch,Outcome}Message so a flood
	// of long error strings cannot blow past the apiserver's status size limit.
	messageMaxBytes = 1024
	// drainTimeout bounds how long Start() waits for in-flight runs to finish
	// after the manager context is cancelled. Should be less than the pod's
	// terminationGracePeriodSeconds.
	drainTimeout = 25 * time.Second
	// statusWriteTimeout bounds the apiserver write that records a run's
	// outcome.
	statusWriteTimeout = 10 * time.Second
	// outcomePollInterval is the interval between session-state polls when
	// resolving RunOutcome.
	outcomePollInterval = 5 * time.Second
	// outcomePollTimeout is the maximum total time spent polling for a
	// session's terminal state before giving up and recording Outcome=Timeout.
	outcomePollTimeout = 15 * time.Minute
)

// cronLoggerAdapter bridges logr.Logger to robfig/cron's logger interface.
type cronLoggerAdapter struct{ l logr.Logger }

func (a cronLoggerAdapter) Info(msg string, keysAndValues ...interface{}) {
	a.l.Info(msg, keysAndValues...)
}
func (a cronLoggerAdapter) Error(err error, msg string, keysAndValues ...interface{}) {
	a.l.Error(err, msg, keysAndValues...)
}

type ScheduledRunScheduler struct {
	kube       client.Client
	dbClient   database.Client
	cronEngine *cron.Cron

	entriesMu sync.Mutex
	entries   map[types.NamespacedName]cron.EntryID

	// runCtx is the manager's runtime context, captured in Start. Dispatch
	// derives from it so in-flight A2A calls cancel cleanly when the
	// controller is shutting down. Nil before Start; baseCtx falls back to
	// context.Background() in that window.
	runCtxMu sync.RWMutex
	runCtx   context.Context

	// pollersWG tracks outcome-polling goroutines so Start can drain them on
	// shutdown alongside cron jobs.
	pollersWG sync.WaitGroup

	// dispatchHook is the agent invocation; tests override it so they don't
	// need a real A2A server to verify the cron→record-result flow.
	dispatchHook func(ctx context.Context, sr *v1alpha2.ScheduledRun, sessionID string) error
	// outcomePollerHook resolves a session to an Outcome; tests override it
	// (or set it to nil) so they don't need a populated database and so
	// async writes are deterministic.
	outcomePollerHook func(ctx context.Context, sessionID, userID string) (v1alpha2.RunOutcome, string, error)
}

// NewScheduledRunScheduler constructs a scheduler.
func NewScheduledRunScheduler(kube client.Client, dbClient database.Client) *ScheduledRunScheduler {
	cronLogger := cronLoggerAdapter{l: schedulerLog}
	s := &ScheduledRunScheduler{
		kube:     kube,
		dbClient: dbClient,
		// Recover protects the engine: a panic inside any one job no longer
		// kills the whole cron loop.
		cronEngine: cron.New(cron.WithChain(cron.Recover(cronLogger))),
		entries:    make(map[types.NamespacedName]cron.EntryID),
	}
	s.dispatchHook = s.runAgentCall
	s.outcomePollerHook = s.pollSessionOutcome
	return s
}

func (s *ScheduledRunScheduler) NeedLeaderElection() bool {
	return true
}

func (s *ScheduledRunScheduler) Start(ctx context.Context) error {
	schedulerLog.Info("Starting scheduled run scheduler")
	s.runCtxMu.Lock()
	s.runCtx = ctx
	s.runCtxMu.Unlock()

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
	s.runCtxMu.RLock()
	defer s.runCtxMu.RUnlock()
	if s.runCtx == nil {
		return context.Background()
	}
	return s.runCtx
}

// scheduleSpecForCron builds the cron expression handed to robfig/cron,
// embedding the SR's TimeZone via the parser-supported CRON_TZ= prefix
// (parser.go:95 in robfig/cron v3).
func scheduleSpecForCron(sr *v1alpha2.ScheduledRun) string {
	if sr.Spec.TimeZone == "" {
		return sr.Spec.Schedule
	}
	return "CRON_TZ=" + sr.Spec.TimeZone + " " + sr.Spec.Schedule
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
		s.runOnce(key)
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

// userIDInjector is an HTTP RoundTripper that injects user identity headers
// so the agent runtime knows which user the session belongs to.
type userIDInjector struct {
	base   http.RoundTripper
	userID string
}

func (t *userIDInjector) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("X-User-Id", t.userID)
	return t.base.RoundTrip(req)
}

// TriggerManualRun fires a run synchronously through the same code path as
// the cron tick and returns the recorded RunHistoryEntry. Manual triggers
// bypass spec.Suspend by design — Suspend gates only the cron path.
func (s *ScheduledRunScheduler) TriggerManualRun(key types.NamespacedName) (*v1alpha2.RunHistoryEntry, error) {
	entry := s.runOnce(key)
	if entry == nil {
		return nil, fmt.Errorf("scheduled run %s not found", key)
	}
	return entry, nil
}

// runOnce performs a single agent invocation: read the SR, send the prompt,
// append the outcome to RunHistory, and (for successful dispatches) spawn a
// background poller that resolves the session's terminal state into Outcome.
func (s *ScheduledRunScheduler) runOnce(key types.NamespacedName) *v1alpha2.RunHistoryEntry {
	log := schedulerLog.WithValues("scheduledRun", key)
	ctx := s.baseCtx()

	var sr v1alpha2.ScheduledRun
	if err := s.kube.Get(ctx, key, &sr); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "Failed to fetch ScheduledRun")
		}
		return nil
	}

	sessionID := protocol.GenerateContextID()
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
		StartTime:      startTime,
		CompletionTime: &completionTime,
		SessionID:      sessionID,
		DispatchStatus: v1alpha2.DispatchStatusDispatched,
	}
	if dispatchErr != nil {
		log.Error(dispatchErr, "Scheduled run failed")
		entry.DispatchStatus = v1alpha2.DispatchStatusFailed
		entry.DispatchMessage = truncate(dispatchErr.Error(), messageMaxBytes)
	} else {
		// Dispatched runs await async outcome resolution.
		entry.Outcome = v1alpha2.RunOutcomePending
	}

	metrics.ObserveScheduledRunDispatch(
		key.Namespace, key.Name, string(entry.DispatchStatus),
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
		if sched, err := cron.ParseStandard(scheduleSpecForCron(latest)); err == nil {
			next := metav1.NewTime(sched.Next(completionTime.Time))
			latest.Status.NextRunTime = &next
		}
	}); err != nil {
		log.Error(err, "Failed to record run outcome")
	}

	if entry.DispatchStatus == v1alpha2.DispatchStatusDispatched {
		// Tests can disable async outcome polling by clearing the hook so
		// RunHistory entries stay deterministic at Outcome=Pending.
		if s.outcomePollerHook != nil {
			s.spawnOutcomePoller(key, sessionID, sessionUserID(&sr))
		}
	} else {
		// Failed dispatches resolve immediately to a Failed outcome for metrics
		// purposes — no session was created so polling would be meaningless.
		metrics.ObserveScheduledRunOutcome(key.Namespace, key.Name, string(v1alpha2.RunOutcomeFailed))
	}

	return &entry
}

// spawnOutcomePoller resolves the session's terminal state asynchronously and
// updates the matching RunHistoryEntry. Match is by SessionID, not by index,
// because RunHistory may be trimmed before polling completes.
func (s *ScheduledRunScheduler) spawnOutcomePoller(key types.NamespacedName, sessionID, userID string) {
	s.pollersWG.Add(1)
	go func() {
		defer s.pollersWG.Done()
		log := schedulerLog.WithValues("scheduledRun", key, "sessionID", sessionID)

		pollCtx, cancel := context.WithTimeout(s.baseCtx(), outcomePollTimeout)
		defer cancel()

		outcome, msg, err := s.outcomePollerHook(pollCtx, sessionID, userID)
		if err != nil {
			log.Error(err, "Outcome polling failed")
			outcome = v1alpha2.RunOutcomeTimeout
			msg = err.Error()
		}

		now := metav1.Now()
		writeCtx, writeCancel := context.WithTimeout(context.Background(), statusWriteTimeout)
		defer writeCancel()
		if err := s.updateStatusWithRetry(writeCtx, key, func(latest *v1alpha2.ScheduledRun) {
			for i := range latest.Status.RunHistory {
				if latest.Status.RunHistory[i].SessionID == sessionID {
					latest.Status.RunHistory[i].Outcome = outcome
					latest.Status.RunHistory[i].OutcomeMessage = truncate(msg, messageMaxBytes)
					latest.Status.RunHistory[i].OutcomeTime = &now
					break
				}
			}
		}); err != nil {
			log.Error(err, "Failed to write outcome")
		}
		metrics.ObserveScheduledRunOutcome(key.Namespace, key.Name, string(outcome))
	}()
}

// pollSessionOutcome polls the session's task list until a terminal state
// is observed. Returns Succeeded for completed, Failed for the negative
// terminal states, and Timeout if the deadline elapses first.
func (s *ScheduledRunScheduler) pollSessionOutcome(ctx context.Context, sessionID, userID string) (v1alpha2.RunOutcome, string, error) {
	t := time.NewTicker(outcomePollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return v1alpha2.RunOutcomeTimeout, "polling deadline exceeded", nil
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
			case protocol.TaskStateCompleted:
				return v1alpha2.RunOutcomeSucceeded, "", nil
			case protocol.TaskStateFailed, protocol.TaskStateCanceled, protocol.TaskStateRejected:
				msg := ""
				if task.Status.Message != nil {
					for _, p := range task.Status.Message.Parts {
						if tp, ok := p.(*protocol.TextPart); ok {
							msg = tp.Text
							break
						}
					}
				}
				return v1alpha2.RunOutcomeFailed, msg, nil
			}
		}
	}
}

// runAgentCall is the production dispatchHook: persist the session, resolve
// the agent's A2A endpoint, and send the prompt.
func (s *ScheduledRunScheduler) runAgentCall(ctx context.Context, sr *v1alpha2.ScheduledRun, sessionID string) error {
	agentNS := sr.Spec.AgentRef.Namespace
	if agentNS == "" {
		agentNS = sr.Namespace
	}
	agentID := utils.ConvertToPythonIdentifier(utils.ResourceRefString(agentNS, sr.Spec.AgentRef.Name))

	userID := sessionUserID(sr)

	storeCtx, storeCancel := context.WithTimeout(ctx, 30*time.Second)
	defer storeCancel()
	if err := s.dbClient.StoreSession(storeCtx, &database.Session{
		ID:      sessionID,
		UserID:  userID,
		AgentID: &agentID,
	}); err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	var agent v1alpha2.Agent
	if err := s.kube.Get(ctx, types.NamespacedName{Namespace: agentNS, Name: sr.Spec.AgentRef.Name}, &agent); err != nil {
		return fmt.Errorf("failed to fetch agent: %w", err)
	}
	agentURL := agenttranslator.GetA2AAgentCard(&agent).URL

	cli, err := a2aclient.NewA2AClient(
		agentURL,
		a2aclient.WithTimeout(5*time.Minute),
		a2aclient.WithHTTPClient(&http.Client{
			Transport: &userIDInjector{
				base:   http.DefaultTransport,
				userID: userID,
			},
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to create A2A client: %w", err)
	}

	if _, err := cli.SendMessage(ctx, protocol.SendMessageParams{
		Message: protocol.Message{
			Kind:      protocol.KindMessage,
			Role:      protocol.MessageRoleUser,
			ContextID: &sessionID,
			Parts:     []protocol.Part{protocol.NewTextPart(sr.Spec.Prompt)},
		},
	}); err != nil {
		return fmt.Errorf("agent invocation failed: %w", err)
	}
	return nil
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
