package controller

import (
	"sync"
	"testing"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/protobuf/proto"
	"istio.io/istio/pkg/kube/krt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func equalityTestReconciliation() AgentReconciliation {
	return AgentReconciliation{
		Agent: &kagentv1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "agent"}},
		Target: &compiledTarget{Revision: translator.Revision{
			Namespace: "test", AgentName: "agent",
			AgentCard: &a2apb.AgentCard{Name: "agent"},
		},
			RevisionID: translator.RevisionID{1},
			ActorTemplate: &ateapipb.ActorTemplate{
				Metadata: &ateapipb.ResourceMetadata{Atespace: "test", Name: "runtime"},
			},
		},
		Warnings:           []string{"warning"},
		PreparationFailure: &ReconciliationFailure{Condition: "Ready", Reason: "RuntimePreparationFailed", Message: "retry", Retryable: true},
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
			r.Target.Revision.AgentCard.ProtoReflect()
			r.Target.ActorTemplate.ProtoReflect()
		}, equal: true},
		{name: "protobuf caches", change: func(r *AgentReconciliation) {
			proto.Size(r.Target.Revision.AgentCard)
			proto.Size(r.Target.ActorTemplate)
		}, equal: true},
		{name: "template source", change: func(r *AgentReconciliation) { r.Agent.Generation++ }},
		{name: "live grants", change: func(r *AgentReconciliation) {
			r.EgressGrant.Destinations = []string{"https://tickets.example.com:443"}
		}},
		{name: "revision identity", change: func(r *AgentReconciliation) { r.Target.RevisionID[0]++ }},
		{name: "revision inputs", change: func(r *AgentReconciliation) { r.Target.Revision.SandboxClass = "microvm" }},
		{name: "agent card", change: func(r *AgentReconciliation) { r.Target.Revision.AgentCard.Name = "changed" }},
		{name: "missing agent card", change: func(r *AgentReconciliation) { r.Target.Revision.AgentCard = nil }},
		{name: "missing revision", change: func(r *AgentReconciliation) { r.Target = nil }},
		{name: "warning", change: func(r *AgentReconciliation) { r.Warnings[0] = "changed" }},
		{name: "desired template", change: func(r *AgentReconciliation) { r.Target.ActorTemplate.Metadata.Name = "changed" }},
		{name: "failure", change: func(r *AgentReconciliation) { r.PreparationFailure.Message = "changed" }},
		{name: "retryability", change: func(r *AgentReconciliation) { r.PreparationFailure.Retryable = false }},
		{name: "failure cleared", change: func(r *AgentReconciliation) { r.PreparationFailure = nil }},
		{name: "compilation failure", change: func(r *AgentReconciliation) {
			r.CompilationFailure = &ReconciliationFailure{Reason: "ActorTemplateInvalid"}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			left, right := equalityTestReconciliation(), equalityTestReconciliation()
			tt.change(&right)
			require.Equal(t, tt.equal, krt.Equal(left, right))
			require.Equal(t, tt.equal, krt.Equal(right, left))
			require.NotNil(t, left.Target.Revision.AgentCard, "comparison must not mutate its inputs")
			require.NotNil(t, left.Target.ActorTemplate)
		})
	}
}

func TestReconciliationEqualityDuringProtobufReads(t *testing.T) {
	var wg sync.WaitGroup
	defer wg.Wait()
	for range 100 {
		left, right := equalityTestReconciliation(), equalityTestReconciliation()
		start := make(chan struct{})
		wg.Go(func() {
			<-start
			for range 10 {
				proto.CloneOf(left.Target.ActorTemplate)
				proto.Size(left.Target.Revision.AgentCard)
			}
		})
		close(start)
		for range 10 {
			require.True(t, krt.Equal(left, right))
		}
	}
}
