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
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

const (
	ScheduledRunConditionTypeAccepted = "Accepted"
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

	// Validate cron expression
	sched, err := cron.ParseStandard(sr.Spec.Schedule)
	if err != nil {
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

	// Validate minimum schedule interval (1 hour)
	now := time.Now()
	first := sched.Next(now)
	second := sched.Next(first)
	if second.Sub(first) < time.Hour {
		meta.SetStatusCondition(&sr.Status.Conditions, metav1.Condition{
			Type:               ScheduledRunConditionTypeAccepted,
			Status:             metav1.ConditionFalse,
			Reason:             "FrequencyTooHigh",
			Message:            fmt.Sprintf("Schedule interval must be at least 1 hour, got %v", second.Sub(first)),
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

	// Update the cron schedule
	if err := r.Scheduler.UpdateSchedule(&sr); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update schedule: %w", err)
	}

	// Compute next run time
	nextRun := sched.Next(metav1.Now().Time)
	nextRunTime := metav1.NewTime(nextRun)
	sr.Status.NextRunTime = &nextRunTime

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

// SetupWithManager sets up the controller with the Manager.
func (r *ScheduledRunController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			NeedLeaderElection: new(true),
		}).
		For(&v1alpha2.ScheduledRun{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named("scheduledrun").
		Complete(r)
}
