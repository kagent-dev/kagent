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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

const (
	ScheduledRunConditionTypeAccepted = "Accepted"
)

// ScheduledRunController reconciles a ScheduledRun object
type ScheduledRunController struct {
	Scheme    *runtime.Scheme
	Kube      client.Client
	Scheduler *scheduledrun.ScheduledRunScheduler
}

// +kubebuilder:rbac:groups=kagent.dev,resources=scheduledruns,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kagent.dev,resources=scheduledruns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=scheduledruns/finalizers,verbs=update

func (r *ScheduledRunController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	var sr v1alpha2.ScheduledRun
	if err := r.Kube.Get(ctx, req.NamespacedName, &sr); err != nil {
		if client.IgnoreNotFound(err) == nil {
			r.Scheduler.RemoveSchedule(req.NamespacedName)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get ScheduledRun: %w", err)
	}

	// Validate spec.timeZone is a known IANA name. Done before the cron
	// parse so a bad TZ surfaces as "InvalidTimeZone" instead of being
	// re-reported as a generic "InvalidSchedule" by the parser.
	timeZone := scheduledrun.ScheduledRunTimeZone(&sr)
	if _, err := time.LoadLocation(timeZone); err != nil {
		return ctrl.Result{}, r.rejectScheduledRun(ctx, req.NamespacedName, &sr, "InvalidTimeZone", fmt.Sprintf("Invalid time zone %q: %v", timeZone, err))
	}

	// Validate cron expression (with optional CRON_TZ embedded via spec.timeZone).
	sched, err := cron.ParseStandard(scheduledrun.ScheduleSpecForCron(&sr))
	if err != nil {
		return ctrl.Result{}, r.rejectScheduledRun(ctx, req.NamespacedName, &sr, "InvalidSchedule", fmt.Sprintf("Invalid cron expression: %v", err))
	}

	if err := scheduledrun.ValidateTargetRef(sr.Spec.TargetRef); err != nil {
		return ctrl.Result{}, r.rejectScheduledRun(ctx, req.NamespacedName, &sr, "InvalidTargetRef", err.Error())
	}

	agentKind := scheduledrun.TargetKind(sr.Spec.TargetRef)
	agentKey := scheduledrun.TargetKey(sr.Namespace, sr.Spec.TargetRef)
	if _, err := scheduledrun.GetTarget(ctx, r.Kube, sr.Namespace, sr.Spec.TargetRef); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Agent disappeared (or targetRef was edited to a missing one).
			// Stop firing the cron entry — otherwise every tick would
			// uselessly append a Failed history entry.
			return ctrl.Result{}, r.rejectScheduledRun(ctx, req.NamespacedName, &sr, "AgentNotFound", fmt.Sprintf("%s %s not found", agentKind, agentKey))
		}
		return ctrl.Result{}, fmt.Errorf("failed to check agent ref: %w", err)
	}

	// Update the cron schedule. NextRunTime is written here on reconcile so new
	// and edited schedules expose the next fire immediately, then refreshed by
	// the scheduler after each run.
	if err := r.Scheduler.UpdateSchedule(&sr); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update schedule: %w", err)
	}

	acceptedMessage := "ScheduledRun is accepted and scheduled"
	if sr.Spec.Suspend {
		sr.Status.NextRunTime = nil
		acceptedMessage = "ScheduledRun is accepted and suspended"
	} else {
		next := metav1.NewTime(sched.Next(time.Now()))
		sr.Status.NextRunTime = &next
	}
	meta.SetStatusCondition(&sr.Status.Conditions, metav1.Condition{
		Type:               ScheduledRunConditionTypeAccepted,
		Status:             metav1.ConditionTrue,
		Reason:             "ScheduleAccepted",
		Message:            acceptedMessage,
		ObservedGeneration: sr.Generation,
	})
	sr.Status.ObservedGeneration = sr.Generation

	if err := r.Kube.Status().Update(ctx, &sr); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *ScheduledRunController) rejectScheduledRun(ctx context.Context, key types.NamespacedName, sr *v1alpha2.ScheduledRun, reason, message string) error {
	r.Scheduler.RemoveSchedule(key)
	sr.Status.NextRunTime = nil
	meta.SetStatusCondition(&sr.Status.Conditions, metav1.Condition{
		Type:               ScheduledRunConditionTypeAccepted,
		Status:             metav1.ConditionFalse,
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
		WithOptions(controller.Options{
			NeedLeaderElection: new(true),
		}).
		For(&v1alpha2.ScheduledRun{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&v1alpha2.Agent{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueScheduledRunsForTarget(v1alpha2.ScheduledRunTargetKindAgent)),
		).
		Watches(
			&v1alpha2.SandboxAgent{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueScheduledRunsForTarget(v1alpha2.ScheduledRunTargetKindSandboxAgent)),
		).
		Named("scheduledrun").
		Complete(r)
}

// enqueueScheduledRunsForTarget returns a map func that finds ScheduledRuns in
// the target's namespace whose TargetRef points at it. TargetRef is restricted to
// the same namespace (validated in Reconcile), so we don't have to scan
// cluster-wide. Reconciling on target create/delete lets the controller
// schedule a previously-missing target's SR or stop firing once a target
// disappears.
func (r *ScheduledRunController) enqueueScheduledRunsForTarget(kind string) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		var list v1alpha2.ScheduledRunList
		if err := r.Kube.List(ctx, &list, client.InNamespace(obj.GetNamespace())); err != nil {
			log.FromContext(ctx).Error(err, "Failed to list ScheduledRuns for target watch")
			return nil
		}
		var requests []reconcile.Request
		for i := range list.Items {
			sr := &list.Items[i]
			if scheduledrun.TargetKind(sr.Spec.TargetRef) != kind {
				continue
			}
			if sr.Spec.TargetRef.Name != obj.GetName() {
				continue
			}
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{Namespace: sr.Namespace, Name: sr.Name},
			})
		}
		return requests
	}
}
