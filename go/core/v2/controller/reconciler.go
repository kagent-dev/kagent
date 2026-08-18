package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	ateclient "github.com/agent-substrate/substrate/pkg/client/clientset/versioned/typed/api/v1alpha1"
	kagentclient "github.com/kagent-dev/kagent/go/api/clientset/versioned/typed/api/v1alpha3"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/v2/substrate"
	v2translator "github.com/kagent-dev/kagent/go/core/v2/translator"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
)

// CollectionConfig contains the controller settings that affect compiled
// desired state. They are inputs to KRT rather than hidden global state.
type CollectionConfig struct {
	PauseImage string
}

// PairReconciliation is the complete desired and observed state for one
// AgentTemplate/Harness pair. Failure is data so invalid pairs still produce
// status instead of disappearing from the graph.
type PairReconciliation struct {
	Pair                  AgentTemplateHarnessPair
	Revision              *v2translator.Revision
	RevisionID            v2translator.RevisionID
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

func newPairReconciliations(
	pairs krt.Collection[AgentTemplateHarnessPair],
	modelConfigs krt.Collection[*kagentv1alpha3.ModelConfig],
	remoteMCPServers krt.Collection[*kagentv1alpha3.RemoteMCPServer],
	configMaps krt.Collection[*corev1.ConfigMap],
	secrets krt.Collection[*corev1.Secret],
	workerPools krt.Collection[*atev1alpha1.WorkerPool],
	actorTemplates krt.Collection[*atev1alpha1.ActorTemplate],
	config CollectionConfig,
	opts krt.OptionsBuilder,
) krt.Collection[PairReconciliation] {
	return krt.NewCollection(pairs, func(ctx krt.HandlerContext, pair AgentTemplateHarnessPair) *PairReconciliation {
		state := &PairReconciliation{Pair: pair}
		reader := collectionReader{
			ctx: ctx, modelConfigs: modelConfigs, remoteMCPServers: remoteMCPServers,
			configMaps: configMaps, secrets: secrets, workerPools: workerPools,
		}
		revision, err := v2translator.NewCompiler(reader).CompileAgentTemplate(context.Background(), pair.Harness, pair.AgentTemplate)
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

		observed := krt.FetchOne(ctx, actorTemplates, krt.FilterObjectName(types.NamespacedName{
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

// runtimeRevisionStore is the controller's narrow view of the shared database.
// Kubernetes owns ActorTemplates; the database retains revisions while a pair
// or, later, an AgentInstance or checkpoint references them.
type runtimeRevisionStore interface {
	UpsertAgentTemplateHarnessPair(context.Context, dbpkg.AgentTemplateHarnessPair) error
	UpsertRuntimeRevision(context.Context, dbpkg.RuntimeRevision) error
	MarkRuntimeRevisionSuccessful(context.Context, dbpkg.AgentTemplateHarnessPair) error
	RetireAgentTemplateHarnessPair(context.Context, string, string, string) error
	ListUnreferencedRuntimeRevisions(context.Context) ([]dbpkg.RuntimeRevision, error)
	DeleteUnreferencedRuntimeRevision(context.Context, string) error
}

// Reconciler is the side-effect boundary for the pure KRT graph. Collection
// handlers enqueue stable keys; retries always read the latest derived state.
type Reconciler struct {
	collections  Collections
	actors       ateclient.ApiV1alpha1Interface
	store        runtimeRevisionStore
	updateStatus func(context.Context, *kagentv1alpha3.AgentTemplate) error

	pairs         controllers.Queue
	statuses      controllers.Queue
	pairHandler   krt.HandlerRegistration
	statusHandler krt.HandlerRegistration
}

// NewReconciler creates the Kubernetes and database write boundary. Run starts
// its queues after the registered KRT handlers have received initial state.
func NewReconciler(config *rest.Config, collections Collections, store runtimeRevisionStore) (*Reconciler, error) {
	actors, err := ateclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create Substrate client: %w", err)
	}
	statusClient, err := kagentclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create kagent status client: %w", err)
	}
	return newReconciler(collections, actors, store, func(ctx context.Context, template *kagentv1alpha3.AgentTemplate) error {
		_, err := statusClient.AgentTemplates(template.Namespace).UpdateStatus(ctx, template, metav1.UpdateOptions{})
		return err
	}), nil
}

func newReconciler(
	collections Collections,
	actors ateclient.ApiV1alpha1Interface,
	store runtimeRevisionStore,
	updateStatus func(context.Context, *kagentv1alpha3.AgentTemplate) error,
) *Reconciler {
	r := &Reconciler{collections: collections, actors: actors, store: store, updateStatus: updateStatus}
	r.pairs = controllers.NewQueue("v2-agent-template-pairs", controllers.WithGenericReconciler(func(item any) error {
		return r.reconcilePair(context.Background(), item.(string))
	}), controllers.WithMaxAttempts(5))
	r.statuses = controllers.NewQueue("v2-agent-template-status", controllers.WithGenericReconciler(func(item any) error {
		return r.reconcileStatus(context.Background(), item.(string))
	}), controllers.WithMaxAttempts(5))

	r.pairHandler = collections.Reconciliations.Register(func(event krt.Event[PairReconciliation]) {
		r.pairs.Add(krt.GetKey(event.Latest()))
	})
	r.statusHandler = collections.AgentTemplateStatuses.Register(func(event krt.Event[krt.ObjectWithStatus[*kagentv1alpha3.AgentTemplate, kagentv1alpha3.AgentTemplateStatus]]) {
		status := event.Latest()
		if apiequality.Semantic.DeepEqual(statusWithTransitionTimes(status.Status, status.Obj.Status), status.Obj.Status) {
			return
		}
		r.statuses.Add(status.ResourceName())
	})
	return r
}

// Run waits for the graph boundary to observe initial state, then processes
// pair and status writes until stop closes.
func (r *Reconciler) Run(stop <-chan struct{}) {
	if !r.pairHandler.WaitUntilSynced(stop) || !r.statusHandler.WaitUntilSynced(stop) {
		r.pairs.ShutDownEarly()
		r.statuses.ShutDownEarly()
		return
	}
	go r.statuses.Run(stop)
	r.pairs.Run(stop)
}

func (r *Reconciler) Start(ctx context.Context) error {
	r.Run(ctx.Done())
	return nil
}

func (r *Reconciler) NeedLeaderElection() bool { return true }

func (r *Reconciler) reconcilePair(ctx context.Context, key string) error {
	state := r.collections.Reconciliations.GetKey(key)
	if state == nil {
		parts := strings.Split(key, "/")
		if len(parts) != 3 {
			return fmt.Errorf("invalid AgentTemplate/Harness pair key %q", key)
		}
		if err := r.store.RetireAgentTemplateHarnessPair(ctx, parts[0], parts[1], parts[2]); err != nil {
			return fmt.Errorf("retire AgentTemplate/Harness pair %s: %w", key, err)
		}
		return r.cleanupUnreferencedRevisions(ctx)
	}
	// Retire every historical identity at this stable name. The upsert below
	// immediately reactivates the exact current UID pair.
	if err := r.store.RetireAgentTemplateHarnessPair(ctx, state.Pair.AgentTemplate.Namespace, state.Pair.AgentTemplate.Name, state.Pair.Harness.Name); err != nil {
		return fmt.Errorf("retire replaced AgentTemplate/Harness pair %s: %w", key, err)
	}
	if state.Revision == nil || state.RevisionID.IsZero() {
		return r.cleanupUnreferencedRevisions(ctx)
	}

	pair := dbpkg.AgentTemplateHarnessPair{
		Namespace: state.Pair.AgentTemplate.Namespace, AgentTemplateName: state.Pair.AgentTemplate.Name,
		AgentTemplateUID: string(state.Pair.AgentTemplate.UID), HarnessName: state.Pair.Harness.Name,
		HarnessUID: string(state.Pair.Harness.UID), DesiredRevision: state.RevisionID.String(),
		AgentTemplateLabels: state.Pair.AgentTemplate.Labels,
	}
	// Store the desired edge before creating compute so a concurrent collector
	// cannot mistake the revision for abandoned state.
	if err := r.store.UpsertAgentTemplateHarnessPair(ctx, pair); err != nil {
		return fmt.Errorf("store AgentTemplate/Harness pair %s: %w", key, err)
	}
	if state.Failure != nil {
		return r.cleanupUnreferencedRevisions(ctx)
	}
	if state.ObservedActorTemplate == nil {
		_, err := r.actors.ActorTemplates(state.DesiredActorTemplate.Namespace).Create(ctx, state.DesiredActorTemplate.DeepCopy(), metav1.CreateOptions{})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create ActorTemplate %s/%s: %w", state.DesiredActorTemplate.Namespace, state.DesiredActorTemplate.Name, err)
		}
		return nil
	}

	observed := state.ObservedActorTemplate
	revision := dbpkg.RuntimeRevision{
		Revision: state.RevisionID.String(), Namespace: pair.Namespace, AgentTemplateName: pair.AgentTemplateName,
		AgentTemplateUID: pair.AgentTemplateUID, HarnessName: pair.HarnessName, HarnessUID: pair.HarnessUID,
		SourceSnapshot: state.Revision.Provenance, AgentCard: state.Revision.AgentCardJSON,
		EgressDestinations:     state.Revision.EgressDestinations,
		ActorTemplateNamespace: observed.Namespace, ActorTemplateName: observed.Name, ActorTemplateUID: string(observed.UID),
		Phase: string(observed.Status.Phase), GoldenSnapshot: observed.Status.GoldenSnapshot,
	}
	if err := r.store.UpsertRuntimeRevision(ctx, revision); err != nil {
		return fmt.Errorf("store runtime revision %s: %w", state.RevisionID, err)
	}
	if observed.Status.Phase == atev1alpha1.PhaseReady {
		if err := r.store.MarkRuntimeRevisionSuccessful(ctx, pair); err != nil {
			return fmt.Errorf("mark runtime revision %s successful: %w", state.RevisionID, err)
		}
		return r.cleanupUnreferencedRevisions(ctx)
	}
	return nil
}

func (r *Reconciler) reconcileStatus(ctx context.Context, key string) error {
	desired := r.collections.AgentTemplateStatuses.GetKey(key)
	template := r.collections.AgentTemplates.GetKey(key)
	if desired == nil || template == nil {
		return nil
	}
	updated := (*template).DeepCopy()
	updated.Status = statusWithTransitionTimes(desired.Status, updated.Status)
	if apiequality.Semantic.DeepEqual(updated.Status, (*template).Status) {
		return nil
	}
	if err := r.updateStatus(ctx, updated); err != nil {
		return fmt.Errorf("update AgentTemplate %s status: %w", key, err)
	}
	return nil
}

// cleanupUnreferencedRevisions removes immutable ActorTemplates after their
// final pair or AgentInstance database reference has been released.
func (r *Reconciler) cleanupUnreferencedRevisions(ctx context.Context) error {
	revisions, err := r.store.ListUnreferencedRuntimeRevisions(ctx)
	if err != nil {
		return fmt.Errorf("list unreferenced runtime revisions: %w", err)
	}
	for _, revision := range revisions {
		client := r.actors.ActorTemplates(revision.ActorTemplateNamespace)
		template, err := client.Get(ctx, revision.ActorTemplateName, metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("get unreferenced ActorTemplate %s/%s: %w", revision.ActorTemplateNamespace, revision.ActorTemplateName, err)
		}
		if err == nil {
			if revision.ActorTemplateUID == "" || string(template.UID) != revision.ActorTemplateUID {
				return fmt.Errorf("unreferenced ActorTemplate %s/%s UID changed", revision.ActorTemplateNamespace, revision.ActorTemplateName)
			}
			uid := types.UID(revision.ActorTemplateUID)
			if err := client.Delete(ctx, revision.ActorTemplateName, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("delete unreferenced ActorTemplate %s/%s: %w", revision.ActorTemplateNamespace, revision.ActorTemplateName, err)
			}
		}
		if err := r.store.DeleteUnreferencedRuntimeRevision(ctx, revision.Revision); err != nil {
			return fmt.Errorf("delete unreferenced runtime revision %s: %w", revision.Revision, err)
		}
	}
	return nil
}

func statusWithTransitionTimes(desired, current kagentv1alpha3.AgentTemplateStatus) kagentv1alpha3.AgentTemplateStatus {
	desired.Harnesses = append([]kagentv1alpha3.AgentTemplateHarnessStatus(nil), desired.Harnesses...)
	for harnessIndex := range desired.Harnesses {
		desiredHarness := &desired.Harnesses[harnessIndex]
		desiredHarness.Conditions = append([]metav1.Condition(nil), desiredHarness.Conditions...)
		var currentHarness *kagentv1alpha3.AgentTemplateHarnessStatus
		for index := range current.Harnesses {
			if current.Harnesses[index].Harness == desiredHarness.Harness {
				currentHarness = &current.Harnesses[index]
				break
			}
		}
		for conditionIndex := range desiredHarness.Conditions {
			condition := &desiredHarness.Conditions[conditionIndex]
			if currentHarness != nil {
				if previous := apimeta.FindStatusCondition(currentHarness.Conditions, condition.Type); previous != nil &&
					previous.Status == condition.Status && previous.Reason == condition.Reason &&
					previous.Message == condition.Message && previous.ObservedGeneration == condition.ObservedGeneration {
					condition.LastTransitionTime = previous.LastTransitionTime
					continue
				}
			}
			condition.LastTransitionTime = metav1.Now()
		}
	}
	return desired
}
