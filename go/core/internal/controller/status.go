package controller

import (
	"fmt"
	"slices"
	"strings"

	atev1alpha1 "go.ate.dev/substrate/pkg/api/v1alpha1"
	"istio.io/istio/pkg/kube/krt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
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

func newHarnessStatuses(
	harnesses krt.Collection[*kagentv1alpha3.Harness],
	workerPools krt.Collection[*atev1alpha1.WorkerPool],
	states krt.Collection[AgentReconciliation],
	images *configv1.HarnessImagesConfig,
	opts krt.OptionsBuilder,
) krt.StatusCollection[*kagentv1alpha3.Harness, kagentv1alpha3.HarnessStatus] {
	byHarness := krt.NewIndex(states, "harness", func(state AgentReconciliation) []string {
		return []string{state.Agent.Namespace + "/" + state.Agent.Spec.HarnessRef.Name}
	})
	statuses, _ := krt.NewStatusManyCollection(harnesses, func(ctx krt.HandlerContext, harness *kagentv1alpha3.Harness) (*kagentv1alpha3.HarnessStatus, []AgentReconciliation) {
		pool := types.NamespacedName{Namespace: harness.Namespace, Name: harness.Spec.Substrate.WorkerPoolRef.Name}
		poolFound := krt.FetchOne(ctx, workerPools, krt.FilterObjectName(pool)) != nil
		_, imageErr := translator.HarnessImage(harness, images)
		agents := krt.Fetch(ctx, states, krt.FilterIndex(byHarness, harness.Namespace+"/"+harness.Name))
		return &kagentv1alpha3.HarnessStatus{
			ObservedGeneration: harness.Generation,
			Conditions: []metav1.Condition{
				harnessReadyCondition(harness.Generation, pool, poolFound, imageErr),
				harnessBootedCondition(harness.Generation, agents),
			},
		}, nil
	}, opts.WithName("HarnessStatuses")...)
	return statuses
}

// harnessReadyCondition states the Harness's own invariants: it is Ready once
// the WorkerPool it references exists and its workload image resolves. What
// the Agents on it observed is the Booted condition's, so one Agent's broken
// configuration never flips a Harness that other Agents share.
func harnessReadyCondition(generation int64, pool types.NamespacedName, poolFound bool, imageErr error) metav1.Condition {
	if !poolFound {
		return harnessCondition(kagentv1alpha3.HarnessConditionTypeReady, generation, metav1.ConditionFalse, "WorkerPoolNotFound", (&translator.WorkerPoolNotFoundError{WorkerPool: pool}).Error())
	}
	if imageErr != nil {
		return harnessCondition(kagentv1alpha3.HarnessConditionTypeReady, generation, metav1.ConditionFalse, "UnsupportedConfiguration", imageErr.Error())
	}
	return harnessCondition(kagentv1alpha3.HarnessConditionTypeReady, generation, metav1.ConditionTrue, "Resolved", fmt.Sprintf("WorkerPool %q and the workload image resolve", pool.String()))
}

// harnessBootedCondition reads the golden boots of the Agents that run on the
// Harness. Every Agent revision boots the Harness's workload, so one
// successful boot proves it boots; a failed boot counts only while no Agent
// booted, since an Agent's own configuration can fail its boot too. Any other
// failure (an unresolved reference, a rejected ActorTemplate, a backend error)
// never reached a boot and says nothing about the Harness.
func harnessBootedCondition(generation int64, agents []AgentReconciliation) metav1.Condition {
	slices.SortFunc(agents, func(a, b AgentReconciliation) int { return strings.Compare(a.ResourceName(), b.ResourceName()) })
	var failed *metav1.Condition
	var pending string
	for _, state := range agents {
		if state.Target == nil || state.CompilationFailure != nil {
			continue
		}
		agent := state.ResourceName()
		switch failure := state.PreparationFailure; {
		case failure == nil && state.ObservedActorTemplate.GetStatus().GetGoldenSnapshotStatus().GetGoldenTag() != nil:
			return harnessCondition(kagentv1alpha3.HarnessConditionTypeBooted, generation, metav1.ConditionTrue, "Booted", fmt.Sprintf("golden boot of Agent %s succeeded", agent))
		case failure == nil:
			if pending == "" {
				pending = agent
			}
		case failure.Reason == reasonActorTemplateFailed:
			if failed == nil {
				c := harnessCondition(kagentv1alpha3.HarnessConditionTypeBooted, generation, metav1.ConditionFalse, failure.Reason, fmt.Sprintf("Agent %s: %s", agent, failure.Message))
				failed = &c
			}
		}
	}
	if failed != nil {
		return *failed
	}
	if pending != "" {
		return harnessCondition(kagentv1alpha3.HarnessConditionTypeBooted, generation, metav1.ConditionUnknown, "BootPending", fmt.Sprintf("waiting for the golden boot of Agent %s", pending))
	}
	return harnessCondition(kagentv1alpha3.HarnessConditionTypeBooted, generation, metav1.ConditionUnknown, "NoAgents", "no Agent runs on this Harness yet")
}

func harnessCondition(conditionType string, generation int64, status metav1.ConditionStatus, reason, message string) metav1.Condition {
	return metav1.Condition{Type: conditionType, Status: status, Reason: reason, Message: message, ObservedGeneration: generation}
}
