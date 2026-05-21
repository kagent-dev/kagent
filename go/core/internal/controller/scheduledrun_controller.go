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

	// scheduledRunAgentRefIndex is the field index for ScheduledRun→Agent
	// reverse lookup. The composite "namespace/name" key sidesteps the
	// per-SR namespace defaulting we'd otherwise have to replay in the
	// EventHandler.
	scheduledRunAgentRefIndex = "spec.agentRef"
)

// ScheduledRunController reconciles a ScheduledRun object
type ScheduledRunController struct {
	Scheme    *runtime.Scheme
	Kube      client.Client
	Scheduler *ScheduledRunScheduler
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
	if sr.Spec.TimeZone != "" {
		if _, err := time.LoadLocation(sr.Spec.TimeZone); err != nil {
			meta.SetStatusCondition(&sr.Status.Conditions, metav1.Condition{
				Type:               ScheduledRunConditionTypeAccepted,
				Status:             metav1.ConditionFalse,
				Reason:             "InvalidTimeZone",
				Message:            fmt.Sprintf("Invalid time zone %q: %v", sr.Spec.TimeZone, err),
				ObservedGeneration: sr.Generation,
			})
			sr.Status.ObservedGeneration = sr.Generation
			if updateErr := r.Kube.Status().Update(ctx, &sr); updateErr != nil {
				return ctrl.Result{}, fmt.Errorf("failed to update status: %w", updateErr)
			}
			return ctrl.Result{}, nil
		}
	}

	// Validate cron expression (with optional CRON_TZ embedded via spec.timeZone).
	if _, err := cron.ParseStandard(scheduleSpecForCron(&sr)); err != nil {
		meta.SetStatusCondition(&sr.Status.Conditions, metav1.Condition{
			Type:               ScheduledRunConditionTypeAccepted,
			Status:             metav1.ConditionFalse,
			Reason:             "InvalidSchedule",
			Message:            fmt.Sprintf("Invalid cron expression: %v", err),
			ObservedGeneration: sr.Generation,
		})
		sr.Status.ObservedGeneration = sr.Generation
		if updateErr := r.Kube.Status().Update(ctx, &sr); updateErr != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update status: %w", updateErr)
		}
		return ctrl.Result{}, nil
	}

	// Validate agent ref exists
	agentNamespace := sr.Spec.AgentRef.Namespace
	if agentNamespace == "" {
		agentNamespace = sr.Namespace
	}
	var agent v1alpha2.Agent
	agentKey := types.NamespacedName{Name: sr.Spec.AgentRef.Name, Namespace: agentNamespace}
	if err := r.Kube.Get(ctx, agentKey, &agent); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Agent disappeared (or agentRef was edited to a missing one).
			// Stop firing the cron entry — otherwise every tick would
			// uselessly append a Failed history entry.
			r.Scheduler.RemoveSchedule(req.NamespacedName)
			meta.SetStatusCondition(&sr.Status.Conditions, metav1.Condition{
				Type:               ScheduledRunConditionTypeAccepted,
				Status:             metav1.ConditionFalse,
				Reason:             "AgentNotFound",
				Message:            fmt.Sprintf("Agent %s not found", agentKey),
				ObservedGeneration: sr.Generation,
			})
			sr.Status.ObservedGeneration = sr.Generation
			if updateErr := r.Kube.Status().Update(ctx, &sr); updateErr != nil {
				return ctrl.Result{}, fmt.Errorf("failed to update status: %w", updateErr)
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to check agent ref: %w", err)
	}

	// Update the cron schedule. NextRunTime is owned by the scheduler — it
	// re-computes after each fire so the user sees the freshest value.
	if err := r.Scheduler.UpdateSchedule(&sr); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update schedule: %w", err)
	}

	meta.SetStatusCondition(&sr.Status.Conditions, metav1.Condition{
		Type:               ScheduledRunConditionTypeAccepted,
		Status:             metav1.ConditionTrue,
		Reason:             "ScheduleAccepted",
		Message:            "ScheduledRun is accepted and scheduled",
		ObservedGeneration: sr.Generation,
	})
	sr.Status.ObservedGeneration = sr.Generation

	if err := r.Kube.Status().Update(ctx, &sr); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager. Registers the
// agentRef field index used by the Agent watcher to fan reconcile requests
// out to only the affected ScheduledRuns.
func (r *ScheduledRunController) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&v1alpha2.ScheduledRun{},
		scheduledRunAgentRefIndex,
		func(obj client.Object) []string {
			sr, ok := obj.(*v1alpha2.ScheduledRun)
			if !ok {
				return nil
			}
			ns := sr.Spec.AgentRef.Namespace
			if ns == "" {
				ns = sr.Namespace
			}
			return []string{ns + "/" + sr.Spec.AgentRef.Name}
		},
	); err != nil {
		return fmt.Errorf("failed to index ScheduledRun by agentRef: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			NeedLeaderElection: new(true),
		}).
		For(&v1alpha2.ScheduledRun{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		// Watch Agent so SRs revisit their cron entry when the referenced
		// agent appears/disappears: a recreated Agent re-arms the schedule
		// without operators bumping the SR generation. Generation predicate
		// filters out Agent status writes (Create/Delete events still fire).
		Watches(
			&v1alpha2.Agent{},
			handler.EnqueueRequestsFromMapFunc(r.findScheduledRunsForAgent),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Named("scheduledrun").
		Complete(r)
}

// findScheduledRunsForAgent returns reconcile requests for every ScheduledRun
// whose AgentRef points at the given Agent. Uses the agentRef field index so
// the lookup is O(matched) instead of O(all SRs).
func (r *ScheduledRunController) findScheduledRunsForAgent(ctx context.Context, obj client.Object) []reconcile.Request {
	var srList v1alpha2.ScheduledRunList
	if err := r.Kube.List(ctx, &srList, client.MatchingFields{
		scheduledRunAgentRefIndex: obj.GetNamespace() + "/" + obj.GetName(),
	}); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0, len(srList.Items))
	for i := range srList.Items {
		sr := &srList.Items[i]
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: sr.Name, Namespace: sr.Namespace},
		})
	}
	return requests
}
