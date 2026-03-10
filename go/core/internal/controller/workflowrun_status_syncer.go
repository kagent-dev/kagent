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

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	workflow "github.com/kagent-dev/kagent/go/core/internal/temporal/workflow"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// defaultSyncInterval is the default polling interval for status syncing.
	defaultSyncInterval = 5 * time.Second
)

// WorkflowRunStatusSyncer polls Temporal and updates WorkflowRun status.
type WorkflowRunStatusSyncer struct {
	K8sClient      client.Client
	TemporalClient TemporalWorkflowClient
	Interval       time.Duration
}

// Start begins the status sync loop. It blocks until the context is cancelled.
func (s *WorkflowRunStatusSyncer) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("status-syncer")
	interval := s.Interval
	if interval == 0 {
		interval = defaultSyncInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Info("Status syncer started", "interval", interval)

	for {
		select {
		case <-ctx.Done():
			logger.Info("Status syncer stopped")
			return nil
		case <-ticker.C:
			if err := s.syncAll(ctx); err != nil {
				logger.Error(err, "sync cycle failed")
			}
		}
	}
}

// syncAll finds all running WorkflowRuns and syncs their status from Temporal.
func (s *WorkflowRunStatusSyncer) syncAll(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("status-syncer")

	var runList v1alpha2.WorkflowRunList
	if err := s.K8sClient.List(ctx, &runList); err != nil {
		return fmt.Errorf("failed to list WorkflowRuns: %w", err)
	}

	for i := range runList.Items {
		run := &runList.Items[i]

		// Only sync runs that are in Running phase with a Temporal workflow ID.
		if run.Status.Phase != v1alpha2.WorkflowRunPhaseRunning || run.Status.TemporalWorkflowID == "" {
			continue
		}

		if err := s.syncOne(ctx, run); err != nil {
			logger.Error(err, "failed to sync WorkflowRun",
				"name", run.Name, "namespace", run.Namespace,
				"workflowID", run.Status.TemporalWorkflowID)
		}
	}

	return nil
}

// syncOne syncs a single WorkflowRun's status from Temporal.
func (s *WorkflowRunStatusSyncer) syncOne(ctx context.Context, run *v1alpha2.WorkflowRun) error {
	workflowID := run.Status.TemporalWorkflowID

	// Describe workflow execution for overall status.
	desc, err := s.TemporalClient.DescribeWorkflow(ctx, workflowID)
	if err != nil {
		return fmt.Errorf("failed to describe workflow %s: %w", workflowID, err)
	}

	// Query per-step statuses from the DAG workflow.
	var stepResults []workflow.StepResult
	if desc.Status == WorkflowExecutionRunning {
		if err := s.TemporalClient.QueryWorkflow(ctx, workflowID, workflow.DAGStatusQueryType, &stepResults); err != nil {
			// Query failure is non-fatal for running workflows — skip step sync.
			log.FromContext(ctx).WithName("status-syncer").V(1).Info(
				"failed to query step status, skipping step sync",
				"workflowID", workflowID, "error", err)
		}
	}

	// Build updated step statuses.
	updated := false
	if len(stepResults) > 0 {
		newSteps := make([]v1alpha2.StepStatus, len(stepResults))
		for i, sr := range stepResults {
			newSteps[i] = v1alpha2.StepStatus{
				Name:    sr.Name,
				Phase:   v1alpha2.StepPhase(sr.Phase),
				Message: sr.Error,
				Retries: sr.Retries,
			}
		}
		if !stepStatusesEqual(run.Status.Steps, newSteps) {
			run.Status.Steps = newSteps
			updated = true
		}
	}

	// Handle terminal states.
	switch desc.Status {
	case WorkflowExecutionCompleted:
		// Query final step statuses for completed workflows.
		// For completed workflows, we get the result from the workflow output, not query.
		now := metav1.Now()
		run.Status.Phase = v1alpha2.WorkflowRunPhaseSucceeded
		run.Status.CompletionTime = &now
		meta.SetStatusCondition(&run.Status.Conditions, metav1.Condition{
			Type:               v1alpha2.WorkflowRunConditionRunning,
			Status:             metav1.ConditionFalse,
			Reason:             "WorkflowCompleted",
			Message:            "Temporal workflow completed successfully",
			ObservedGeneration: run.Generation,
		})
		meta.SetStatusCondition(&run.Status.Conditions, metav1.Condition{
			Type:               v1alpha2.WorkflowRunConditionSucceeded,
			Status:             metav1.ConditionTrue,
			Reason:             "Succeeded",
			Message:            "Workflow completed successfully",
			ObservedGeneration: run.Generation,
		})
		updated = true

	case WorkflowExecutionFailed:
		now := metav1.Now()
		run.Status.Phase = v1alpha2.WorkflowRunPhaseFailed
		run.Status.CompletionTime = &now
		message := "Temporal workflow failed"
		if desc.Error != "" {
			message = fmt.Sprintf("Temporal workflow failed: %s", desc.Error)
		}
		meta.SetStatusCondition(&run.Status.Conditions, metav1.Condition{
			Type:               v1alpha2.WorkflowRunConditionRunning,
			Status:             metav1.ConditionFalse,
			Reason:             "WorkflowFailed",
			Message:            message,
			ObservedGeneration: run.Generation,
		})
		meta.SetStatusCondition(&run.Status.Conditions, metav1.Condition{
			Type:               v1alpha2.WorkflowRunConditionSucceeded,
			Status:             metav1.ConditionFalse,
			Reason:             "Failed",
			Message:            message,
			ObservedGeneration: run.Generation,
		})
		updated = true

	case WorkflowExecutionCancelled, WorkflowExecutionTerminated, WorkflowExecutionTimedOut:
		now := metav1.Now()
		run.Status.Phase = v1alpha2.WorkflowRunPhaseCancelled
		run.Status.CompletionTime = &now
		meta.SetStatusCondition(&run.Status.Conditions, metav1.Condition{
			Type:               v1alpha2.WorkflowRunConditionRunning,
			Status:             metav1.ConditionFalse,
			Reason:             "Workflow" + string(desc.Status),
			Message:            fmt.Sprintf("Temporal workflow %s", desc.Status),
			ObservedGeneration: run.Generation,
		})
		updated = true
	}

	if updated {
		if err := s.K8sClient.Status().Update(ctx, run); err != nil {
			return fmt.Errorf("failed to update WorkflowRun status: %w", err)
		}
	}

	return nil
}

// stepStatusesEqual compares two slices of StepStatus for equality.
func stepStatusesEqual(a, b []v1alpha2.StepStatus) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Phase != b[i].Phase || a[i].Message != b[i].Message || a[i].Retries != b[i].Retries {
			return false
		}
	}
	return true
}

// NeedLeaderElection implements manager.LeaderElectionRunnable so the syncer
// only runs on the leader.
func (s *WorkflowRunStatusSyncer) NeedLeaderElection() bool {
	return true
}

