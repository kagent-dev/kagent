package controller

import (
	"context"
	"errors"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/v2/substrate"
	v2translator "github.com/kagent-dev/kagent/go/core/v2/translator"
	"istio.io/istio/pkg/kube/krt"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/types"
)

// CollectionConfig contains the controller settings that affect compiled
// desired state. They are inputs to KRT rather than hidden global state.
type CollectionConfig struct {
	PauseImage         string
	MCPEgressPlaintext bool
}

// PairReconciliation is the complete desired and observed state for one
// AgentTemplate/Harness pair. Failure is data so invalid pairs still produce
// status instead of disappearing from the graph.
type PairReconciliation struct {
	Pair                  AgentTemplateHarnessPair
	Revision              *v2translator.Revision
	RevisionID            string
	DesiredActorTemplate  *atev1alpha1.ActorTemplate
	ObservedActorTemplate *atev1alpha1.ActorTemplate
	Failure               *ReconciliationFailure
}

func (r PairReconciliation) ResourceName() string { return r.Pair.ResourceName() }

// ReconciliationFailure identifies the condition stage blocked by a pair.
type ReconciliationFailure struct {
	Condition string
	Reason    string
	Message   string
}

func newReconciliationCollections(collections *Collections, config CollectionConfig, opts krt.OptionsBuilder) {
	collections.Reconciliations = newPairReconciliations(collections, config, opts)
	collections.AgentTemplateStatuses = newAgentTemplateStatuses(collections.AgentTemplates, collections.Reconciliations, opts)
}

func newPairReconciliations(collections *Collections, config CollectionConfig, opts krt.OptionsBuilder) krt.Collection[PairReconciliation] {
	return krt.NewCollection(collections.Pairs, func(ctx krt.HandlerContext, pair AgentTemplateHarnessPair) *PairReconciliation {
		state := &PairReconciliation{Pair: pair}
		reader := collectionReader{ctx: ctx, collections: collections}
		revision, err := v2translator.NewCompiler(reader, config.MCPEgressPlaintext).CompileAgentTemplate(context.Background(), pair.Harness, pair.AgentTemplate)
		if err != nil {
			condition, reason := kagentv1alpha3.AgentTemplateConditionResolvedRefs, "ReferenceResolutionFailed"
			var validation *v2translator.ValidationError
			if errors.As(err, &validation) {
				condition, reason = kagentv1alpha3.AgentTemplateConditionCompatible, "UnsupportedConfiguration"
			}
			state.Failure = &ReconciliationFailure{Condition: condition, Reason: reason, Message: err.Error()}
			return state
		}
		state.Revision = revision
		state.RevisionID, err = revision.Digest()
		if err != nil {
			state.Failure = &ReconciliationFailure{Condition: kagentv1alpha3.AgentTemplateConditionCompatible, Reason: "RevisionInvalid", Message: err.Error()}
			return state
		}

		workerPool := &atev1alpha1.WorkerPool{}
		workerKey := types.NamespacedName{Namespace: revision.Namespace, Name: revision.WorkerPoolName}
		if err := reader.Get(context.Background(), workerKey, workerPool); err != nil {
			state.Failure = &ReconciliationFailure{Condition: kagentv1alpha3.AgentTemplateConditionResolvedRefs, Reason: "WorkerPoolNotFound", Message: err.Error()}
			return state
		}
		state.DesiredActorTemplate, err = substrate.ActorTemplateForRevision(revision, state.RevisionID, config.PauseImage)
		if err != nil {
			state.Failure = &ReconciliationFailure{Condition: kagentv1alpha3.AgentTemplateConditionCompatible, Reason: "ActorTemplateInvalid", Message: err.Error()}
			return state
		}

		observed := krt.FetchOne(ctx, collections.ActorTemplates, krt.FilterObjectName(types.NamespacedName{
			Namespace: state.DesiredActorTemplate.Namespace,
			Name:      state.DesiredActorTemplate.Name,
		}))
		if observed == nil {
			return state
		}
		state.ObservedActorTemplate = (*observed).DeepCopy()
		if !apiequality.Semantic.DeepEqual(state.ObservedActorTemplate.Spec, state.DesiredActorTemplate.Spec) {
			state.Failure = &ReconciliationFailure{
				Condition: kagentv1alpha3.AgentTemplateConditionReady,
				Reason:    "ActorTemplateConflict",
				Message:   "existing immutable ActorTemplate differs from the compiled revision",
			}
		}
		return state
	}, opts.WithName("PairReconciliations")...)
}
