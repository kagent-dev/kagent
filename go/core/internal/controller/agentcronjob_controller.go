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
	"time"

	"github.com/robfig/cron/v3"

	kagentclient "github.com/kagent-dev/kagent/go/api/client"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

const (
	cronJobSystemUser  = "system:cronjob@kagent.dev"
	cronJobExecTimeout = 5 * time.Minute
)

// AgentCronJobController reconciles AgentCronJob objects.
// It parses cron schedules, triggers agent runs via the HTTP API, and uses
// RequeueAfter to schedule the next reconciliation at the appropriate time.
type AgentCronJobController struct {
	client.Client
	Scheme     *runtime.Scheme
	A2ABaseURL string // Base URL of the kagent HTTP server (e.g., "http://127.0.0.1:8083")
}

// +kubebuilder:rbac:groups=kagent.dev,resources=agentcronjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kagent.dev,resources=agentcronjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=agentcronjobs/finalizers,verbs=update

func (r *AgentCronJobController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// 1. Fetch the AgentCronJob CR
	var cronJob v1alpha2.AgentCronJob
	if err := r.Get(ctx, req.NamespacedName, &cronJob); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 2. Parse cron schedule
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(cronJob.Spec.Schedule)
	if err != nil {
		meta.SetStatusCondition(&cronJob.Status.Conditions, metav1.Condition{
			Type:               v1alpha2.AgentCronJobConditionTypeAccepted,
			Status:             metav1.ConditionFalse,
			Reason:             "InvalidSchedule",
			Message:            fmt.Sprintf("Failed to parse cron schedule: %v", err),
			ObservedGeneration: cronJob.Generation,
		})
		cronJob.Status.ObservedGeneration = cronJob.Generation
		if statusErr := r.Status().Update(ctx, &cronJob); statusErr != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update status for invalid schedule: %w", statusErr)
		}
		return ctrl.Result{}, nil
	}

	// 3. Set Accepted=True
	meta.SetStatusCondition(&cronJob.Status.Conditions, metav1.Condition{
		Type:               v1alpha2.AgentCronJobConditionTypeAccepted,
		Status:             metav1.ConditionTrue,
		Reason:             "ScheduleValid",
		Message:            "Cron schedule is valid",
		ObservedGeneration: cronJob.Generation,
	})

	// 4. Calculate next run time
	now := time.Now()
	var referenceTime time.Time
	if cronJob.Status.LastRunTime != nil {
		referenceTime = cronJob.Status.LastRunTime.Time
	} else {
		referenceTime = cronJob.CreationTimestamp.Time
	}
	nextRun := schedule.Next(referenceTime)

	// 5. Check if it's time to run
	if !now.Before(nextRun) {
		logger.Info("Executing scheduled run", "agentRef", cronJob.Spec.AgentRef, "scheduledTime", nextRun)

		sessionID, execErr := r.executeRun(ctx, &cronJob)
		if execErr != nil {
			logger.Error(execErr, "Failed to execute cron job run")
			cronJob.Status.LastRunResult = "Failed"
			cronJob.Status.LastRunMessage = execErr.Error()
		} else {
			cronJob.Status.LastRunResult = "Success"
			cronJob.Status.LastRunMessage = ""
			cronJob.Status.LastSessionID = sessionID
		}
		cronJob.Status.LastRunTime = &metav1.Time{Time: now}

		// Recalculate next run from now
		nextRun = schedule.Next(now)
	}

	// 6. Update status with next run time
	cronJob.Status.NextRunTime = &metav1.Time{Time: nextRun}

	meta.SetStatusCondition(&cronJob.Status.Conditions, metav1.Condition{
		Type:               v1alpha2.AgentCronJobConditionTypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Scheduled",
		Message:            fmt.Sprintf("Next run at %s", nextRun.Format(time.RFC3339)),
		ObservedGeneration: cronJob.Generation,
	})

	cronJob.Status.ObservedGeneration = cronJob.Generation
	if err := r.Status().Update(ctx, &cronJob); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
	}

	// 7. Requeue for next run
	requeueAfter := time.Until(nextRun)
	if requeueAfter < 0 {
		requeueAfter = time.Second
	}
	logger.Info("Scheduling next reconciliation", "requeueAfter", requeueAfter, "nextRun", nextRun)

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// executeRun creates a session and sends the prompt to the agent via A2A.
func (r *AgentCronJobController) executeRun(ctx context.Context, cronJob *v1alpha2.AgentCronJob) (string, error) {
	// Verify the Agent CR exists
	var agent v1alpha2.Agent
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: cronJob.Namespace,
		Name:      cronJob.Spec.AgentRef,
	}, &agent); err != nil {
		return "", fmt.Errorf("agent %q not found: %w", cronJob.Spec.AgentRef, err)
	}

	// Create session via HTTP API
	baseClient := kagentclient.NewBaseClient(r.A2ABaseURL, kagentclient.WithUserID(cronJobSystemUser))
	sessionClient := kagentclient.NewSessionClient(baseClient)

	sessionName := fmt.Sprintf("cronjob-%s-%d", cronJob.Name, time.Now().Unix())
	agentRef := fmt.Sprintf("%s/%s", cronJob.Namespace, cronJob.Spec.AgentRef)

	sessionResp, err := sessionClient.CreateSession(ctx, &api.SessionRequest{
		AgentRef: &agentRef,
		Name:     &sessionName,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}
	if sessionResp.Error || sessionResp.Data == nil {
		return "", fmt.Errorf("session creation failed: %s", sessionResp.Message)
	}

	sessionID := sessionResp.Data.ID

	// Send prompt via A2A
	a2aURL := fmt.Sprintf("%s/api/a2a/%s/%s", r.A2ABaseURL, cronJob.Namespace, cronJob.Spec.AgentRef)

	execCtx, cancel := context.WithTimeout(ctx, cronJobExecTimeout)
	defer cancel()

	a2aC, err := a2aclient.NewA2AClient(a2aURL,
		a2aclient.WithAPIKeyAuth(cronJobSystemUser, "x-user-id"),
		a2aclient.WithTimeout(cronJobExecTimeout),
	)
	if err != nil {
		return sessionID, fmt.Errorf("failed to create A2A client: %w", err)
	}

	msg := protocol.Message{
		Kind:      protocol.KindMessage,
		Role:      protocol.MessageRoleUser,
		Parts:     []protocol.Part{protocol.NewTextPart(cronJob.Spec.Prompt)},
		ContextID: &sessionID,
	}

	result, err := a2aC.SendMessage(execCtx, protocol.SendMessageParams{Message: msg})
	if err != nil {
		return sessionID, fmt.Errorf("failed to send message to agent: %w", err)
	}

	// Check the task result status if it's a Task response.
	if result != nil && result.Result != nil {
		if task, ok := result.Result.(*protocol.Task); ok {
			switch task.Status.State {
			case protocol.TaskStateFailed:
				msg := "task failed"
				if task.Status.Message != nil {
					for _, p := range task.Status.Message.Parts {
						if tp, ok := p.(protocol.TextPart); ok {
							msg = tp.Text
							break
						}
					}
				}
				return sessionID, fmt.Errorf("agent task failed: %s", msg)
			case protocol.TaskStateCanceled:
				return sessionID, fmt.Errorf("agent task was cancelled")
			}
		}
	}

	return sessionID, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AgentCronJobController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			NeedLeaderElection: ptr.To(true),
		}).
		For(&v1alpha2.AgentCronJob{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named("agentcronjob").
		Complete(r)
}
