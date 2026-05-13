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

	"github.com/robfig/cron/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

var schedulerLog = ctrl.Log.WithName("scheduledrun-scheduler")

type ScheduledRunScheduler struct {
	kube       client.Client
	dbClient   database.Client
	cronEngine *cron.Cron
	mu         sync.Mutex
	entries    map[types.NamespacedName]cron.EntryID
}

func NewScheduledRunScheduler(kube client.Client, dbClient database.Client) *ScheduledRunScheduler {
	return &ScheduledRunScheduler{
		kube:       kube,
		dbClient:   dbClient,
		cronEngine: cron.New(),
		entries:    make(map[types.NamespacedName]cron.EntryID),
	}
}

func (s *ScheduledRunScheduler) NeedLeaderElection() bool {
	return true
}

func (s *ScheduledRunScheduler) Start(ctx context.Context) error {
	schedulerLog.Info("Starting scheduled run scheduler")
	s.cronEngine.Start()
	<-ctx.Done()
	schedulerLog.Info("Stopping scheduled run scheduler")
	s.cronEngine.Stop()
	return nil
}

func (s *ScheduledRunScheduler) UpdateSchedule(sr *v1alpha2.ScheduledRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := types.NamespacedName{Name: sr.Name, Namespace: sr.Namespace}

	if existingID, ok := s.entries[key]; ok {
		s.cronEngine.Remove(existingID)
		delete(s.entries, key)
	}

	if sr.Spec.Suspend {
		return nil
	}

	entryID, err := s.cronEngine.AddFunc(sr.Spec.Schedule, func() {
		s.triggerRun(key)
	})
	if err != nil {
		return fmt.Errorf("failed to add cron schedule for %s: %w", key, err)
	}

	s.entries[key] = entryID
	return nil
}

func (s *ScheduledRunScheduler) RemoveSchedule(key types.NamespacedName) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existingID, ok := s.entries[key]; ok {
		s.cronEngine.Remove(existingID)
		delete(s.entries, key)
	}
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

func (s *ScheduledRunScheduler) triggerRun(key types.NamespacedName) {
	log := schedulerLog.WithValues("scheduledRun", key)
	ctx := context.Background()

	var sr v1alpha2.ScheduledRun
	if err := s.kube.Get(ctx, key, &sr); err != nil {
		log.Error(err, "Failed to fetch ScheduledRun")
		return
	}

	if sr.Spec.Suspend {
		log.V(1).Info("ScheduledRun is suspended, skipping")
		return
	}

	if sr.Status.Active > 0 && sr.Spec.ConcurrencyPolicy == v1alpha2.ConcurrencyPolicyForbid {
		log.V(1).Info("Skipping run due to Forbid concurrency policy", "active", sr.Status.Active)
		return
	}

	now := metav1.Now()
	sessionID := protocol.GenerateContextID()
	agentRef := utils.ResourceRefString(sr.Spec.AgentRef.Namespace, sr.Spec.AgentRef.Name)
	agentID := utils.ConvertToPythonIdentifier(agentRef)

	entry := v1alpha2.RunHistoryEntry{
		StartTime: now,
		Status:    v1alpha2.RunStatusRunning,
		SessionID: sessionID,
	}
	sr.Status.Active++
	sr.Status.RunHistory = append(sr.Status.RunHistory, entry)
	sr.Status.LastRunTime = &now

	if err := s.kube.Status().Update(ctx, &sr); err != nil {
		log.Error(err, "Failed to update ScheduledRun status to Running")
		return
	}

	// Use a fixed user ID for scheduled runs.
	sessionUserID := "scheduled-run"

	session := &database.Session{
		ID:      sessionID,
		UserID:  sessionUserID,
		AgentID: &agentID,
	}
	if err := s.dbClient.StoreSession(ctx, session); err != nil {
		log.Error(err, "Failed to create session for scheduled run")
		s.markRunComplete(ctx, key, sessionID, v1alpha2.RunStatusFailed, fmt.Sprintf("failed to create session: %v", err))
		return
	}

	// Send A2A message to the agent to actually execute the prompt.
	agentURL := fmt.Sprintf("http://%s.%s:8080", sr.Spec.AgentRef.Name, sr.Spec.AgentRef.Namespace)
	client, err := a2aclient.NewA2AClient(
		agentURL,
		a2aclient.WithTimeout(5*time.Minute),
		a2aclient.WithHTTPClient(&http.Client{
			Transport: &userIDInjector{
				base:   http.DefaultTransport,
				userID: sessionUserID,
			},
		}),
	)
	if err != nil {
		log.Error(err, "Failed to create A2A client for scheduled run")
		s.markRunComplete(ctx, key, sessionID, v1alpha2.RunStatusFailed, fmt.Sprintf("failed to create A2A client: %v", err))
		return
	}

	log.Info("Sending A2A message to agent", "sessionID", sessionID, "agentURL", agentURL)

	_, err = client.SendMessage(ctx, protocol.SendMessageParams{
		Message: protocol.Message{
			Kind:      protocol.KindMessage,
			Role:      protocol.MessageRoleUser,
			ContextID: &sessionID,
			Parts:     []protocol.Part{protocol.NewTextPart(sr.Spec.Prompt)},
		},
	})
	if err != nil {
		log.Error(err, "Failed to send A2A message for scheduled run")
		s.markRunComplete(ctx, key, sessionID, v1alpha2.RunStatusFailed, fmt.Sprintf("agent invocation failed: %v", err))
		return
	}

	s.markRunComplete(ctx, key, sessionID, v1alpha2.RunStatusSucceeded, "")
	log.Info("Scheduled run completed successfully", "sessionID", sessionID)
}

func (s *ScheduledRunScheduler) markRunComplete(ctx context.Context, key types.NamespacedName, sessionID string, status v1alpha2.RunStatus, message string) {
	log := schedulerLog.WithValues("scheduledRun", key, "sessionID", sessionID)

	var sr v1alpha2.ScheduledRun
	if err := s.kube.Get(ctx, key, &sr); err != nil {
		log.Error(err, "Failed to fetch ScheduledRun for status update")
		return
	}

	completionTime := metav1.Now()
	for i := range sr.Status.RunHistory {
		if sr.Status.RunHistory[i].SessionID == sessionID && sr.Status.RunHistory[i].Status == v1alpha2.RunStatusRunning {
			sr.Status.RunHistory[i].Status = status
			sr.Status.RunHistory[i].CompletionTime = &completionTime
			sr.Status.RunHistory[i].Message = message
			break
		}
	}

	if sr.Status.Active > 0 {
		sr.Status.Active--
	}

	maxHistory := sr.Spec.MaxRunHistory
	if maxHistory <= 0 {
		maxHistory = 10
	}
	if len(sr.Status.RunHistory) > maxHistory {
		sr.Status.RunHistory = sr.Status.RunHistory[len(sr.Status.RunHistory)-maxHistory:]
	}

	if err := s.kube.Status().Update(ctx, &sr); err != nil {
		log.Error(err, "Failed to update ScheduledRun status after completion")
	}
}

func (s *ScheduledRunScheduler) TriggerManualRun(key types.NamespacedName) {
	go s.triggerRun(key)
}

func (s *ScheduledRunScheduler) GetNextRunTime(schedule string) (*time.Time, error) {
	sched, err := cron.ParseStandard(schedule)
	if err != nil {
		return nil, fmt.Errorf("failed to parse cron schedule: %w", err)
	}
	next := sched.Next(time.Now())
	return &next, nil
}
