package controller

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"google.golang.org/protobuf/proto"
	"istio.io/istio/pkg/kube/krt"
)

// Sandbox preparation uses the same informer graph as Agents. Only observations
// of external work are mutable; compilation never performs I/O.
type sandboxCollections struct {
	states       krt.Collection[sandboxReconciliation]
	observations krt.StaticCollection[sandboxRuntimeObservation]
	guestImage   krt.StaticSingleton[string]
	policy       substrate.SandboxPolicy
}

type sandboxReconciliation struct {
	Template              *kagentv1alpha3.SandboxTemplate
	RevisionID            string
	SourceSnapshot        json.RawMessage
	DesiredActorTemplate  *ateapipb.ActorTemplate
	ObservedActorTemplate *ateapipb.ActorTemplate
	NeedsGuestImage       bool
	CompilationError      string
	Failure               *ReconciliationFailure
}

func (s sandboxReconciliation) ResourceName() string {
	return s.Template.Namespace + "/" + s.Template.Name
}

var _ krt.Equaler[sandboxReconciliation] = sandboxReconciliation{}

func (s sandboxReconciliation) Equals(other sandboxReconciliation) bool {
	if !proto.Equal(s.DesiredActorTemplate, other.DesiredActorTemplate) || !proto.Equal(s.ObservedActorTemplate, other.ObservedActorTemplate) {
		return false
	}
	s.DesiredActorTemplate, other.DesiredActorTemplate = nil, nil
	s.ObservedActorTemplate, other.ObservedActorTemplate = nil, nil
	return reflect.DeepEqual(s, other)
}

func (s sandboxReconciliation) desiredRevision() string {
	if s.RevisionID != "" {
		return s.RevisionID
	}
	// Unresolved inputs must replace the old desired edge without inventing a
	// runnable revision. Kubernetes generation identifies the requested spec.
	return fmt.Sprintf("pending:%s:%d", s.Template.UID, s.Template.Generation)
}

func (s sandboxReconciliation) canPrepare() bool {
	return s.DesiredActorTemplate != nil && (s.Failure == nil || s.Failure.Retryable)
}

type sandboxRuntimeObservation struct {
	Key        string
	RevisionID string
	Template   *ateapipb.ActorTemplate
	Failure    *ReconciliationFailure
}

func (s sandboxRuntimeObservation) ResourceName() string { return s.Key }

var _ krt.Equaler[sandboxRuntimeObservation] = sandboxRuntimeObservation{}

func (s sandboxRuntimeObservation) Equals(other sandboxRuntimeObservation) bool {
	if !proto.Equal(s.Template, other.Template) {
		return false
	}
	s.Template, other.Template = nil, nil
	return reflect.DeepEqual(s, other)
}

func newSandboxCollections(inputs Collections, policy substrate.SandboxPolicy, opts krt.OptionsBuilder) sandboxCollections {
	observations := krt.NewStaticCollection[sandboxRuntimeObservation](nil, nil, opts.WithName("SandboxRuntimeObservations")...)
	guestImage := krt.NewStatic[string](nil, true, opts.WithName("SandboxGuestImage")...)
	states := krt.NewCollection(inputs.SandboxTemplates, func(ctx krt.HandlerContext, template *kagentv1alpha3.SandboxTemplate) *sandboxReconciliation {
		state := &sandboxReconciliation{Template: template}
		if !template.DeletionTimestamp.IsZero() {
			return state
		}
		pool := krt.FetchOne(ctx, inputs.WorkerPools, krt.FilterKey(template.Namespace+"/"+template.Spec.Substrate.WorkerPoolRef.Name))
		if pool == nil {
			state.Failure = &ReconciliationFailure{Reason: "WorkerPoolNotFound", Message: "The referenced sandbox WorkerPool does not exist"}
		} else if image := krt.FetchOne(ctx, guestImage.AsCollection()); image == nil {
			state.NeedsGuestImage = true
		} else {
			resolvedPolicy := policy
			resolvedPolicy.GuestImage = *image
			var err error
			state.DesiredActorTemplate, state.RevisionID, state.SourceSnapshot, err = substrate.SandboxActorTemplate(template, (*pool).Spec.SandboxClass, resolvedPolicy)
			if err != nil {
				state.CompilationError = err.Error()
				state.Failure = &ReconciliationFailure{Reason: sandboxPreparationFailed, Message: sandboxPreparationFailureMessage}
			}
		}
		observation := krt.FetchOne(ctx, observations, krt.FilterKey(state.ResourceName()))
		if observation == nil || observation.RevisionID != state.desiredRevision() {
			return state
		}
		if observation.Failure != nil {
			state.Failure = observation.Failure
		} else if state.Failure == nil {
			state.ObservedActorTemplate = observation.Template
		}
		return state
	}, opts.WithName("SandboxReconciliations")...)
	return sandboxCollections{states: states, observations: observations, guestImage: guestImage, policy: policy}
}
