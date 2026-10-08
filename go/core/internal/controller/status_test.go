package controller

import (
	"context"
	"testing"
	"time"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	kagentfake "github.com/kagent-dev/kagent/go/api/clientset/versioned/fake"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestStatusForPairPublishesCompilationWarnings(t *testing.T) {
	warnings := []string{"partial MCP selection is not enforced"}
	state := AgentReconciliation{
		Agent:    &kagentv1alpha3.Agent{},
		Warnings: warnings,
		Target:   &compiledTarget{RevisionID: translator.RevisionID{1}},
	}
	status := statusForAgent(state, 1, "")
	if len(status.Warnings) != 1 || status.Warnings[0] != warnings[0] {
		t.Fatalf("warnings = %v, want %v", status.Warnings, warnings)
	}
	warnings[0] = "changed"
	if status.Warnings[0] == warnings[0] {
		t.Fatal("status aliases mutable compilation warnings")
	}
}

func harnessAgentState(name string, mutate func(*AgentReconciliation)) AgentReconciliation {
	state := AgentReconciliation{
		Agent: &kagentv1alpha3.Agent{
			ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: name},
			Spec:       kagentv1alpha3.AgentSpec{HarnessRef: &corev1.LocalObjectReference{Name: "claude"}},
		},
		Target:                &compiledTarget{RevisionID: translator.RevisionID{1}},
		ObservedActorTemplate: &ateapipb.ActorTemplate{},
	}
	if mutate != nil {
		mutate(&state)
	}
	return state
}

func booted(state *AgentReconciliation) {
	state.ObservedActorTemplate = &ateapipb.ActorTemplate{Status: &ateapipb.ActorTemplateStatus{
		GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{GoldenTag: &ateapipb.ObjectRef{Atespace: "ate-golden", Name: "golden"}},
	}}
}

func bootFailed(state *AgentReconciliation) {
	state.PreparationFailure = &ReconciliationFailure{Condition: kagentv1alpha3.AgentConditionReady, Reason: "ActorTemplateFailed", Message: "ImagePullBackOff"}
}

func TestHarnessReadyCondition(t *testing.T) {
	pool := types.NamespacedName{Namespace: "team-a", Name: "gvisor"}
	tests := []struct {
		name        string
		poolFound   bool
		agents      []AgentReconciliation
		wantStatus  metav1.ConditionStatus
		wantReason  string
		wantMessage string
	}{
		{name: "missing worker pool", agents: []AgentReconciliation{harnessAgentState("a", booted)},
			wantStatus: metav1.ConditionFalse, wantReason: "WorkerPoolNotFound", wantMessage: `WorkerPool "team-a/gvisor" not found`},
		{name: "no agents yet", poolFound: true,
			wantStatus: metav1.ConditionTrue, wantReason: "WorkerPoolResolved", wantMessage: "WorkerPool \"team-a/gvisor\" resolves; no Agent has booted on the Harness yet"},
		{name: "boot pending", poolFound: true, agents: []AgentReconciliation{harnessAgentState("a", nil)},
			wantStatus: metav1.ConditionTrue, wantReason: "WorkerPoolResolved", wantMessage: "WorkerPool \"team-a/gvisor\" resolves; waiting for the golden boot of Agent team-a/a"},
		{name: "booted", poolFound: true, agents: []AgentReconciliation{harnessAgentState("a", booted)},
			wantStatus: metav1.ConditionTrue, wantReason: "Booted", wantMessage: "golden boot of Agent team-a/a succeeded"},
		{name: "failed boot", poolFound: true, agents: []AgentReconciliation{harnessAgentState("a", bootFailed)},
			wantStatus: metav1.ConditionFalse, wantReason: "ActorTemplateFailed", wantMessage: "Agent team-a/a: ImagePullBackOff"},
		{name: "crashed boot retrying", poolFound: true, agents: []AgentReconciliation{harnessAgentState("a", func(s *AgentReconciliation) {
			s.GoldenBootRetry = &GoldenBootRetry{Attempt: 1, Message: "GoldenActorCrashed: exit 1"}
		})},
			wantStatus: metav1.ConditionFalse, wantReason: "ActorTemplateRetrying", wantMessage: "Agent team-a/a: golden boot 1 of 6 failed (GoldenActorCrashed: exit 1); starting it over"},
		{name: "one booted agent outweighs another's failure", poolFound: true, agents: []AgentReconciliation{harnessAgentState("a", bootFailed), harnessAgentState("b", booted)},
			wantStatus: metav1.ConditionTrue, wantReason: "Booted", wantMessage: "golden boot of Agent team-a/b succeeded"},
		{name: "a failure outweighs a pending boot", poolFound: true, agents: []AgentReconciliation{harnessAgentState("a", nil), harnessAgentState("b", bootFailed)},
			wantStatus: metav1.ConditionFalse, wantReason: "ActorTemplateFailed", wantMessage: "Agent team-a/b: ImagePullBackOff"},
		{name: "agent failures before the boot say nothing about the harness", poolFound: true, agents: []AgentReconciliation{
			harnessAgentState("a", func(s *AgentReconciliation) {
				s.Target = nil
				s.CompilationFailure = &ReconciliationFailure{Condition: kagentv1alpha3.AgentConditionCompatible, Reason: "UnsupportedConfiguration"}
			}),
			harnessAgentState("b", func(s *AgentReconciliation) {
				s.PreparationFailure = &ReconciliationFailure{Condition: kagentv1alpha3.AgentConditionCompatible, Reason: "ActorTemplateInvalid"}
			}),
		},
			wantStatus: metav1.ConditionTrue, wantReason: "WorkerPoolResolved", wantMessage: "WorkerPool \"team-a/gvisor\" resolves; no Agent has booted on the Harness yet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := harnessReadyCondition(3, pool, tt.poolFound, tt.agents)
			want := metav1.Condition{
				Type: kagentv1alpha3.HarnessConditionTypeReady, Status: tt.wantStatus, Reason: tt.wantReason, Message: tt.wantMessage, ObservedGeneration: 3,
			}
			require.Equal(t, want, got)
		})
	}
}

func TestReconcilerWritesHarnessReady(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	opts := krt.NewOptionsBuilder(stop, "test", nil)

	harness := &kagentv1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "claude", Generation: 2},
		Spec: kagentv1alpha3.HarnessSpec{Substrate: kagentv1alpha3.RuntimeSubstratePolicy{
			WorkerPoolRef: corev1.LocalObjectReference{Name: "gvisor"},
		}},
		Status: kagentv1alpha3.HarnessStatus{Capabilities: &kagentv1alpha3.HarnessCapabilities{Version: "v1"}},
	}
	mock := krttest.NewMock(t, []any{
		harness,
		&atev1alpha1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "gvisor"}},
		harnessAgentState("assistant", booted),
		harnessAgentState("elsewhere", func(s *AgentReconciliation) {
			s.Agent.Spec.HarnessRef.Name = "codex"
			bootFailed(s)
		}),
	})
	harnesses := krttest.GetMockCollection[*kagentv1alpha3.Harness](mock)
	states := krttest.GetMockCollection[AgentReconciliation](mock)
	collections := Collections{
		Harnesses:           harnesses,
		Reconciliations:     krt.NewStaticCollection[AgentReconciliation](nil, nil, opts.WithName("NoReconciliations")...),
		HarnessStatuses:     newHarnessStatuses(harnesses, krttest.GetMockCollection[*atev1alpha1.WorkerPool](mock), states, opts),
		AgentStatuses:       krttest.GetMockCollection[krt.ObjectWithStatus[*kagentv1alpha3.Agent, kagentv1alpha3.AgentStatus]](mock),
		ModelConfigStatuses: krttest.GetMockCollection[krt.ObjectWithStatus[*kagentv1alpha3.ModelConfig, kagentv1alpha3.ModelConfigStatus]](mock),
	}
	statusClient := kagentfake.NewSimpleClientset(harness.DeepCopy()).ApiV1alpha3()
	go newReconciler(collections, &fakeActorTemplates{}, &fakeRuntimeRevisionStore{}, statusClient).Run(stop)

	var updated *kagentv1alpha3.Harness
	require.Eventually(t, func() bool {
		var err error
		updated, err = statusClient.Harnesses("team-a").Get(context.Background(), "claude", metav1.GetOptions{})
		return err == nil && apimeta.IsStatusConditionTrue(updated.Status.Conditions, kagentv1alpha3.HarnessConditionTypeReady)
	}, 3*time.Second, 10*time.Millisecond)
	ready := apimeta.FindStatusCondition(updated.Status.Conditions, kagentv1alpha3.HarnessConditionTypeReady)
	require.Equal(t, "Booted", ready.Reason)
	require.False(t, ready.LastTransitionTime.IsZero())
	require.Equal(t, int64(2), updated.Status.ObservedGeneration)
	require.Equal(t, harness.Status.Capabilities, updated.Status.Capabilities, "the status write keeps the capabilities")
}
