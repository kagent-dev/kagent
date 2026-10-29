package controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/uuid"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/scheduling"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type ScheduledRunReconciliation struct {
	Resource *v1alpha3.ScheduledRun
	AgentUID string
	// FailureReason is the Ready condition reason when Failure is set.
	FailureReason string
	Failure       string
}

func (s *ScheduledRunReconciliation) fail(reason, message string) *ScheduledRunReconciliation {
	s.FailureReason, s.Failure = reason, message
	return s
}

func (s ScheduledRunReconciliation) ResourceName() string {
	return s.Resource.Namespace + "/" + s.Resource.Name
}
func (s ScheduledRunReconciliation) Equals(other ScheduledRunReconciliation) bool {
	return s.Resource.UID == other.Resource.UID && s.Resource.Generation == other.Resource.Generation &&
		s.Resource.DeletionTimestamp.Equal(other.Resource.DeletionTimestamp) &&
		reflect.DeepEqual(s.Resource.Spec, other.Resource.Spec) &&
		s.AgentUID == other.AgentUID && s.FailureReason == other.FailureReason && s.Failure == other.Failure
}

// Ready condition reasons for ScheduledRuns that cannot be scheduled.
const (
	ScheduledRunReasonInvalidConfiguration = "InvalidConfiguration"
	ScheduledRunReasonAgentNotFound        = "AgentNotFound"
	ScheduledRunReasonAgentNotReady        = "AgentNotReady"
	ScheduledRunReasonMemoryNotSupported   = "MemoryNotSupported"
)

func newScheduledRunReconciliations(resources krt.Collection[*v1alpha3.ScheduledRun], agents krt.Collection[*v1alpha3.Agent], harnesses krt.Collection[*v1alpha3.Harness], opts krt.OptionsBuilder) krt.Collection[ScheduledRunReconciliation] {
	return krt.NewCollection(resources, func(ctx krt.HandlerContext, resource *v1alpha3.ScheduledRun) *ScheduledRunReconciliation {
		state := &ScheduledRunReconciliation{Resource: resource}
		if !resource.DeletionTimestamp.IsZero() {
			return state
		}
		if err := scheduling.Validate(resource.Spec); err != nil {
			return state.fail(ScheduledRunReasonInvalidConfiguration, err.Error())
		}
		agent := krt.FetchOne(ctx, agents, krt.FilterKey(resource.Namespace+"/"+resource.Spec.AgentRef.Name))
		if agent == nil || !(*agent).DeletionTimestamp.IsZero() {
			return state.fail(ScheduledRunReasonAgentNotFound, "Agent does not exist")
		}
		state.AgentUID = string((*agent).UID)
		// Every scheduled Session is created by the shared controller principal, so
		// memory would be one partition across schedules and their continuers.
		harness := krt.FetchOne(ctx, harnesses, krt.FilterKey(resource.Namespace+"/"+(*agent).Spec.HarnessRef.Name))
		if harness != nil && (*harness).Spec.Kagent != nil && (*harness).Spec.Kagent.Memory != nil {
			return state.fail(ScheduledRunReasonMemoryNotSupported, "Scheduled runs do not support Agents whose Harness enables memory")
		}
		if (*agent).Status.LatestSuccessfulRevision == "" {
			return state.fail(ScheduledRunReasonAgentNotReady, "Agent has no successful runtime revision yet")
		}
		return state
	}, opts.WithName("ScheduledRunReconciliations")...)
}

type scheduleProjectionStore interface {
	SyncScheduledRun(context.Context, *database.ScheduledRun) (*database.ScheduledRun, error)
	GetActiveScheduledRunID(context.Context, types.NamespacedName) (uuid.UUID, error)
	RetireScheduledRun(context.Context, uuid.UUID) error
	DisableScheduledRun(context.Context, uuid.UUID, int64) error
}

// ScheduledRunReconciler is the sole writer of desired scheduling projections.
// Its KRT dependency on Agent tracks target identity and runtime readiness.
type ScheduledRunReconciler struct {
	states  krt.Collection[ScheduledRunReconciliation]
	store   scheduleProjectionStore
	kube    client.Client
	reader  client.Reader
	caller  auth.Session
	queue   controllers.Queue
	handler krt.HandlerRegistration
	ctx     context.Context
}

func NewScheduledRunReconciler(states krt.Collection[ScheduledRunReconciliation], store scheduleProjectionStore, kube client.Client, reader client.Reader, caller auth.Session) *ScheduledRunReconciler {
	r := &ScheduledRunReconciler{states: states, store: store, kube: kube, reader: reader, caller: caller}
	r.queue = newReconciliationQueue("scheduled-runs", func(key any) error { return r.reconcile(r.ctx, key.(string)) })
	r.handler = states.Register(func(event krt.Event[ScheduledRunReconciliation]) {
		if event.New != nil {
			r.queue.Add(event.New.ResourceName())
		} else if event.Old != nil {
			r.queue.Add(event.Old.ResourceName())
		}
	})
	return r
}

func (*ScheduledRunReconciler) NeedLeaderElection() bool { return true }
func (r *ScheduledRunReconciler) Start(ctx context.Context) error {
	r.ctx = ctx
	if !r.handler.WaitUntilSynced(ctx.Done()) {
		r.queue.ShutDownEarly()
		return nil
	}
	r.queue.Run(ctx.Done())
	return nil
}

func (r *ScheduledRunReconciler) reconcile(ctx context.Context, key string) error {
	namespace, name, _ := strings.Cut(key, "/")
	ref := types.NamespacedName{Namespace: namespace, Name: name}
	// Read the projection before the live resource. Retiring only this observed
	// UID cannot accidentally retire a concurrently reconciled replacement.
	previousID, err := r.store.GetActiveScheduledRunID(ctx, ref)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return err
	}
	resource := &v1alpha3.ScheduledRun{}
	if err := r.reader.Get(ctx, ref, resource); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		if previousID != uuid.Nil {
			return r.store.RetireScheduledRun(ctx, previousID)
		}
		return nil
	}
	id, err := uuid.Parse(string(resource.UID))
	if err != nil {
		return fmt.Errorf("invalid ScheduledRun UID: %w", err)
	}
	if previousID != uuid.Nil && previousID != id {
		if err := r.store.RetireScheduledRun(ctx, previousID); err != nil {
			return err
		}
	}
	if !resource.DeletionTimestamp.IsZero() {
		if err := r.store.RetireScheduledRun(ctx, id); err != nil {
			return err
		}
		return nil
	}
	state := r.states.GetKey(key)
	if state == nil || state.Resource.UID != resource.UID || state.Resource.Generation != resource.Generation {
		// KRT will enqueue the authoritative resource when its watch catches up.
		return nil
	}

	condition := metav1.Condition{Type: v1alpha3.ScheduledRunConditionReady, Status: metav1.ConditionTrue, Reason: "Reconciled", Message: "Schedule configuration is synchronized", ObservedGeneration: resource.Generation}
	failure := state.Failure
	if failure != "" {
		condition.Reason = state.FailureReason
	}
	if r.caller == nil {
		condition.Reason = "SchedulerNotConfigured"
		failure = "Scheduler service-account authentication is not configured"
	}
	if failure != "" {
		if err := r.store.DisableScheduledRun(ctx, id, resource.Generation); err != nil {
			return err
		}
		condition.Status, condition.Message = metav1.ConditionFalse, failure
	} else {
		_, err := r.store.SyncScheduledRun(ctx, &database.ScheduledRun{
			ID: id.String(), Creator: r.caller.Principal().ID,
			Ref:        types.NamespacedName{Namespace: resource.Namespace, Name: resource.Name},
			Generation: resource.Generation, ResourceVersion: resource.ResourceVersion, Spec: resource.Spec, AgentUID: state.AgentUID,
		})
		if err != nil {
			return err
		}
	}
	previous := resource.Status.DeepCopy()
	resource.Status.ObservedGeneration = resource.Generation
	meta.SetStatusCondition(&resource.Status.Conditions, condition)
	if reflect.DeepEqual(*previous, resource.Status) {
		return nil
	}
	if err := r.kube.Status().Update(ctx, resource); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}
