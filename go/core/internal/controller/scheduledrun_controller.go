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

	"github.com/kagent-dev/kagent/go/core/internal/scheduledrun"
	"github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

const (
	scheduledRunReasonAccepted         = "ScheduleAccepted"
	scheduledRunReasonInvalidSchedule  = "InvalidSchedule"
	scheduledRunReasonInvalidTargetRef = "InvalidTargetRef"
	scheduledRunReasonInvalidTimeZone  = "InvalidTimeZone"
	scheduledRunReasonTargetNotFound   = "TargetNotFound"
)

// ScheduledRunController validates ScheduledRuns and keeps their cron entries in sync.
type ScheduledRunController struct {
	Kube      client.Client
	Scheduler *scheduledrun.ScheduledRunScheduler
}

// +kubebuilder:rbac:groups=kagent.dev,resources=scheduledruns,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kagent.dev,resources=scheduledruns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=scheduledruns/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

func (r *ScheduledRunController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var sr v1alpha2.ScheduledRun
	if err := r.Kube.Get(ctx, req.NamespacedName, &sr); err != nil {
		if apierrors.IsNotFound(err) {
			r.Scheduler.RemoveCronEntry(req.NamespacedName)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get Scheduled Run: %w", err)
	}

	// Validate spec.timeZone is a known IANA name. Done before the cron
	// parse so a bad TZ surfaces as "InvalidTimeZone" instead of being
	// re-reported as a generic "InvalidSchedule" by the parser.
	timeZone := scheduledrun.ScheduledRunTimeZone(&sr)
	if _, err := time.LoadLocation(timeZone); err != nil {
		message := fmt.Sprintf("Invalid time zone %q: %v", timeZone, err)
		return ctrl.Result{}, r.rejectScheduledRun(ctx, &sr, scheduledRunReasonInvalidTimeZone, message)
	}

	// Validate cron expression (with optional CRON_TZ embedded via spec.timeZone).
	parsedSchedule, err := cron.ParseStandard(scheduledrun.CronSpecForSchedule(&sr))
	if err != nil {
		message := fmt.Sprintf("Invalid cron expression: %v", err)
		return ctrl.Result{}, r.rejectScheduledRun(ctx, &sr, scheduledRunReasonInvalidSchedule, message)
	}

	if err := scheduledrun.ValidateTargetRef(sr.Spec.TargetRef); err != nil {
		return ctrl.Result{}, r.rejectScheduledRun(ctx, &sr, scheduledRunReasonInvalidTargetRef, err.Error())
	}

	targetKey := scheduledrun.TargetKey(sr.Namespace, sr.Spec.TargetRef)
	_, err = scheduledrun.GetTarget(ctx, r.Kube, sr.Namespace, sr.Spec.TargetRef)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Stop any existing cron entry while the target is absent; otherwise every tick would
			// uselessly append a DispatchFailed execution.
			message := fmt.Sprintf("target %s not found", targetKey)
			return ctrl.Result{}, r.rejectScheduledRun(ctx, &sr, scheduledRunReasonTargetNotFound, message)
		}
		return ctrl.Result{}, fmt.Errorf("failed to check targetRef: %w", err)
	}

	return ctrl.Result{}, r.acceptScheduledRun(ctx, &sr, parsedSchedule)
}

func (r *ScheduledRunController) acceptScheduledRun(
	ctx context.Context,
	sr *v1alpha2.ScheduledRun,
	parsedSchedule cron.Schedule,
) error {
	if err := r.Scheduler.UpdateCronEntry(sr); err != nil {
		return fmt.Errorf("failed to update cron entry: %w", err)
	}

	message := "Scheduled Run is accepted and scheduled"
	if scheduledrun.IsSuspended(sr) {
		sr.Status.NextExecutionTime = nil
		message = "Scheduled Run is accepted and suspended"
	} else {
		next := metav1.NewTime(parsedSchedule.Next(time.Now()))
		sr.Status.NextExecutionTime = &next
	}

	return r.updateScheduledRunStatus(ctx, sr, metav1.ConditionTrue, scheduledRunReasonAccepted, message)
}

func (r *ScheduledRunController) rejectScheduledRun(ctx context.Context, sr *v1alpha2.ScheduledRun, reason, message string) error {
	r.Scheduler.RemoveCronEntry(client.ObjectKeyFromObject(sr))
	sr.Status.NextExecutionTime = nil
	return r.updateScheduledRunStatus(ctx, sr, metav1.ConditionFalse, reason, message)
}

func (r *ScheduledRunController) updateScheduledRunStatus(
	ctx context.Context,
	sr *v1alpha2.ScheduledRun,
	status metav1.ConditionStatus,
	reason string,
	message string,
) error {
	meta.SetStatusCondition(&sr.Status.Conditions, metav1.Condition{
		Type:               v1alpha2.ScheduledRunConditionTypeAccepted,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: sr.Generation,
	})
	sr.Status.ObservedGeneration = sr.Generation
	if updateErr := r.Kube.Status().Update(ctx, sr); updateErr != nil {
		return fmt.Errorf("failed to update status: %w", updateErr)
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScheduledRunController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha2.ScheduledRun{}, builder.WithPredicates(scheduledRunReconcilePredicate())).
		Watches(
			&v1alpha2.Agent{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueScheduledRunsForTarget(scheduledrun.TargetKindAgent)),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Watches(
			&v1alpha2.SandboxAgent{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueScheduledRunsForTarget(scheduledrun.TargetKindSandboxAgent)),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Named("scheduledrun").
		Complete(r)
}

// scheduledRunReconcilePredicate admits spec changes and transitions into or
// out of InProgress. The latter lets the leader observe manual executions dispatched
// by any API replica and attach a deduplicated outcome poller.
func scheduledRunReconcilePredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return true },
		DeleteFunc:  func(event.DeleteEvent) bool { return true },
		GenericFunc: func(event.GenericEvent) bool { return false },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldRun, oldOK := e.ObjectOld.(*v1alpha2.ScheduledRun)
			newRun, newOK := e.ObjectNew.(*v1alpha2.ScheduledRun)
			if !oldOK || !newOK {
				return false
			}
			if oldRun.Generation != newRun.Generation {
				return true
			}
			return !sameInProgressExecutions(oldRun, newRun)
		},
	}
}

func sameInProgressExecutions(a, b *v1alpha2.ScheduledRun) bool {
	aInProgress := inProgressExecutionIDs(a)
	bInProgress := inProgressExecutionIDs(b)
	if len(aInProgress) != len(bInProgress) {
		return false
	}
	for id := range aInProgress {
		if _, exists := bInProgress[id]; !exists {
			return false
		}
	}
	return true
}

func inProgressExecutionIDs(sr *v1alpha2.ScheduledRun) map[string]struct{} {
	inProgress := make(map[string]struct{})
	for _, execution := range sr.Status.RecentExecutions {
		if execution.Status == v1alpha2.ScheduledRunExecutionStatus_InProgress {
			inProgress[execution.ID] = struct{}{}
		}
	}
	return inProgress
}

// enqueueScheduledRunsForTarget returns a map func that finds ScheduledRuns
// whose TargetRef points at the changed object.
func (r *ScheduledRunController) enqueueScheduledRunsForTarget(kind string) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		var list v1alpha2.ScheduledRunList
		apiGroup := scheduledrun.TargetAPIGroup
		if err := r.Kube.List(ctx, &list, client.MatchingFields{
			scheduledrun.TargetRefIndexField: scheduledrun.TargetRefKey(obj.GetNamespace(), corev1.TypedLocalObjectReference{
				APIGroup: &apiGroup,
				Kind:     kind,
				Name:     obj.GetName(),
			}),
		}); err != nil {
			log.FromContext(ctx).Error(err, "failed to list Scheduled Runs for target watch")
			return nil
		}
		requests := make([]reconcile.Request, 0, len(list.Items))
		for i := range list.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{Namespace: list.Items[i].Namespace, Name: list.Items[i].Name},
			})
		}
		return requests
	}
}
