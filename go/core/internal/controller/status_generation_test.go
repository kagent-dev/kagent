package controller

import (
	"testing"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The Agent informer can deliver a new generation before the reconciliation
// of that generation exists. The status of the previous generation's
// reconciliation, Ready on its revision, must not claim the new generation:
// a caller waiting for Ready at the generation it wrote would create a
// Session on the previous revision.
func TestAgentStatusObservesTheReconciledGeneration(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	opts := krt.NewOptionsBuilder(stop, "test", nil)
	reconciled := &kagentv1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "assistant", Generation: 1}}
	updated := reconciled.DeepCopy()
	updated.Generation = 2
	state := AgentReconciliation{
		Agent:  reconciled,
		Target: &compiledTarget{RevisionID: translator.RevisionID{1}},
		ObservedActorTemplate: &ateapipb.ActorTemplate{Status: &ateapipb.ActorTemplateStatus{
			GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{GoldenTag: &ateapipb.ObjectRef{Atespace: "ate-golden", Name: "golden"}},
		}},
	}
	agents := krt.NewStaticCollection(nil, []*kagentv1alpha3.Agent{updated}, opts.WithName("Agents")...)
	reconciliations := krt.NewStaticCollection(nil, []AgentReconciliation{state}, opts.WithName("Reconciliations")...)
	statuses := newAgentStatuses(agents, reconciliations, opts)

	waitFor(t, func() bool { return len(statuses.List()) == 1 })
	status := statuses.List()[0].Status
	require.Equal(t, int64(1), status.ObservedGeneration)
	ready := apimeta.FindStatusCondition(status.Conditions, kagentv1alpha3.AgentConditionReady)
	require.NotNil(t, ready)
	require.Equal(t, metav1.ConditionTrue, ready.Status)
	require.Equal(t, int64(1), ready.ObservedGeneration, "Ready describes the reconciled generation, not the Agent's newer one")
}
