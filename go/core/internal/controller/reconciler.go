package controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	kagentclient "github.com/kagent-dev/kagent/go/api/clientset/versioned/typed/api/v1alpha3"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	byotranslator "github.com/kagent-dev/kagent/go/core/internal/translator/byo"
	claudetranslator "github.com/kagent-dev/kagent/go/core/internal/translator/claude"
	codextranslator "github.com/kagent-dev/kagent/go/core/internal/translator/codex"
	kagenttranslator "github.com/kagent-dev/kagent/go/core/internal/translator/kagent"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/workqueue"
)

// PairReconciliation is the complete desired and observed state for one
// AgentTemplate/Harness pair. Failure is data so invalid pairs still produce
// status instead of disappearing from the graph.
type PairReconciliation struct {
	Pair                  AgentTemplateHarnessPair
	Revision              *v2translator.Revision
	Warnings              []string
	RevisionID            v2translator.RevisionID
	DesiredActorTemplate  *ateapipb.ActorTemplate
	ObservedActorTemplate *ateapipb.ActorTemplate
	Failure               *ReconciliationFailure
}

func (r PairReconciliation) ResourceName() string { return r.Pair.ResourceName() }

var _ krt.Equaler[PairReconciliation] = PairReconciliation{}

// Equals keeps KRT from reflecting over protobuf caches that mutate during reads.
func (r PairReconciliation) Equals(other PairReconciliation) bool {
	if (r.Revision == nil) != (other.Revision == nil) ||
		(r.Revision != nil && !r.Revision.Equals(*other.Revision)) ||
		!proto.Equal(r.DesiredActorTemplate, other.DesiredActorTemplate) ||
		!proto.Equal(r.ObservedActorTemplate, other.ObservedActorTemplate) {
		return false
	}
	r.Revision, other.Revision = nil, nil
	r.DesiredActorTemplate, other.DesiredActorTemplate = nil, nil
	r.ObservedActorTemplate, other.ObservedActorTemplate = nil, nil
	return reflect.DeepEqual(r, other)
}

func (r PairReconciliation) desiredRevision() string {
	if r.Revision == nil || r.RevisionID.IsZero() {
		return requestedRevision(r.Pair.AgentTemplate, r.Pair.Harness.Name)
	}
	return r.RevisionID.String()
}

func (r PairReconciliation) canPrepare() bool {
	return r.Revision != nil && !r.RevisionID.IsZero() && r.DesiredActorTemplate != nil &&
		(r.Failure == nil || r.Failure.Retryable)
}

// ReconciliationFailure identifies the condition stage blocked by a pair.
type ReconciliationFailure struct {
	Condition string
	Reason    string
	Message   string
	Retryable bool
}

func newPairReconciliations(
	pairs krt.Collection[AgentTemplateHarnessPair],
	collections v2translator.Collections,
	pairRuntimeObservations krt.Collection[PairRuntimeObservation],
	opts krt.OptionsBuilder,
) krt.Collection[PairReconciliation] {
	return krt.NewCollection(pairs, func(ctx krt.HandlerContext, pair AgentTemplateHarnessPair) *PairReconciliation {
		state := &PairReconciliation{Pair: pair}
		compilation, err := v2translator.NewCompiler(ctx, collections, map[v2translator.HarnessType]v2translator.HarnessCompiler{
			v2translator.HarnessTypeKagent: kagenttranslator.NewCompiler(ctx, collections),
			v2translator.HarnessTypeCodex:  codextranslator.NewCompiler(ctx, collections),
			v2translator.HarnessTypeClaude: claudetranslator.NewCompiler(ctx, collections),
			v2translator.HarnessTypeBYO:    byotranslator.NewCompiler(ctx, collections),
		}).CompileAgentTemplate(context.Background(), pair.Harness, pair.AgentTemplate)
		if err != nil {
			condition, reason := kagentv1alpha3.AgentTemplateConditionResolvedRefs, "ReferenceResolutionFailed"
			var validation *v2translator.ValidationError
			var missingPool *v2translator.WorkerPoolNotFoundError
			switch {
			case errors.As(err, &validation):
				condition, reason = kagentv1alpha3.AgentTemplateConditionCompatible, "UnsupportedConfiguration"
			case errors.As(err, &missingPool):
				reason = "WorkerPoolNotFound"
			}
			state.Failure = &ReconciliationFailure{Condition: condition, Reason: reason, Message: err.Error()}
			return state
		}
		revision := &compilation.Revision
		state.Revision = revision
		state.Warnings = append([]string(nil), compilation.Warnings...)
		state.RevisionID, err = revision.Digest()
		if err != nil {
			state.Failure = &ReconciliationFailure{Condition: kagentv1alpha3.AgentTemplateConditionCompatible, Reason: "RevisionInvalid", Message: err.Error()}
			return state
		}

		state.DesiredActorTemplate, err = substrate.ActorTemplateForRevision(revision, state.RevisionID)
		if err != nil {
			state.Failure = &ReconciliationFailure{Condition: kagentv1alpha3.AgentTemplateConditionCompatible, Reason: "ActorTemplateInvalid", Message: err.Error()}
			return state
		}

		observed := krt.FetchOne(ctx, pairRuntimeObservations, krt.FilterKey(pair.ResourceName()))
		if observed == nil || observed.RevisionID != state.RevisionID {
			return state
		}
		if observed.Failure != nil {
			state.Failure = observed.Failure
			return state
		}
		state.ObservedActorTemplate = (*observed).Template
		if !substrate.ActorTemplateSpecEqual(state.ObservedActorTemplate, state.DesiredActorTemplate) {
			state.Failure = &ReconciliationFailure{
				Condition: kagentv1alpha3.AgentTemplateConditionReady,
				Reason:    "ActorTemplateConflict",
				Message:   "existing immutable ActorTemplate differs from the compiled revision",
			}
		} else if message := state.ObservedActorTemplate.GetStatus().GetGoldenSnapshotStatus().GetErrorMessage(); message != "" {
			state.Failure = &ReconciliationFailure{Condition: kagentv1alpha3.AgentTemplateConditionReady, Reason: "ActorTemplateFailed", Message: message}
		}
		return state
	}, opts.WithName("PairReconciliations")...)
}

// runtimeRevisionStore is the controller's narrow view of the shared database.
// Substrate owns ActorTemplates; the database retains revisions while a pair
// or an AgentInstance or checkpoint references them.
type runtimeRevisionStore interface {
	UpsertAgentTemplateHarnessPair(context.Context, database.AgentTemplateHarnessPair) error
	RecordRuntimeRevision(context.Context, database.RuntimeRevision, bool) error
	RetirePairIdentities(ctx context.Context, namespace, templateName, harnessName string, except *database.AgentTemplateHarnessPair) error
}

type actorTemplateClient interface {
	EnsureAtespace(context.Context, string) error
	GetActorTemplate(context.Context, string, string) (*ateapipb.ActorTemplate, error)
	CreateActorTemplate(context.Context, *ateapipb.ActorTemplate) (*ateapipb.ActorTemplate, error)
}

// Reconciler is the side-effect boundary for the pure KRT graph. Collection
// handlers enqueue stable keys; retries always read the latest derived state.
type Reconciler struct {
	collections Collections
	templates   actorTemplateClient
	store       runtimeRevisionStore
	status      kagentclient.ApiV1alpha3Interface

	pairs                      controllers.Queue
	agentTemplateStatuses      controllers.Queue
	modelConfigStatuses        controllers.Queue
	harnessStatuses            controllers.Queue
	pairHandler                krt.HandlerRegistration
	agentTemplateStatusHandler krt.HandlerRegistration
	modelConfigStatusHandler   krt.HandlerRegistration
	harnessStatusHandler       krt.HandlerRegistration
}

// NewReconciler creates the Kubernetes and database write boundary. Run starts
// its queues after the registered KRT handlers have received initial state.
func NewReconciler(config *rest.Config, collections Collections, store runtimeRevisionStore, templates actorTemplateClient) (*Reconciler, error) {
	statusClient, err := kagentclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create kagent status client: %w", err)
	}
	return newReconciler(collections, templates, store, statusClient), nil
}

func newReconciler(
	collections Collections,
	templates actorTemplateClient,
	store runtimeRevisionStore,
	status kagentclient.ApiV1alpha3Interface,
) *Reconciler {
	r := &Reconciler{
		collections: collections,
		templates:   templates,
		store:       store,
		status:      status,
	}
	r.pairs = newReconciliationQueue("v2-agent-template-pairs", func(item any) error {
		return r.reconcilePair(context.Background(), item.(string))
	})
	r.agentTemplateStatuses = newReconciliationQueue("v2-agent-template-status", func(item any) error {
		return r.reconcileAgentTemplateStatus(context.Background(), item.(string))
	})
	r.modelConfigStatuses = newReconciliationQueue("v2-model-config-status", func(item any) error {
		return r.reconcileModelConfigStatus(context.Background(), item.(string))
	})
	r.harnessStatuses = newReconciliationQueue("v2-harness-status", func(item any) error {
		return r.reconcileHarnessStatus(context.Background(), item.(string))
	})

	r.pairHandler = collections.Reconciliations.Register(func(event krt.Event[PairReconciliation]) {
		r.pairs.Add(krt.GetKey(event.Latest()))
	})
	r.agentTemplateStatusHandler = collections.AgentTemplateStatuses.Register(func(event krt.Event[krt.ObjectWithStatus[*kagentv1alpha3.AgentTemplate, kagentv1alpha3.AgentTemplateStatus]]) {
		status := event.Latest()
		if apiequality.Semantic.DeepEqual(statusWithTransitionTimes(status.Status, status.Obj.Status), status.Obj.Status) {
			return
		}
		r.agentTemplateStatuses.Add(status.ResourceName())
	})
	r.modelConfigStatusHandler = collections.ModelConfigStatuses.Register(func(event krt.Event[krt.ObjectWithStatus[*kagentv1alpha3.ModelConfig, kagentv1alpha3.ModelConfigStatus]]) {
		status := event.Latest()
		if apiequality.Semantic.DeepEqual(modelConfigStatusWithTransitionTimes(status.Status, status.Obj.Status), status.Obj.Status) {
			return
		}
		r.modelConfigStatuses.Add(status.ResourceName())
	})
	r.harnessStatusHandler = collections.HarnessStatuses.Register(func(event krt.Event[krt.ObjectWithStatus[*kagentv1alpha3.Harness, kagentv1alpha3.HarnessStatus]]) {
		status := event.Latest()
		if apiequality.Semantic.DeepEqual(harnessStatusWithTransitionTimes(status.Status, status.Obj.Status), status.Obj.Status) {
			return
		}
		r.harnessStatuses.Add(status.ResourceName())
	})
	return r
}

// Ten attempts span about 2.5 minutes. Each queue owns its backoff state;
// fresh graph events can enqueue work again after an error budget is exhausted.
func newReconciliationQueue(name string, reconcile func(any) error) controllers.Queue {
	return controllers.NewQueue(name, controllers.WithGenericReconciler(reconcile),
		controllers.WithMaxAttempts(10),
		controllers.WithRateLimiter(workqueue.NewTypedItemExponentialFailureRateLimiter[any](time.Second, 30*time.Second)),
	)
}

// Run waits for the graph boundary to observe initial state, then processes
// pair and status writes until stop closes.
func (r *Reconciler) Run(stop <-chan struct{}) {
	if !r.pairHandler.WaitUntilSynced(stop) || !r.agentTemplateStatusHandler.WaitUntilSynced(stop) || !r.modelConfigStatusHandler.WaitUntilSynced(stop) || !r.harnessStatusHandler.WaitUntilSynced(stop) {
		r.pairs.ShutDownEarly()
		r.agentTemplateStatuses.ShutDownEarly()
		r.modelConfigStatuses.ShutDownEarly()
		r.harnessStatuses.ShutDownEarly()
		return
	}
	go r.pollPendingTemplates(stop)
	go r.agentTemplateStatuses.Run(stop)
	go r.modelConfigStatuses.Run(stop)
	go r.harnessStatuses.Run(stop)
	r.pairs.Run(stop)
}

func (r *Reconciler) pollPendingTemplates(stop <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			for _, state := range r.collections.Reconciliations.List() {
				golden := state.ObservedActorTemplate.GetStatus().GetGoldenSnapshotStatus()
				if state.canPrepare() && golden.GetGoldenTag() == nil {
					r.pairs.Add(state.ResourceName())
				}
			}
		}
	}
}

func (r *Reconciler) Start(ctx context.Context) error {
	r.Run(ctx.Done())
	return nil
}

func (r *Reconciler) NeedLeaderElection() bool { return true }

func (r *Reconciler) reconcilePair(ctx context.Context, key string) error {
	state := r.collections.Reconciliations.GetKey(key)
	if observation := r.collections.PairRuntimeObservations.GetKey(key); observation != nil &&
		(state == nil || state.Revision == nil || observation.RevisionID != state.RevisionID) {
		r.collections.PairRuntimeObservations.DeleteObject(key)
	}
	if state == nil {
		parts := strings.Split(key, "/")
		if len(parts) != 3 {
			return fmt.Errorf("invalid AgentTemplate/Harness pair key %q", key)
		}
		if err := r.store.RetirePairIdentities(ctx, parts[0], parts[1], parts[2], nil); err != nil {
			return fmt.Errorf("retire AgentTemplate/Harness pair %s: %w", key, err)
		}
		return nil
	}
	pair := database.AgentTemplateHarnessPair{
		Namespace: state.Pair.AgentTemplate.Namespace, AgentTemplateName: state.Pair.AgentTemplate.Name,
		AgentTemplateUID: string(state.Pair.AgentTemplate.UID), HarnessName: state.Pair.Harness.Name,
		HarnessUID: string(state.Pair.Harness.UID), DesiredRevision: state.desiredRevision(),
	}
	// Store the desired edge before creating compute so a concurrent collector
	// cannot mistake the revision for abandoned state. Unresolved inputs replace
	// the old desired edge with their requested identity, not a runtime revision;
	// the upsert preserves the current UID's last-good runtime in either case.
	if err := r.store.UpsertAgentTemplateHarnessPair(ctx, pair); err != nil {
		if errors.Is(err, database.ErrObjectDeleting) {
			// A desired digest may be awaiting cleanup from an earlier identity.
			// Clearing the observation makes KRT derive a pending pair, which
			// the pending-template poll retries until GC finishes.
			r.collections.PairRuntimeObservations.DeleteObject(key)
			return nil
		}
		return r.observePreparationError(*state, fmt.Errorf("store AgentTemplate/Harness pair %s: %w", key, err))
	}
	if !state.canPrepare() {
		return nil
	}
	desiredRef := state.DesiredActorTemplate.GetMetadata()
	observed, err := r.templates.GetActorTemplate(ctx, desiredRef.GetAtespace(), desiredRef.GetName())
	if status.Code(err) == codes.NotFound {
		if err := r.templates.EnsureAtespace(ctx, desiredRef.GetAtespace()); err != nil {
			return r.observePreparationError(*state, fmt.Errorf("ensure Atespace %s: %w", desiredRef.GetAtespace(), err))
		}
		observed, err = r.templates.CreateActorTemplate(ctx, state.DesiredActorTemplate)
		if status.Code(err) == codes.AlreadyExists {
			observed, err = r.templates.GetActorTemplate(ctx, desiredRef.GetAtespace(), desiredRef.GetName())
		}
	}
	if err != nil {
		return r.observePreparationError(*state, fmt.Errorf("reconcile ActorTemplate %s/%s: %w", desiredRef.GetAtespace(), desiredRef.GetName(), err))
	}
	if !substrate.ActorTemplateSpecEqual(observed, state.DesiredActorTemplate) {
		r.observePreparation(*state, observed, nil)
		return nil
	}

	revision := database.RuntimeRevision{
		Revision: state.RevisionID.String(), Namespace: pair.Namespace, AgentTemplateName: pair.AgentTemplateName,
		AgentTemplateUID: pair.AgentTemplateUID, HarnessName: pair.HarnessName, HarnessUID: pair.HarnessUID,
		SourceSnapshot: state.Revision.Provenance, AgentCard: state.Revision.AgentCard,
		EgressDestinations:    state.Revision.EgressDestinations,
		Credentials:           state.Revision.Credentials,
		ActorTemplateAtespace: observed.GetMetadata().GetAtespace(), ActorTemplateName: observed.GetMetadata().GetName(), ActorTemplateUID: observed.GetMetadata().GetUid(),
	}
	ready := observed.GetStatus().GetGoldenSnapshotStatus().GetGoldenTag() != nil
	if err := r.store.RecordRuntimeRevision(ctx, revision, ready); err != nil {
		return r.observePreparationError(*state, fmt.Errorf("store runtime revision %s: %w", state.RevisionID, err))
	}
	// This observation drives Kubernetes Ready status on a separate queue.
	// Publish it only after instance creation can select the persisted revision.
	r.observePreparation(*state, observed, nil)
	return nil
}

func (r *Reconciler) observePreparationError(state PairReconciliation, err error) error {
	if state.canPrepare() {
		r.observePreparation(state, nil, runtimePreparationFailure(err, state.DesiredActorTemplate.GetSandboxConfig().GetConfigName(), state.Revision.SandboxClass))
	}
	return err
}

func runtimePreparationFailure(err error, configName string, class atev1alpha1.SandboxClass) *ReconciliationFailure {
	// Backend errors can contain credentials or infrastructure details. Publish
	// only the code and expected configuration, never the raw error or details.
	message := fmt.Sprintf("Runtime preparation failed (%s); check controller logs for details", status.Code(err))
	if status.Code(err) == codes.FailedPrecondition {
		if class == "" {
			class = atev1alpha1.SandboxClassGvisor
		}
		message = fmt.Sprintf("Substrate rejected runtime preparation (FailedPrecondition); verify SandboxConfig %q exists with spec.sandboxClass=%q and the required runtime assets; check controller logs for details",
			configName, class)
	}
	return &ReconciliationFailure{
		Condition: kagentv1alpha3.AgentTemplateConditionReady,
		Reason:    "RuntimePreparationFailed",
		Message:   message,
		Retryable: true,
	}
}

// Observations belong to the pair's current preparation, independently of how
// long instances or checkpoints keep its old runtime alive in the database.
func (r *Reconciler) observePreparation(state PairReconciliation, template *ateapipb.ActorTemplate, failure *ReconciliationFailure) {
	r.collections.PairRuntimeObservations.ConditionalUpdateObject(PairRuntimeObservation{
		Namespace:         state.Pair.AgentTemplate.Namespace,
		AgentTemplateName: state.Pair.AgentTemplate.Name,
		HarnessName:       state.Pair.Harness.Name,
		RevisionID:        state.RevisionID,
		Template:          template,
		Failure:           failure,
	})
}

func (r *Reconciler) reconcileAgentTemplateStatus(ctx context.Context, key string) error {
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
	if _, err := r.status.AgentTemplates(updated.Namespace).UpdateStatus(ctx, updated, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update AgentTemplate %s status: %w", key, err)
	}
	return nil
}

func (r *Reconciler) reconcileModelConfigStatus(ctx context.Context, key string) error {
	desired := r.collections.ModelConfigStatuses.GetKey(key)
	modelConfig := r.collections.ModelConfigs.GetKey(key)
	if desired == nil || modelConfig == nil {
		return nil
	}
	updated := (*modelConfig).DeepCopy()
	updated.Status = modelConfigStatusWithTransitionTimes(desired.Status, updated.Status)
	if apiequality.Semantic.DeepEqual(updated.Status, (*modelConfig).Status) {
		return nil
	}
	if _, err := r.status.ModelConfigs(updated.Namespace).UpdateStatus(ctx, updated, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update ModelConfig %s status: %w", key, err)
	}
	return nil
}

func (r *Reconciler) reconcileHarnessStatus(ctx context.Context, key string) error {
	desired := r.collections.HarnessStatuses.GetKey(key)
	harness := r.collections.Harnesses.GetKey(key)
	if desired == nil || harness == nil {
		return nil
	}
	updated := (*harness).DeepCopy()
	updated.Status = harnessStatusWithTransitionTimes(desired.Status, updated.Status)
	if apiequality.Semantic.DeepEqual(updated.Status, (*harness).Status) {
		return nil
	}
	if _, err := r.status.Harnesses(updated.Namespace).UpdateStatus(ctx, updated, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update Harness %s status: %w", key, err)
	}
	return nil
}

func harnessStatusWithTransitionTimes(desired, current kagentv1alpha3.HarnessStatus) kagentv1alpha3.HarnessStatus {
	desired.Conditions = append([]metav1.Condition(nil), desired.Conditions...)
	for conditionIndex := range desired.Conditions {
		condition := &desired.Conditions[conditionIndex]
		if previous := apimeta.FindStatusCondition(current.Conditions, condition.Type); previous != nil &&
			previous.Status == condition.Status && previous.Reason == condition.Reason &&
			previous.Message == condition.Message && previous.ObservedGeneration == condition.ObservedGeneration {
			condition.LastTransitionTime = previous.LastTransitionTime
			continue
		}
		condition.LastTransitionTime = metav1.Now()
	}
	return desired
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

func modelConfigStatusWithTransitionTimes(desired, current kagentv1alpha3.ModelConfigStatus) kagentv1alpha3.ModelConfigStatus {
	desired.Conditions = append([]metav1.Condition(nil), desired.Conditions...)
	for conditionIndex := range desired.Conditions {
		condition := &desired.Conditions[conditionIndex]
		if previous := apimeta.FindStatusCondition(current.Conditions, condition.Type); previous != nil &&
			previous.Status == condition.Status && previous.Reason == condition.Reason &&
			previous.Message == condition.Message && previous.ObservedGeneration == condition.ObservedGeneration {
			condition.LastTransitionTime = previous.LastTransitionTime
			continue
		}
		condition.LastTransitionTime = metav1.Now()
	}
	return desired
}
