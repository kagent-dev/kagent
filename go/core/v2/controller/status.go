package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"istio.io/istio/pkg/kube/krt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func newAgentTemplateStatuses(templates krt.Collection[*kagentv1alpha3.AgentTemplate], states krt.Collection[PairReconciliation], opts krt.OptionsBuilder) krt.StatusCollection[*kagentv1alpha3.AgentTemplate, kagentv1alpha3.AgentTemplateStatus] {
	statesByTemplate := krt.NewIndex(states, "statesByAgentTemplate", func(state PairReconciliation) []string {
		return []string{state.Pair.AgentTemplate.Namespace + "/" + state.Pair.AgentTemplate.Name}
	})
	statuses, _ := krt.NewStatusManyCollection(templates, func(ctx krt.HandlerContext, template *kagentv1alpha3.AgentTemplate) (*kagentv1alpha3.AgentTemplateStatus, []PairReconciliation) {
		pairStates := statesByTemplate.Fetch(ctx, template.Namespace+"/"+template.Name)
		slices.SortFunc(pairStates, func(a, b PairReconciliation) int {
			return strings.Compare(a.Pair.Harness.Name, b.Pair.Harness.Name)
		})
		previous := make(map[string]string, len(template.Status.Harnesses))
		for _, status := range template.Status.Harnesses {
			previous[status.Harness] = status.LatestSuccessfulRevision
		}
		statuses := make([]kagentv1alpha3.AgentTemplateHarnessStatus, 0, len(pairStates))
		for _, state := range pairStates {
			statuses = append(statuses, statusForPair(state, template.Generation, previous[state.Pair.Harness.Name]))
		}
		return &kagentv1alpha3.AgentTemplateStatus{ObservedGeneration: template.Generation, Harnesses: statuses}, nil
	}, opts.WithName("AgentTemplateStatuses")...)
	return statuses
}

func statusForPair(state PairReconciliation, generation int64, latestSuccessful string) kagentv1alpha3.AgentTemplateHarnessStatus {
	desired := state.RevisionID.String()
	if state.RevisionID.IsZero() {
		desired = requestedRevision(state.Pair.AgentTemplate, state.Pair.Harness.Name)
	}
	status := kagentv1alpha3.AgentTemplateHarnessStatus{
		Harness: state.Pair.Harness.Name, DesiredRevision: desired, LatestSuccessfulRevision: latestSuccessful,
	}
	setPairCondition(&status, generation, kagentv1alpha3.AgentTemplateConditionAccepted, metav1.ConditionTrue, "Accepted", "Harness admission selector matches the AgentTemplate")
	if state.Failure != nil {
		if state.Failure.Condition != kagentv1alpha3.AgentTemplateConditionResolvedRefs {
			setPairCondition(&status, generation, kagentv1alpha3.AgentTemplateConditionResolvedRefs, metav1.ConditionTrue, "Resolved", "All runtime references resolved")
		}
		if state.Failure.Condition == kagentv1alpha3.AgentTemplateConditionReady {
			setPairCondition(&status, generation, kagentv1alpha3.AgentTemplateConditionCompatible, metav1.ConditionTrue, "Compatible", "Resolved configuration is compatible with the Harness")
		}
		setPairFailure(&status, generation, state.Failure)
		return status
	}
	setPairCondition(&status, generation, kagentv1alpha3.AgentTemplateConditionResolvedRefs, metav1.ConditionTrue, "Resolved", "All runtime references resolved")
	setPairCondition(&status, generation, kagentv1alpha3.AgentTemplateConditionCompatible, metav1.ConditionTrue, "Compatible", "Resolved configuration is compatible with the Harness")
	if state.ObservedActorTemplate == nil || state.ObservedActorTemplate.Status.Phase != atev1alpha1.PhaseReady {
		setPairCondition(&status, generation, kagentv1alpha3.AgentTemplateConditionReady, metav1.ConditionFalse, "ActorTemplatePending", "waiting for the ActorTemplate golden snapshot")
		return status
	}
	status.LatestSuccessfulRevision = state.RevisionID.String()
	setPairCondition(&status, generation, kagentv1alpha3.AgentTemplateConditionReady, metav1.ConditionTrue, "Ready", "ActorTemplate golden snapshot is ready")
	return status
}

func setPairFailure(status *kagentv1alpha3.AgentTemplateHarnessStatus, generation int64, failure *ReconciliationFailure) {
	stages := []string{kagentv1alpha3.AgentTemplateConditionResolvedRefs, kagentv1alpha3.AgentTemplateConditionCompatible, kagentv1alpha3.AgentTemplateConditionReady}
	failed := false
	for _, stage := range stages {
		if stage == failure.Condition {
			setPairCondition(status, generation, stage, metav1.ConditionFalse, failure.Reason, failure.Message)
			failed = true
			continue
		}
		if failed {
			setPairCondition(status, generation, stage, metav1.ConditionFalse, "Blocked", "blocked by "+failure.Condition)
		}
	}
}

func setPairCondition(status *kagentv1alpha3.AgentTemplateHarnessStatus, generation int64, conditionType string, conditionStatus metav1.ConditionStatus, reason, message string) {
	status.Conditions = append(status.Conditions, metav1.Condition{
		Type: conditionType, Status: conditionStatus, Reason: reason, Message: message, ObservedGeneration: generation,
	})
}

func requestedRevision(template *kagentv1alpha3.AgentTemplate, harness string) string {
	raw, _ := json.Marshal(struct {
		UID        types.UID
		Generation int64
		Spec       kagentv1alpha3.AgentTemplateSpec
		Harness    string
	}{template.UID, template.Generation, template.Spec, harness})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
