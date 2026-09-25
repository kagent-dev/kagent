package controller

import (
	"sync"
	"testing"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"istio.io/istio/pkg/kube/krt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func equalityTestReconciliation() PairReconciliation {
	return PairReconciliation{
		Pair: AgentTemplateHarnessPair{
			AgentTemplate: &kagentv1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "agent"}},
			Harness:       &kagentv1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "harness"}},
		},
		Revision: &v2translator.Revision{
			Namespace: "test", AgentTemplateName: "agent", HarnessName: "harness",
			AgentCard: &a2apb.AgentCard{Name: "agent"},
		},
		RevisionID: v2translator.RevisionID{1},
		Warnings:   []string{"warning"},
		DesiredActorTemplate: &ateapipb.ActorTemplate{
			Metadata: &ateapipb.ResourceMetadata{Atespace: "test", Name: "runtime"},
		},
		ObservedActorTemplate: &ateapipb.ActorTemplate{
			Metadata: &ateapipb.ResourceMetadata{Atespace: "test", Name: "runtime", Uid: "runtime-uid"},
			Status:   &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{ErrorMessage: "preparing"}},
		},
		Failure: &ReconciliationFailure{Condition: "Ready", Reason: "RuntimePreparationFailed", Message: "retry", Retryable: true},
	}
}

func TestPairReconciliationEquality(t *testing.T) {
	require.True(t, krt.Equal(PairReconciliation{}, PairReconciliation{}))
	for _, tt := range []struct {
		name   string
		change func(*PairReconciliation)
		equal  bool
	}{
		{name: "identical", change: func(*PairReconciliation) {}, equal: true},
		{name: "protobuf reflection", change: func(r *PairReconciliation) {
			r.Revision.AgentCard.ProtoReflect()
			r.DesiredActorTemplate.ProtoReflect()
			r.ObservedActorTemplate.ProtoReflect()
		}, equal: true},
		{name: "protobuf caches", change: func(r *PairReconciliation) {
			proto.Size(r.Revision.AgentCard)
			proto.Size(r.DesiredActorTemplate)
			proto.Size(r.ObservedActorTemplate)
		}, equal: true},
		{name: "template source", change: func(r *PairReconciliation) { r.Pair.AgentTemplate.Generation++ }},
		{name: "harness source", change: func(r *PairReconciliation) { r.Pair.Harness.Generation++ }},
		{name: "revision identity", change: func(r *PairReconciliation) { r.RevisionID[0]++ }},
		{name: "revision inputs", change: func(r *PairReconciliation) { r.Revision.SandboxClass = "microvm" }},
		{name: "agent card", change: func(r *PairReconciliation) { r.Revision.AgentCard.Name = "changed" }},
		{name: "missing agent card", change: func(r *PairReconciliation) { r.Revision.AgentCard = nil }},
		{name: "missing revision", change: func(r *PairReconciliation) { r.Revision = nil }},
		{name: "warning", change: func(r *PairReconciliation) { r.Warnings[0] = "changed" }},
		{name: "desired template", change: func(r *PairReconciliation) { r.DesiredActorTemplate.Metadata.Name = "changed" }},
		{name: "observed template identity", change: func(r *PairReconciliation) { r.ObservedActorTemplate.Metadata.Uid = "changed" }},
		{name: "observed template status", change: func(r *PairReconciliation) {
			r.ObservedActorTemplate.Status.GoldenSnapshotStatus.ErrorMessage = "changed"
		}},
		{name: "missing desired template", change: func(r *PairReconciliation) { r.DesiredActorTemplate = nil }},
		{name: "missing observed template", change: func(r *PairReconciliation) { r.ObservedActorTemplate = nil }},
		{name: "failure", change: func(r *PairReconciliation) { r.Failure.Message = "changed" }},
		{name: "retryability", change: func(r *PairReconciliation) { r.Failure.Retryable = false }},
		{name: "failure cleared", change: func(r *PairReconciliation) { r.Failure = nil }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			left, right := equalityTestReconciliation(), equalityTestReconciliation()
			tt.change(&right)
			require.Equal(t, tt.equal, krt.Equal(left, right))
			require.Equal(t, tt.equal, krt.Equal(right, left))
			require.NotNil(t, left.Revision.AgentCard, "comparison must not mutate its inputs")
			require.NotNil(t, left.DesiredActorTemplate)
			require.NotNil(t, left.ObservedActorTemplate)
		})
	}
}

func equalityTestObservation() PairRuntimeObservation {
	state := equalityTestReconciliation()
	return PairRuntimeObservation{
		Namespace: "test", AgentTemplateName: "agent", HarnessName: "harness",
		RevisionID: state.RevisionID, Template: state.ObservedActorTemplate, Failure: state.Failure,
	}
}

func TestPairRuntimeObservationEquality(t *testing.T) {
	require.True(t, krt.Equal(PairRuntimeObservation{}, PairRuntimeObservation{}))
	for _, tt := range []struct {
		name   string
		change func(*PairRuntimeObservation)
		equal  bool
	}{
		{name: "identical", change: func(*PairRuntimeObservation) {}, equal: true},
		{name: "protobuf reflection", change: func(o *PairRuntimeObservation) { o.Template.ProtoReflect() }, equal: true},
		{name: "protobuf caches", change: func(o *PairRuntimeObservation) { proto.Size(o.Template) }, equal: true},
		{name: "namespace", change: func(o *PairRuntimeObservation) { o.Namespace = "other" }},
		{name: "agent", change: func(o *PairRuntimeObservation) { o.AgentTemplateName = "other" }},
		{name: "harness", change: func(o *PairRuntimeObservation) { o.HarnessName = "other" }},
		{name: "revision", change: func(o *PairRuntimeObservation) { o.RevisionID[0]++ }},
		{name: "template identity", change: func(o *PairRuntimeObservation) { o.Template.Metadata.Uid = "changed" }},
		{name: "template status", change: func(o *PairRuntimeObservation) { o.Template.Status.GoldenSnapshotStatus.ErrorMessage = "changed" }},
		{name: "missing template", change: func(o *PairRuntimeObservation) { o.Template = nil }},
		{name: "failure", change: func(o *PairRuntimeObservation) { o.Failure.Message = "changed" }},
		{name: "retryability", change: func(o *PairRuntimeObservation) { o.Failure.Retryable = false }},
		{name: "failure cleared", change: func(o *PairRuntimeObservation) { o.Failure = nil }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			left, right := equalityTestObservation(), equalityTestObservation()
			tt.change(&right)
			require.Equal(t, tt.equal, krt.Equal(left, right))
			require.Equal(t, tt.equal, krt.Equal(right, left))
			require.NotNil(t, left.Template, "comparison must not mutate its inputs")
		})
	}
}

func TestPairEqualityDuringProtobufReads(t *testing.T) {
	var wg sync.WaitGroup
	defer wg.Wait()
	for range 100 {
		left, right := equalityTestReconciliation(), equalityTestReconciliation()
		observationLeft, observationRight := equalityTestObservation(), equalityTestObservation()
		start := make(chan struct{})
		wg.Go(func() {
			<-start
			for range 10 {
				proto.CloneOf(left.DesiredActorTemplate)
				proto.Size(left.ObservedActorTemplate)
				proto.Size(left.Revision.AgentCard)
				proto.Size(observationLeft.Template)
			}
		})
		close(start)
		for range 10 {
			require.True(t, krt.Equal(left, right))
			require.True(t, krt.Equal(observationLeft, observationRight))
		}
	}
}
