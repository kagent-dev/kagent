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
	"sort"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// defaultRetentionInterval is the default polling interval for retention cleanup.
	defaultRetentionInterval = 60 * time.Second
)

// WorkflowRunRetentionController periodically cleans up old WorkflowRuns
// based on retention policies (history limits) and TTL settings.
type WorkflowRunRetentionController struct {
	K8sClient client.Client
	Interval  time.Duration
}

// Start begins the retention cleanup loop. It blocks until the context is cancelled.
func (r *WorkflowRunRetentionController) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("retention-controller")
	interval := r.Interval
	if interval == 0 {
		interval = defaultRetentionInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Info("Retention controller started", "interval", interval)

	for {
		select {
		case <-ctx.Done():
			logger.Info("Retention controller stopped")
			return nil
		case <-ticker.C:
			if err := r.cleanup(ctx); err != nil {
				logger.Error(err, "retention cleanup cycle failed")
			}
		}
	}
}

// NeedLeaderElection implements manager.LeaderElectionRunnable so the retention
// controller only runs on the leader.
func (r *WorkflowRunRetentionController) NeedLeaderElection() bool {
	return true
}

// cleanup performs a single retention cleanup cycle.
func (r *WorkflowRunRetentionController) cleanup(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("retention-controller")

	// Enforce TTL-based cleanup.
	if err := r.cleanupTTL(ctx); err != nil {
		logger.Error(err, "TTL cleanup failed")
	}

	// Enforce history-limit-based cleanup.
	if err := r.cleanupHistoryLimits(ctx); err != nil {
		logger.Error(err, "history limit cleanup failed")
	}

	return nil
}

// cleanupTTL deletes completed WorkflowRuns whose TTL has expired.
func (r *WorkflowRunRetentionController) cleanupTTL(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("retention-controller")

	var runList v1alpha2.WorkflowRunList
	if err := r.K8sClient.List(ctx, &runList); err != nil {
		return fmt.Errorf("failed to list WorkflowRuns: %w", err)
	}

	now := time.Now()
	for i := range runList.Items {
		run := &runList.Items[i]

		// Skip runs without TTL, non-terminal runs, or runs without completion time.
		if run.Spec.TTLSecondsAfterFinished == nil || !isTerminalPhase(run.Status.Phase) || run.Status.CompletionTime == nil {
			continue
		}

		ttl := time.Duration(*run.Spec.TTLSecondsAfterFinished) * time.Second
		expiry := run.Status.CompletionTime.Time.Add(ttl)
		if now.After(expiry) {
			logger.Info("Deleting WorkflowRun due to TTL expiry",
				"name", run.Name, "namespace", run.Namespace,
				"completionTime", run.Status.CompletionTime.Time,
				"ttl", ttl)
			if err := r.K8sClient.Delete(ctx, run); err != nil {
				logger.Error(err, "failed to delete expired WorkflowRun",
					"name", run.Name, "namespace", run.Namespace)
			}
		}
	}

	return nil
}

// cleanupHistoryLimits enforces retention history limits from WorkflowTemplates.
func (r *WorkflowRunRetentionController) cleanupHistoryLimits(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("retention-controller")

	// List all templates with retention policies.
	var templateList v1alpha2.WorkflowTemplateList
	if err := r.K8sClient.List(ctx, &templateList); err != nil {
		return fmt.Errorf("failed to list WorkflowTemplates: %w", err)
	}

	for i := range templateList.Items {
		tmpl := &templateList.Items[i]
		if tmpl.Spec.Retention == nil {
			continue
		}

		if err := r.enforceHistoryLimit(ctx, tmpl); err != nil {
			logger.Error(err, "failed to enforce history limit",
				"template", tmpl.Name, "namespace", tmpl.Namespace)
		}
	}

	return nil
}

// enforceHistoryLimit deletes the oldest completed runs beyond the retention limits for a template.
func (r *WorkflowRunRetentionController) enforceHistoryLimit(ctx context.Context, tmpl *v1alpha2.WorkflowTemplate) error {
	logger := log.FromContext(ctx).WithName("retention-controller")

	// List all runs for this template.
	var runList v1alpha2.WorkflowRunList
	if err := r.K8sClient.List(ctx, &runList, client.InNamespace(tmpl.Namespace)); err != nil {
		return fmt.Errorf("failed to list WorkflowRuns: %w", err)
	}

	// Separate into succeeded and failed, filtering for this template.
	var succeeded, failed []*v1alpha2.WorkflowRun
	for i := range runList.Items {
		run := &runList.Items[i]
		if run.Spec.WorkflowTemplateRef != tmpl.Name {
			continue
		}

		switch run.Status.Phase {
		case v1alpha2.WorkflowRunPhaseSucceeded:
			succeeded = append(succeeded, run)
		case v1alpha2.WorkflowRunPhaseFailed:
			failed = append(failed, run)
		}
	}

	// Sort by completion time ascending (oldest first).
	sortByCompletionTime(succeeded)
	sortByCompletionTime(failed)

	// Enforce successful runs limit.
	if tmpl.Spec.Retention.SuccessfulRunsHistoryLimit != nil {
		limit := int(*tmpl.Spec.Retention.SuccessfulRunsHistoryLimit)
		if len(succeeded) > limit {
			toDelete := succeeded[:len(succeeded)-limit]
			for _, run := range toDelete {
				logger.Info("Deleting WorkflowRun due to successful history limit",
					"name", run.Name, "namespace", run.Namespace,
					"template", tmpl.Name, "limit", limit)
				if err := r.K8sClient.Delete(ctx, run); err != nil {
					logger.Error(err, "failed to delete WorkflowRun",
						"name", run.Name, "namespace", run.Namespace)
				}
			}
		}
	}

	// Enforce failed runs limit.
	if tmpl.Spec.Retention.FailedRunsHistoryLimit != nil {
		limit := int(*tmpl.Spec.Retention.FailedRunsHistoryLimit)
		if len(failed) > limit {
			toDelete := failed[:len(failed)-limit]
			for _, run := range toDelete {
				logger.Info("Deleting WorkflowRun due to failed history limit",
					"name", run.Name, "namespace", run.Namespace,
					"template", tmpl.Name, "limit", limit)
				if err := r.K8sClient.Delete(ctx, run); err != nil {
					logger.Error(err, "failed to delete WorkflowRun",
						"name", run.Name, "namespace", run.Namespace)
				}
			}
		}
	}

	return nil
}

// sortByCompletionTime sorts runs by completion time ascending (oldest first).
// Runs without completion time are placed first (treated as oldest).
func sortByCompletionTime(runs []*v1alpha2.WorkflowRun) {
	sort.Slice(runs, func(i, j int) bool {
		ti := runs[i].Status.CompletionTime
		tj := runs[j].Status.CompletionTime
		if ti == nil && tj == nil {
			return false
		}
		if ti == nil {
			return true
		}
		if tj == nil {
			return false
		}
		return ti.Time.Before(tj.Time)
	})
}

// isTerminalPhase returns true if the phase represents a completed workflow.
func isTerminalPhase(phase string) bool {
	switch phase {
	case v1alpha2.WorkflowRunPhaseSucceeded, v1alpha2.WorkflowRunPhaseFailed, v1alpha2.WorkflowRunPhaseCancelled:
		return true
	}
	return false
}
