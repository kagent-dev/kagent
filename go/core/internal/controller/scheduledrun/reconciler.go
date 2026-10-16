package scheduledrun

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"google.golang.org/protobuf/types/known/durationpb"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	schedules "github.com/kagent-dev/kagent/go/api/scheduledrun"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
)

type reconcilerStore interface {
	ApplyScheduledRun(context.Context, *apiv1.ScheduledRun, string) (*apiv1.ScheduledRun, error)
	RetireScheduledRuns(context.Context, string) error
}

// Reconciler persists ScheduledRun resources as control-plane schedules. The
// Scheduler and execution Controller then treat them like API-created
// schedules; only the creator differs, so no human principal can edit them.
type Reconciler struct {
	client client.Client
	store  reconcilerStore
}

func NewReconciler(client client.Client, store reconcilerStore) *Reconciler {
	return &Reconciler{client: client, store: store}
}

func (r *Reconciler) SetupWithManager(manager ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(manager).
		For(&v1alpha3.ScheduledRun{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		// Only Agent existence is observed, so create and delete events suffice.
		Watches(&v1alpha3.Agent{}, handler.EnqueueRequestsFromMapFunc(r.requestsForAgent), builder.WithPredicates(predicate.Funcs{
			UpdateFunc: func(event.UpdateEvent) bool { return false },
		})).
		Complete(r)
}

// retirementFinalizer keeps a ScheduledRun until its schedule is retired, so a
// deletion while no controller is running is still observed on restart.
const retirementFinalizer = "kagent.dev/scheduledrun-retirement"

func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	creator := schedules.KubernetesCreator(request.Namespace, request.Name)
	schedule := &v1alpha3.ScheduledRun{}
	if err := r.client.Get(ctx, request.NamespacedName, schedule); err != nil {
		if !apierrors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
		// Covers objects whose finalizer was removed by hand. Idempotent.
		return reconcile.Result{}, r.store.RetireScheduledRuns(ctx, creator)
	}
	if !schedule.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(schedule, retirementFinalizer) {
			return reconcile.Result{}, nil
		}
		// The name cannot be reused until the finalizer is removed, so every live
		// row of this creator belongs to the object being deleted.
		if err := r.store.RetireScheduledRuns(ctx, creator); err != nil {
			return reconcile.Result{}, err
		}
		controllerutil.RemoveFinalizer(schedule, retirementFinalizer)
		return reconcile.Result{}, r.client.Update(ctx, schedule)
	}
	if controllerutil.AddFinalizer(schedule, retirementFinalizer) {
		// Persist the finalizer before the row exists, so it can never outlive the object.
		if err := r.client.Update(ctx, schedule); err != nil {
			return reconcile.Result{}, fmt.Errorf("add ScheduledRun finalizer: %w", err)
		}
	}
	status := schedule.Status.DeepCopy()
	status.ObservedGeneration = schedule.Generation
	condition := func(conditionType string, conditionStatus metav1.ConditionStatus, reason, message string) {
		apiMeta.SetStatusCondition(&status.Conditions, metav1.Condition{
			Type: conditionType, Status: conditionStatus, Reason: reason, Message: message,
			ObservedGeneration: schedule.Generation,
		})
	}

	resolved, err := r.resolveAgent(ctx, schedule)
	if err != nil {
		return reconcile.Result{}, err
	}
	if resolved {
		condition(v1alpha3.ScheduledRunConditionResolvedRefs, metav1.ConditionTrue, "ResolvedRefs", "Agent "+schedule.Spec.AgentRef.Name+" exists")
	} else {
		condition(v1alpha3.ScheduledRunConditionResolvedRefs, metav1.ConditionFalse, "AgentNotFound", "Agent "+schedule.Spec.AgentRef.Name+" does not exist; occurrences fail until it does")
	}

	config := configFromSpec(schedule.Spec)
	if err := schedules.Validate(config); err != nil {
		// The last accepted schedule, if any, keeps running: deletion is final per
		// request, so an invalid edit must not retire the row.
		condition(v1alpha3.ScheduledRunConditionAccepted, metav1.ConditionFalse, "InvalidSchedule", err.Error())
		condition(v1alpha3.ScheduledRunConditionReady, metav1.ConditionFalse, "NotAccepted", "The schedule was not accepted")
		return reconcile.Result{}, r.updateStatus(ctx, schedule, status)
	}
	condition(v1alpha3.ScheduledRunConditionAccepted, metav1.ConditionTrue, "Accepted", "The schedule is valid")

	stored, err := r.store.ApplyScheduledRun(ctx, &apiv1.ScheduledRun{
		Creator: creator,
		Agent:   &apiv1.ResourceReference{Namespace: schedule.Namespace, Name: schedule.Spec.AgentRef.Name},
		Config:  config,
	}, string(schedule.UID))
	if err != nil {
		condition(v1alpha3.ScheduledRunConditionReady, metav1.ConditionFalse, "PersistenceFailed", err.Error())
		return reconcile.Result{}, errors.Join(fmt.Errorf("apply ScheduledRun: %w", err), r.updateStatus(ctx, schedule, status))
	}
	state := "active"
	if config.GetPaused() {
		state = "paused"
	}
	condition(v1alpha3.ScheduledRunConditionReady, metav1.ConditionTrue, "Scheduled", "Scheduled run "+stored.GetId()+" is "+state)
	return reconcile.Result{}, r.updateStatus(ctx, schedule, status)
}

// resolveAgent reports whether the referenced Agent exists. Readiness is left
// to execution, matching the gRPC service.
func (r *Reconciler) resolveAgent(ctx context.Context, schedule *v1alpha3.ScheduledRun) (bool, error) {
	err := r.client.Get(ctx, types.NamespacedName{Namespace: schedule.Namespace, Name: schedule.Spec.AgentRef.Name}, &v1alpha3.Agent{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("resolve Agent %s: %w", schedule.Spec.AgentRef.Name, err)
	}
	return true, nil
}

func (r *Reconciler) updateStatus(ctx context.Context, schedule *v1alpha3.ScheduledRun, status *v1alpha3.ScheduledRunStatus) error {
	if reflect.DeepEqual(schedule.Status, *status) {
		return nil
	}
	original := schedule.DeepCopy()
	schedule.Status = *status
	if err := r.client.Status().Patch(ctx, schedule, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("update ScheduledRun status: %w", err)
	}
	return nil
}

func (r *Reconciler) requestsForAgent(ctx context.Context, object client.Object) []reconcile.Request {
	list := &v1alpha3.ScheduledRunList{}
	if err := r.client.List(ctx, list, client.InNamespace(object.GetNamespace())); err != nil {
		return nil
	}
	var requests []reconcile.Request
	for i := range list.Items {
		if list.Items[i].Spec.AgentRef.Name == object.GetName() {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
		}
	}
	return requests
}

// configFromSpec translates the declared spec into the normalized control-plane
// configuration. Omitted optional fields take the same defaults as the gRPC API.
func configFromSpec(spec v1alpha3.ScheduledRunSpec) *apiv1.ScheduledRunConfig {
	config := &apiv1.ScheduledRunConfig{Schedule: spec.Schedule, Prompt: spec.Prompt}
	if spec.TimeZone != nil {
		config.TimeZone = *spec.TimeZone
	}
	if spec.Paused != nil {
		config.Paused = *spec.Paused
	}
	if spec.ExecutionTimeout != nil {
		config.ExecutionTimeout = durationpb.New(spec.ExecutionTimeout.Duration)
	}
	return schedules.Normalize(config)
}

var (
	_ reconcile.Reconciler = (*Reconciler)(nil)
	_ reconcilerStore      = (*database.Client)(nil)
)
