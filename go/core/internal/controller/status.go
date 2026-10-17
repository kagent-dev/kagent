package controller

import (
	"fmt"
	"strings"

	"istio.io/istio/pkg/kube/krt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
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
	if state.Target != nil {
		status.WorkloadImage = new(state.Target.Revision.Image)
	}
	setAgentCondition(&status, generation, kagentv1alpha3.AgentConditionAccepted, metav1.ConditionTrue, "Accepted", "Agent explicitly selects its template and harness")
	status.Conditions = append(status.Conditions, egressPoliciesCondition(kagentv1alpha3.AgentConditionEgressPoliciesApplied, generation, state.EgressIssues, state.EgressConflict))
	failure := state.CompilationFailure
	if failure == nil {
		failure = state.PreparationFailure
	}
	if failure != nil {
		if failure.Condition != kagentv1alpha3.AgentConditionResolvedRefs {
			setAgentCondition(&status, generation, kagentv1alpha3.AgentConditionResolvedRefs, metav1.ConditionTrue, "Resolved", "All runtime references resolved")
		}
		if failure.Condition == kagentv1alpha3.AgentConditionReady {
			setAgentCondition(&status, generation, kagentv1alpha3.AgentConditionCompatible, metav1.ConditionTrue, "Compatible", "Resolved configuration is compatible with the Harness")
		}
		setAgentFailure(&status, generation, failure)
		return status
	}
	setAgentCondition(&status, generation, kagentv1alpha3.AgentConditionResolvedRefs, metav1.ConditionTrue, "Resolved", "All runtime references resolved")
	setAgentCondition(&status, generation, kagentv1alpha3.AgentConditionCompatible, metav1.ConditionTrue, "Compatible", "Resolved configuration is compatible with the Harness")
	if state.ObservedActorTemplate.GetStatus().GetGoldenSnapshotStatus().GetGoldenTag() == nil {
		setAgentCondition(&status, generation, kagentv1alpha3.AgentConditionReady, metav1.ConditionFalse, "ActorTemplatePending", "waiting for the ActorTemplate golden snapshot")
		return status
	}
	status.LatestSuccessfulRevision = state.Target.RevisionID.String()
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

func egressPoliciesCondition(conditionType string, generation int64, issues []translator.EgressGrantIssue, conflict string) metav1.Condition {
	condition := metav1.Condition{
		Type: conditionType, ObservedGeneration: generation,
		Status: metav1.ConditionTrue, Reason: kagentv1alpha3.EgressPoliciesReasonApplied,
		Message: "All requested egress grants are admitted",
	}
	if len(issues) == 0 && conflict == "" {
		return condition
	}
	condition.Status = metav1.ConditionFalse
	var messages []string
	for _, issue := range issues {
		message := issue.Message
		if issue.Policy != "" {
			message = fmt.Sprintf("%s: %s", issue.Policy, message)
		}
		messages = append(messages, message)
	}
	if conflict != "" {
		condition.Reason = kagentv1alpha3.EgressPoliciesReasonConflict
		condition.Message = "new grants are held until the conflict is resolved; "
		messages = append(messages, "first conflict: "+conflict)
	} else {
		condition.Reason = issues[0].Reason
		condition.Message = ""
	}
	condition.Message += strings.Join(messages, "; ")
	// Stay comfortably below the Kubernetes condition message limit.
	const maxMessage = 4096
	if len(condition.Message) > maxMessage {
		condition.Message = strings.ToValidUTF8(condition.Message[:maxMessage-3], "") + "..."
	}
	return condition
}
