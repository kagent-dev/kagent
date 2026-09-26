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

func equalityTestReconciliation() AgentReconciliation {
	return AgentReconciliation{
		Agent: &kagentv1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "agent"}},
		Revision: &v2translator.Revision{
			Namespace: "test", AgentName: "agent",
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

func TestAgentReconciliationEquality(t *testing.T) {
	require.True(t, krt.Equal(AgentReconciliation{}, AgentReconciliation{}))
	for _, tt := range []struct {
		name   string
		change func(*AgentReconciliation)
		equal  bool
	}{
		{name: "identical", change: func(*AgentReconciliation) {}, equal: true},
		{name: "protobuf reflection", change: func(r *AgentReconciliation) {
			r.Revision.AgentCard.ProtoReflect()
			r.DesiredActorTemplate.ProtoReflect()
			r.ObservedActorTemplate.ProtoReflect()
		}, equal: true},
		{name: "protobuf caches", change: func(r *AgentReconciliation) {
			proto.Size(r.Revision.AgentCard)
			proto.Size(r.DesiredActorTemplate)
			proto.Size(r.ObservedActorTemplate)
		}, equal: true},
		{name: "template source", change: func(r *AgentReconciliation) { r.Agent.Generation++ }},
		{name: "revision identity", change: func(r *AgentReconciliation) { r.RevisionID[0]++ }},
		{name: "revision inputs", change: func(r *AgentReconciliation) { r.Revision.SandboxClass = "microvm" }},
		{name: "agent card", change: func(r *AgentReconciliation) { r.Revision.AgentCard.Name = "changed" }},
		{name: "missing agent card", change: func(r *AgentReconciliation) { r.Revision.AgentCard = nil }},
		{name: "missing revision", change: func(r *AgentReconciliation) { r.Revision = nil }},
		{name: "warning", change: func(r *AgentReconciliation) { r.Warnings[0] = "changed" }},
		{name: "desired template", change: func(r *AgentReconciliation) { r.DesiredActorTemplate.Metadata.Name = "changed" }},
		{name: "observed template identity", change: func(r *AgentReconciliation) { r.ObservedActorTemplate.Metadata.Uid = "changed" }},
		{name: "observed template status", change: func(r *AgentReconciliation) {
			r.ObservedActorTemplate.Status.GoldenSnapshotStatus.ErrorMessage = "changed"
		}},
		{name: "missing desired template", change: func(r *AgentReconciliation) { r.DesiredActorTemplate = nil }},
		{name: "missing observed template", change: func(r *AgentReconciliation) { r.ObservedActorTemplate = nil }},
		{name: "failure", change: func(r *AgentReconciliation) { r.Failure.Message = "changed" }},
		{name: "retryability", change: func(r *AgentReconciliation) { r.Failure.Retryable = false }},
		{name: "failure cleared", change: func(r *AgentReconciliation) { r.Failure = nil }},
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

func equalityTestObservation() AgentRuntimeObservation {
	state := equalityTestReconciliation()
	return AgentRuntimeObservation{
		Namespace: "test", AgentName: "agent",
		RevisionID: state.RevisionID, Template: state.ObservedActorTemplate, Failure: state.Failure,
	}
}

func TestAgentRuntimeObservationEquality(t *testing.T) {
	require.True(t, krt.Equal(AgentRuntimeObservation{}, AgentRuntimeObservation{}))
	for _, tt := range []struct {
		name   string
		change func(*AgentRuntimeObservation)
		equal  bool
	}{
		{name: "identical", change: func(*AgentRuntimeObservation) {}, equal: true},
		{name: "protobuf reflection", change: func(o *AgentRuntimeObservation) { o.Template.ProtoReflect() }, equal: true},
		{name: "protobuf caches", change: func(o *AgentRuntimeObservation) { proto.Size(o.Template) }, equal: true},
		{name: "namespace", change: func(o *AgentRuntimeObservation) { o.Namespace = "other" }},
		{name: "agent", change: func(o *AgentRuntimeObservation) { o.AgentName = "other" }},
		{name: "revision", change: func(o *AgentRuntimeObservation) { o.RevisionID[0]++ }},
		{name: "template identity", change: func(o *AgentRuntimeObservation) { o.Template.Metadata.Uid = "changed" }},
		{name: "template status", change: func(o *AgentRuntimeObservation) { o.Template.Status.GoldenSnapshotStatus.ErrorMessage = "changed" }},
		{name: "missing template", change: func(o *AgentRuntimeObservation) { o.Template = nil }},
		{name: "failure", change: func(o *AgentRuntimeObservation) { o.Failure.Message = "changed" }},
		{name: "retryability", change: func(o *AgentRuntimeObservation) { o.Failure.Retryable = false }},
		{name: "failure cleared", change: func(o *AgentRuntimeObservation) { o.Failure = nil }},
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
