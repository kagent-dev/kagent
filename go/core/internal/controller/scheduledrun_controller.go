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
	"slices"
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
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

const (
	scheduledRunReasonAccepted                  = "ScheduleAccepted"
	scheduledRunReasonInvalidSchedule           = "InvalidSchedule"
	scheduledRunReasonInvalidTargetRef          = "InvalidTargetRef"
	scheduledRunReasonInvalidTimeZone           = "InvalidTimeZone"
	scheduledRunReasonTargetNamespaceNotWatched = "TargetNamespaceNotWatched"
	scheduledRunReasonTargetNotFound            = "TargetNotFound"
	scheduledRunReasonTargetReferenceNotAllowed = "TargetReferenceNotAllowed"
)

// ScheduledRunController validates ScheduledRuns and keeps their cron entries in sync.
type ScheduledRunController struct {
	Kube              client.Client
	Scheduler         *scheduledrun.ScheduledRunScheduler
	WatchedNamespaces []string
}

// +kubebuilder:rbac:groups=kagent.dev,resources=scheduledruns,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kagent.dev,resources=scheduledruns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=scheduledruns/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

func (r *ScheduledRunController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var sr v1alpha2.ScheduledRun
	if err := r.Kube.Get(ctx, req.NamespacedName, &sr); err != nil {
		if apierrors.IsNotFound(err) {
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
		message := fmt.Sprintf("Invalid time zone %q: %v", timeZone, err)
		return ctrl.Result{}, r.rejectScheduledRun(ctx, &sr, scheduledRunReasonInvalidTimeZone, message)
	}

	// Validate cron expression (with optional CRON_TZ embedded via spec.timeZone).
	cronSchedule, err := cron.ParseStandard(scheduledrun.ScheduleSpecForCron(&sr))
	if err != nil {
		message := fmt.Sprintf("Invalid cron expression: %v", err)
		return ctrl.Result{}, r.rejectScheduledRun(ctx, &sr, scheduledRunReasonInvalidSchedule, message)
	}

	if err := scheduledrun.ValidateTargetRef(sr.Spec.TargetRef); err != nil {
		return ctrl.Result{}, r.rejectScheduledRun(ctx, &sr, scheduledRunReasonInvalidTargetRef, err.Error())
	}

	targetKey := scheduledrun.TargetKey(sr.Namespace, sr.Spec.TargetRef)
	if !r.isNamespaceWatched(targetKey.Namespace) {
		message := fmt.Sprintf("Target namespace %q is not watched by the controller", targetKey.Namespace)
		return ctrl.Result{}, r.rejectScheduledRun(ctx, &sr, scheduledRunReasonTargetNamespaceNotWatched, message)
	}

	target, err := scheduledrun.GetTarget(ctx, r.Kube, sr.Namespace, sr.Spec.TargetRef)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Stop any existing cron entry while the target is absent; otherwise every tick would
			// uselessly append a DispatchFailed history entry.
			message := fmt.Sprintf("%s %s not found", scheduledrun.TargetKind(sr.Spec.TargetRef), targetKey)
			return ctrl.Result{}, r.rejectScheduledRun(ctx, &sr, scheduledRunReasonTargetNotFound, message)
		}
		return ctrl.Result{}, fmt.Errorf("failed to check targetRef: %w", err)
	}

	if err := scheduledrun.ValidateTargetNamespaceAccess(ctx, r.Kube, sr.Namespace, target); err != nil {
		if errors.Is(err, scheduledrun.ErrTargetAccessDenied) {
			return ctrl.Result{}, r.rejectScheduledRun(ctx, &sr, scheduledRunReasonTargetReferenceNotAllowed, err.Error())
		}
		return ctrl.Result{}, fmt.Errorf("failed to validate target namespace access: %w", err)
	}

	return ctrl.Result{}, r.acceptScheduledRun(ctx, &sr, cronSchedule)
}

func (r *ScheduledRunController) acceptScheduledRun(
	ctx context.Context,
	sr *v1alpha2.ScheduledRun,
	cronSchedule cron.Schedule,
) error {
	if err := r.Scheduler.UpdateSchedule(sr); err != nil {
		return fmt.Errorf("failed to update schedule: %w", err)
	}

	message := "ScheduledRun is accepted and scheduled"
	if sr.Spec.Suspend {
		sr.Status.NextRunTime = nil
		message = "ScheduledRun is accepted and suspended"
	} else {
		next := metav1.NewTime(cronSchedule.Next(time.Now()))
		sr.Status.NextRunTime = &next
	}

	return r.updateScheduledRunStatus(ctx, sr, metav1.ConditionTrue, scheduledRunReasonAccepted, message)
}

func (r *ScheduledRunController) rejectScheduledRun(ctx context.Context, sr *v1alpha2.ScheduledRun, reason, message string) error {
	r.Scheduler.RemoveSchedule(client.ObjectKeyFromObject(sr))
	sr.Status.NextRunTime = nil
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
		WithOptions(controller.Options{
			NeedLeaderElection: new(true),
		}).
		For(&v1alpha2.ScheduledRun{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&v1alpha2.Agent{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueScheduledRunsForTarget(v1alpha2.ScheduledRunTargetKindAgent)),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Watches(
			&v1alpha2.SandboxAgent{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueScheduledRunsForTarget(v1alpha2.ScheduledRunTargetKindSandboxAgent)),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Named("scheduledrun").
		Complete(r)
}

// enqueueScheduledRunsForTarget returns a map func that finds ScheduledRuns
// whose normalized TargetRef points at the changed object. The field index
// keeps cross-namespace target watches from requiring a cluster-wide scan.
func (r *ScheduledRunController) enqueueScheduledRunsForTarget(kind string) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		apiGroup := v1alpha2.ScheduledRunTargetAPIGroup
		targetNamespace := obj.GetNamespace()
		ref := corev1.TypedObjectReference{
			APIGroup:  &apiGroup,
			Kind:      kind,
			Namespace: &targetNamespace,
			Name:      obj.GetName(),
		}
		var list v1alpha2.ScheduledRunList
		if err := r.Kube.List(ctx, &list, client.MatchingFields{
			scheduledrun.TargetRefIndexField: scheduledrun.TargetRefKey("", ref),
		}); err != nil {
			log.FromContext(ctx).Error(err, "failed to list ScheduledRuns for target watch")
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

func (r *ScheduledRunController) isNamespaceWatched(namespace string) bool {
	return len(r.WatchedNamespaces) == 0 || slices.Contains(r.WatchedNamespaces, namespace)
}
