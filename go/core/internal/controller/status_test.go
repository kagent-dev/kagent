package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	atev1alpha1 "go.ate.dev/substrate/pkg/api/v1alpha1"
	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kagentfake "github.com/kagent-dev/kagent/go/api/clientset/versioned/fake"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
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

func TestStatusReportsDesiredWorkloadImage(t *testing.T) {
	image := "registry/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	state := AgentReconciliation{Target: &compiledTarget{Revision: translator.Revision{Image: image}}}
	status := statusForAgent(state, 1, "previous-revision")
	require.Equal(t, new(image), status.WorkloadImage)
	require.Equal(t, "previous-revision", status.LatestSuccessfulRevision)
	state.Target = nil
	state.CompilationFailure = &ReconciliationFailure{Condition: kagentv1alpha3.AgentConditionCompatible, Reason: "UnsupportedConfiguration", Message: "no default image configured"}
	status = statusForAgent(state, 2, "previous-revision")
	require.Nil(t, status.WorkloadImage)
	require.Equal(t, "previous-revision", status.LatestSuccessfulRevision)
}

func TestEgressConditionSurvivesRuntimeFailureAndBoundsMessages(t *testing.T) {
	issues := []translator.EgressGrantIssue{
		{Policy: "team/missing", Reason: kagentv1alpha3.EgressPoliciesReasonNotFound, Message: "missing"},
	}
	state := AgentReconciliation{EgressIssues: issues, EgressConflict: strings.Repeat("conflict", 1000),
		CompilationFailure: &ReconciliationFailure{Condition: kagentv1alpha3.AgentConditionResolvedRefs, Reason: "ReferenceResolutionFailed", Message: "Harness is missing"}}
	status := statusForAgent(state, 1, "")
	condition := meta.FindStatusCondition(status.Conditions, kagentv1alpha3.AgentConditionEgressPoliciesApplied)
	require.Equal(t, kagentv1alpha3.EgressPoliciesReasonConflict, condition.Reason)
	require.Contains(t, condition.Message, "new grants are held until the conflict is resolved")
	require.Contains(t, condition.Message, "team/missing")
	require.LessOrEqual(t, len(condition.Message), 4096)
	require.False(t, meta.IsStatusConditionTrue(status.Conditions, kagentv1alpha3.AgentConditionReady))
}

func harnessAgentState(name string, mutate func(*AgentReconciliation)) AgentReconciliation {
	state := AgentReconciliation{
		Agent: &kagentv1alpha3.Agent{
			ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: name},
			Spec:       kagentv1alpha3.AgentSpec{HarnessRef: corev1.LocalObjectReference{Name: "claude"}},
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
	require.Equal(t, metav1.Condition{
		Type: kagentv1alpha3.HarnessConditionTypeReady, Status: metav1.ConditionFalse, Reason: "WorkerPoolNotFound",
		Message: `WorkerPool "team-a/gvisor" not found`, ObservedGeneration: 3,
	}, harnessReadyCondition(3, pool, false))
	require.Equal(t, metav1.Condition{
		Type: kagentv1alpha3.HarnessConditionTypeReady, Status: metav1.ConditionTrue, Reason: "WorkerPoolResolved",
		Message: `WorkerPool "team-a/gvisor" resolves`, ObservedGeneration: 3,
	}, harnessReadyCondition(3, pool, true))
}

func TestHarnessBootedCondition(t *testing.T) {
	tests := []struct {
		name        string
		agents      []AgentReconciliation
		wantStatus  metav1.ConditionStatus
		wantReason  string
		wantMessage string
	}{
		{name: "no agents",
			wantStatus: metav1.ConditionUnknown, wantReason: "NoAgents", wantMessage: "no Agent runs on this Harness yet"},
		{name: "boot pending", agents: []AgentReconciliation{harnessAgentState("a", nil)},
			wantStatus: metav1.ConditionUnknown, wantReason: "BootPending", wantMessage: "waiting for the golden boot of Agent team-a/a"},
		{name: "booted", agents: []AgentReconciliation{harnessAgentState("a", booted)},
			wantStatus: metav1.ConditionTrue, wantReason: "Booted", wantMessage: "golden boot of Agent team-a/a succeeded"},
		{name: "failed boot", agents: []AgentReconciliation{harnessAgentState("a", bootFailed)},
			wantStatus: metav1.ConditionFalse, wantReason: "ActorTemplateFailed", wantMessage: "Agent team-a/a: ImagePullBackOff"},
		{name: "one booted agent outweighs another's failure", agents: []AgentReconciliation{harnessAgentState("a", bootFailed), harnessAgentState("b", booted)},
			wantStatus: metav1.ConditionTrue, wantReason: "Booted", wantMessage: "golden boot of Agent team-a/b succeeded"},
		{name: "a failure outweighs a pending boot", agents: []AgentReconciliation{harnessAgentState("a", nil), harnessAgentState("b", bootFailed)},
			wantStatus: metav1.ConditionFalse, wantReason: "ActorTemplateFailed", wantMessage: "Agent team-a/b: ImagePullBackOff"},
		{name: "agent failures before the boot say nothing about the harness", agents: []AgentReconciliation{
			harnessAgentState("a", func(s *AgentReconciliation) {
				s.Target = nil
				s.CompilationFailure = &ReconciliationFailure{Condition: kagentv1alpha3.AgentConditionCompatible, Reason: "UnsupportedConfiguration"}
			}),
			harnessAgentState("b", func(s *AgentReconciliation) {
				s.PreparationFailure = &ReconciliationFailure{Condition: kagentv1alpha3.AgentConditionCompatible, Reason: "ActorTemplateInvalid"}
			}),
		},
			wantStatus: metav1.ConditionUnknown, wantReason: "NoAgents", wantMessage: "no Agent runs on this Harness yet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := metav1.Condition{
				Type: kagentv1alpha3.HarnessConditionTypeBooted, Status: tt.wantStatus, Reason: tt.wantReason, Message: tt.wantMessage, ObservedGeneration: 3,
			}
			require.Equal(t, want, harnessBootedCondition(3, tt.agents))
		})
	}
}

func TestReconcilerWritesHarnessStatus(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	opts := krt.NewOptionsBuilder(ctx.Done(), "test", nil)

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
	reconciler := newReconciler(collections, &fakeActorTemplates{}, &fakeRuntimeRevisionStore{}, statusClient)
	go func() { assert.NoError(t, reconciler.Start(ctx)) }()

	var updated *kagentv1alpha3.Harness
	require.Eventually(t, func() bool {
		var err error
		updated, err = statusClient.Harnesses("team-a").Get(context.Background(), "claude", metav1.GetOptions{})
		return err == nil && meta.IsStatusConditionTrue(updated.Status.Conditions, kagentv1alpha3.HarnessConditionTypeBooted)
	}, 3*time.Second, 10*time.Millisecond)
	ready := meta.FindStatusCondition(updated.Status.Conditions, kagentv1alpha3.HarnessConditionTypeReady)
	require.Equal(t, metav1.ConditionTrue, ready.Status)
	require.Equal(t, "WorkerPoolResolved", ready.Reason)
	bootedCondition := meta.FindStatusCondition(updated.Status.Conditions, kagentv1alpha3.HarnessConditionTypeBooted)
	require.Equal(t, "Booted", bootedCondition.Reason)
	require.Equal(t, "golden boot of Agent team-a/assistant succeeded", bootedCondition.Message, "the failed boot on another Harness does not count")
	require.False(t, bootedCondition.LastTransitionTime.IsZero())
	require.Equal(t, int64(2), updated.Status.ObservedGeneration)
	require.Equal(t, harness.Status.Capabilities, updated.Status.Capabilities, "the status write keeps the capabilities")
}
