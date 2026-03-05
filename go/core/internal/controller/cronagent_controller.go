package controller

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/controller/translator"
)

// CronAgentReconciler reconciles a CronAgent object
type CronAgentReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	DBClient   database.Client
	Translator *translator.CronAgentTranslator
}

// +kubebuilder:rbac:groups=kagent.dev,resources=cronagents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kagent.dev,resources=cronagents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=cronagents/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;delete

// Reconcile implements the reconciliation logic for CronAgent
func (r *CronAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the CronAgent
	cronAgent := &v1alpha2.CronAgent{}
	if err := r.Get(ctx, req.NamespacedName, cronAgent); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check for manual trigger annotation
	if cronAgent.Annotations["cronagent.kagent.dev/trigger"] != "" {
		logger.Info("Manual trigger detected", "cronagent", cronAgent.Name)
		if err := r.triggerManualRun(ctx, cronAgent); err != nil {
			logger.Error(err, "Failed to trigger manual run")
			return ctrl.Result{}, err
		}
		// Remove trigger annotation
		delete(cronAgent.Annotations, "cronagent.kagent.dev/trigger")
		if err := r.Update(ctx, cronAgent); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Store CronAgent configuration in database
	if err := r.storeCronAgentConfig(ctx, cronAgent); err != nil {
		logger.Error(err, "Failed to store CronAgent configuration")
		return ctrl.Result{}, err
	}

	// Create or update CronJob
	cronJob, err := r.Translator.TranslateToCronJob(cronAgent)
	if err != nil {
		logger.Error(err, "Failed to translate CronAgent to CronJob")
		return ctrl.Result{}, err
	}

	if err := r.createOrUpdateCronJob(ctx, cronAgent, cronJob); err != nil {
		logger.Error(err, "Failed to create or update CronJob")
		return ctrl.Result{}, err
	}

	// Update status by watching Jobs
	if err := r.updateStatus(ctx, cronAgent); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	// Cleanup old Jobs based on history limits
	if err := r.cleanupOldJobs(ctx, cronAgent); err != nil {
		logger.Error(err, "Failed to cleanup old jobs")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// storeCronAgentConfig stores the CronAgent configuration in the database
func (r *CronAgentReconciler) storeCronAgentConfig(ctx context.Context, cronAgent *v1alpha2.CronAgent) error {
	// Convert ThreadPolicy and ConcurrencyPolicy to database types
	threadPolicy := database.ThreadPolicy(cronAgent.Spec.ThreadPolicy)
	if cronAgent.Spec.ThreadPolicy == "" {
		threadPolicy = database.ThreadPolicyPerRun
	}

	var concurrencyPolicy *database.ConcurrencyPolicy
	if cronAgent.Spec.ConcurrencyPolicy != nil {
		policy := database.ConcurrencyPolicy(*cronAgent.Spec.ConcurrencyPolicy)
		concurrencyPolicy = &policy
	}

	// Create database Agent record with CronAgent configuration
	dbAgent := &database.Agent{
		ID:   cronAgent.Name,
		Type: database.AgentTypeCronAgent,
		CronAgentConfig: &database.CronAgentConfig{
			Schedule:                   cronAgent.Spec.Schedule,
			Timezone:                   cronAgent.Spec.Timezone,
			InitialTask:                cronAgent.Spec.InitialTask,
			ThreadPolicy:               threadPolicy,
			ConcurrencyPolicy:          concurrencyPolicy,
			StartingDeadlineSeconds:    cronAgent.Spec.StartingDeadlineSeconds,
			SuccessfulJobsHistoryLimit: cronAgent.Spec.SuccessfulJobsHistoryLimit,
			FailedJobsHistoryLimit:     cronAgent.Spec.FailedJobsHistoryLimit,
			Suspend:                    cronAgent.Spec.Suspend,
		},
		// Note: Config field would contain the AgentTemplate's config
		// This can be populated when translating the AgentTemplate
	}

	return r.DBClient.StoreCronAgent(dbAgent)
}

// createOrUpdateCronJob creates or updates the underlying CronJob
func (r *CronAgentReconciler) createOrUpdateCronJob(ctx context.Context, cronAgent *v1alpha2.CronAgent, cronJob *batchv1.CronJob) error {
	existing := &batchv1.CronJob{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      cronJob.Name,
		Namespace: cronJob.Namespace,
	}, existing)

	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		// CronJob doesn't exist, create it
		return r.Create(ctx, cronJob)
	}

	// CronJob exists, update it
	existing.Spec = cronJob.Spec
	existing.Labels = cronJob.Labels
	existing.Annotations = cronJob.Annotations
	return r.Update(ctx, existing)
}

// triggerManualRun creates a Job immediately outside the cron schedule
func (r *CronAgentReconciler) triggerManualRun(ctx context.Context, cronAgent *v1alpha2.CronAgent) error {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	job, err := r.Translator.TranslateToJob(cronAgent, timestamp)
	if err != nil {
		return fmt.Errorf("failed to translate to Job: %w", err)
	}

	return r.Create(ctx, job)
}

// updateStatus updates the CronAgent status based on Job states
func (r *CronAgentReconciler) updateStatus(ctx context.Context, cronAgent *v1alpha2.CronAgent) error {
	// List all Jobs created by this CronAgent
	jobList := &batchv1.JobList{}
	if err := r.List(ctx, jobList,
		client.InNamespace(cronAgent.Namespace),
		client.MatchingLabels{"cronagent.kagent.dev/name": cronAgent.Name},
	); err != nil {
		return err
	}

	// Separate jobs by status
	var active []v1alpha2.JobRunReference
	var successful *v1alpha2.JobRunReference
	var failed *v1alpha2.JobRunReference

	// Sort jobs by creation time (newest first)
	sort.Slice(jobList.Items, func(i, j int) bool {
		return jobList.Items[i].CreationTimestamp.After(jobList.Items[j].CreationTimestamp.Time)
	})

	for _, job := range jobList.Items {
		ref := v1alpha2.JobRunReference{
			Name:      job.Name,
			StartTime: job.Status.StartTime,
		}

		if job.Status.CompletionTime != nil {
			ref.CompletionTime = job.Status.CompletionTime
		}

		// Determine job status
		if job.Status.Active > 0 {
			active = append(active, ref)
		} else if job.Status.Succeeded > 0 {
			if successful == nil {
				successful = &ref
			}
		} else if job.Status.Failed > 0 {
			if failed == nil {
				failed = &ref
			}
		}
	}

	// Update CronAgent status
	cronAgent.Status.ActiveRuns = active
	cronAgent.Status.LastSuccessfulRun = successful
	cronAgent.Status.LastFailedRun = failed

	return r.Status().Update(ctx, cronAgent)
}

// cleanupOldJobs removes Jobs exceeding the history limits
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
		// Skip active jobs
		if job.Status.Active > 0 {
			continue
		}

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

// SetupWithManager sets up the controller with the Manager
func (r *CronAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha2.CronAgent{}).
		Owns(&batchv1.CronJob{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
