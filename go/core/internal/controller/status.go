package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"istio.io/istio/pkg/kube/krt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func newAgentStatuses(agents krt.Collection[*kagentv1alpha3.Agent], states krt.Collection[AgentReconciliation], opts krt.OptionsBuilder) krt.StatusCollection[*kagentv1alpha3.Agent, kagentv1alpha3.AgentStatus] {
	statuses, _ := krt.NewStatusManyCollection(agents, func(ctx krt.HandlerContext, agent *kagentv1alpha3.Agent) (*kagentv1alpha3.AgentStatus, []AgentReconciliation) {
		state := krt.FetchOne(ctx, states, krt.FilterKey(agent.Namespace+"/"+agent.Name))
		if state == nil {
			return nil, nil
		}
		status := statusForAgent(*state, agent.Generation, agent.Status.LatestSuccessfulRevision)
		return &status, nil
	}, opts.WithName("AgentStatuses")...)
	return statuses
}

func statusForAgent(state AgentReconciliation, generation int64, latestSuccessful string) kagentv1alpha3.AgentStatus {
	status := kagentv1alpha3.AgentStatus{
		ObservedGeneration: generation, DesiredRevision: state.desiredRevision(), LatestSuccessfulRevision: latestSuccessful,
	}
	status.Warnings = append([]string(nil), state.Warnings...)
	setAgentCondition(&status, generation, kagentv1alpha3.AgentConditionAccepted, metav1.ConditionTrue, "Accepted", "Agent explicitly selects its template and harness")
	if state.Failure != nil {
		if state.Failure.Condition != kagentv1alpha3.AgentConditionResolvedRefs {
			setAgentCondition(&status, generation, kagentv1alpha3.AgentConditionResolvedRefs, metav1.ConditionTrue, "Resolved", "All runtime references resolved")
		}
		if state.Failure.Condition == kagentv1alpha3.AgentConditionReady {
			setAgentCondition(&status, generation, kagentv1alpha3.AgentConditionCompatible, metav1.ConditionTrue, "Compatible", "Resolved configuration is compatible with the Harness")
		}
		setAgentFailure(&status, generation, state.Failure)
		return status
	}
	setAgentCondition(&status, generation, kagentv1alpha3.AgentConditionResolvedRefs, metav1.ConditionTrue, "Resolved", "All runtime references resolved")
	setAgentCondition(&status, generation, kagentv1alpha3.AgentConditionCompatible, metav1.ConditionTrue, "Compatible", "Resolved configuration is compatible with the Harness")
	if state.ObservedActorTemplate.GetStatus().GetGoldenSnapshotStatus().GetGoldenTag() == nil {
		setAgentCondition(&status, generation, kagentv1alpha3.AgentConditionReady, metav1.ConditionFalse, "ActorTemplatePending", "waiting for the ActorTemplate golden snapshot")
		return status
	}
	status.LatestSuccessfulRevision = state.RevisionID.String()
	setAgentCondition(&status, generation, kagentv1alpha3.AgentConditionReady, metav1.ConditionTrue, "Ready", "ActorTemplate golden snapshot is ready")
	return status
}

func setAgentFailure(status *kagentv1alpha3.AgentStatus, generation int64, failure *ReconciliationFailure) {
	stages := []string{kagentv1alpha3.AgentConditionResolvedRefs, kagentv1alpha3.AgentConditionCompatible, kagentv1alpha3.AgentConditionReady}
	failed := false
	for _, stage := range stages {
		if stage == failure.Condition {
			setAgentCondition(status, generation, stage, metav1.ConditionFalse, failure.Reason, failure.Message)
			failed = true
			continue
		}
		if failed {
			setAgentCondition(status, generation, stage, metav1.ConditionFalse, "Blocked", "blocked by "+failure.Condition)
		}
	}
}

func setAgentCondition(status *kagentv1alpha3.AgentStatus, generation int64, conditionType string, conditionStatus metav1.ConditionStatus, reason, message string) {
	status.Conditions = append(status.Conditions, metav1.Condition{
		Type: conditionType, Status: conditionStatus, Reason: reason, Message: message, ObservedGeneration: generation,
	})
}

func requestedRevision(agent *kagentv1alpha3.Agent) string {
	raw, _ := json.Marshal(struct {
		UID        types.UID
		Generation int64
		Spec       kagentv1alpha3.AgentSpec
	}{agent.UID, agent.Generation, agent.Spec})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
