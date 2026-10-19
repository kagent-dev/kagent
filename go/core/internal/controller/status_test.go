package controller

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"

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
