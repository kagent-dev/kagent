package controller

import (
	"fmt"
	"slices"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
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
	if retry := state.GoldenBootRetry; retry != nil {
		setAgentCondition(&status, generation, kagentv1alpha3.AgentConditionReady, metav1.ConditionFalse, "ActorTemplateRetrying", goldenBootFailedMessage(retry.Attempt, retry.Message, "starting it over"))
		return status
	}
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

// harnessKey is the Harness an Agent runs on, or "" for an Agent that
// declares its harness inline.
func harnessKey(agent *kagentv1alpha3.Agent) string {
	if agent.Spec.HarnessRef == nil {
		return ""
	}
	return agent.Namespace + "/" + agent.Spec.HarnessRef.Name
}

func newHarnessStatuses(
	harnesses krt.Collection[*kagentv1alpha3.Harness],
	workerPools krt.Collection[*atev1alpha1.WorkerPool],
	states krt.Collection[AgentReconciliation],
	opts krt.OptionsBuilder,
) krt.StatusCollection[*kagentv1alpha3.Harness, kagentv1alpha3.HarnessStatus] {
	byHarness := krt.NewIndex(states, "harness", func(state AgentReconciliation) []string {
		if key := harnessKey(state.Agent); key != "" {
			return []string{key}
		}
		return nil
	})
	statuses, _ := krt.NewStatusManyCollection(harnesses, func(ctx krt.HandlerContext, harness *kagentv1alpha3.Harness) (*kagentv1alpha3.HarnessStatus, []AgentReconciliation) {
		pool := types.NamespacedName{Namespace: harness.Namespace, Name: harness.Spec.Substrate.WorkerPoolRef.Name}
		poolFound := krt.FetchOne(ctx, workerPools, krt.FilterObjectName(pool)) != nil
		agents := krt.Fetch(ctx, states, krt.FilterIndex(byHarness, harness.Namespace+"/"+harness.Name))
		return &kagentv1alpha3.HarnessStatus{
			ObservedGeneration: harness.Generation,
			Conditions:         []metav1.Condition{harnessReadyCondition(harness.Generation, pool, poolFound, agents)},
		}, nil
	}, opts.WithName("HarnessStatuses")...)
	return statuses
}

// harnessReadyCondition reads a Harness's readiness from its WorkerPool and
// the golden boots of the Agents that run on it. Every Agent revision boots
// the Harness's workload, so one successful golden boot proves the Harness
// boots; a failed boot fails it only while no Agent booted, since a single
// Agent's own configuration can fail its boot too. Without a booted or failed
// Agent nothing has tried the workload yet and readiness is Unknown.
func harnessReadyCondition(generation int64, pool types.NamespacedName, poolFound bool, agents []AgentReconciliation) metav1.Condition {
	condition := func(status metav1.ConditionStatus, reason, message string) metav1.Condition {
		return metav1.Condition{
			Type: kagentv1alpha3.HarnessConditionTypeReady, Status: status, Reason: reason, Message: message, ObservedGeneration: generation,
		}
	}
	if !poolFound {
		return condition(metav1.ConditionFalse, "WorkerPoolNotFound", fmt.Sprintf("WorkerPool %q not found", pool.String()))
	}
	slices.SortFunc(agents, func(a, b AgentReconciliation) int { return strings.Compare(a.ResourceName(), b.ResourceName()) })
	var failed, retrying, pending *metav1.Condition
	for _, state := range agents {
		if state.Target == nil || state.CompilationFailure != nil {
			continue
		}
		agent := state.ResourceName()
		switch failure := state.PreparationFailure; {
		case failure != nil && failure.Condition == kagentv1alpha3.AgentConditionReady:
			if failed == nil {
				c := condition(metav1.ConditionFalse, failure.Reason, fmt.Sprintf("Agent %s: %s", agent, failure.Message))
				failed = &c
			}
		case failure != nil:
		case state.GoldenBootRetry != nil:
			if retrying == nil {
				retry := state.GoldenBootRetry
				c := condition(metav1.ConditionFalse, "ActorTemplateRetrying", fmt.Sprintf("Agent %s: %s", agent, goldenBootFailedMessage(retry.Attempt, retry.Message, "starting it over")))
				retrying = &c
			}
		case state.ObservedActorTemplate.GetStatus().GetGoldenSnapshotStatus().GetGoldenTag() != nil:
			return condition(metav1.ConditionTrue, "Booted", fmt.Sprintf("golden boot of Agent %s succeeded", agent))
		default:
			if pending == nil {
				c := condition(metav1.ConditionUnknown, "BootPending", fmt.Sprintf("waiting for the golden boot of Agent %s", agent))
				pending = &c
			}
		}
	}
	for _, c := range []*metav1.Condition{failed, retrying, pending} {
		if c != nil {
			return *c
		}
	}
	return condition(metav1.ConditionUnknown, "NoAgents", "no Agent runs on this Harness yet")
}
