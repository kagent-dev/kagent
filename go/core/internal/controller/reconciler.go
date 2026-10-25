package controller

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"time"

	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
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

	kagentclient "github.com/kagent-dev/kagent/go/api/clientset/versioned/typed/api/v1alpha3"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	byotranslator "github.com/kagent-dev/kagent/go/core/internal/translator/byo"
	claudetranslator "github.com/kagent-dev/kagent/go/core/internal/translator/claude"
	codextranslator "github.com/kagent-dev/kagent/go/core/internal/translator/codex"
	kagenttranslator "github.com/kagent-dev/kagent/go/core/internal/translator/kagent"
)

// AgentReconciliation is the complete desired and observed state for one
// Agent. Compilation failures are data so invalid Agents still produce
// status instead of disappearing from the graph.
type AgentReconciliation struct {
	Agent                 *kagentv1alpha3.Agent
	EgressGrant           egress.Access // Requested union, including conflicting grants.
	EgressIssues          []translator.EgressGrantIssue
	EgressConflict        string
	IntegrationGrants     []egress.IntegrationGrant // Last applied sources, or sources from a successfully compiled target.
	Target                *compiledTarget
	Warnings              []string
	CompilationFailure    *ReconciliationFailure
	ObservedActorTemplate *ateapipb.ActorTemplate
	PreparationFailure    *ReconciliationFailure
}

// compiledTarget is published only after compilation, hashing, and ActorTemplate
// construction all succeed. An absent target means there is no desired runtime.
type compiledTarget struct {
	Revision          translator.Revision
	RevisionID        translator.RevisionID
	ActorTemplate     *ateapipb.ActorTemplate
	IntegrationGrants []egress.IntegrationGrant
}

func (c *compiledTarget) equals(other *compiledTarget) bool {
	if c == nil || other == nil {
		return c == other
	}
	return c.RevisionID == other.RevisionID && c.Revision.Equals(other.Revision) &&
		proto.Equal(c.ActorTemplate, other.ActorTemplate) && reflect.DeepEqual(c.IntegrationGrants, other.IntegrationGrants)
}

func (r AgentReconciliation) ResourceName() string { return r.Agent.Namespace + "/" + r.Agent.Name }

var _ krt.Equaler[AgentReconciliation] = AgentReconciliation{}

// Equals keeps KRT from reflecting over protobuf caches that mutate during reads.
func (r AgentReconciliation) Equals(other AgentReconciliation) bool {
	if !r.Target.equals(other.Target) ||
		!proto.Equal(r.ObservedActorTemplate, other.ObservedActorTemplate) {
		return false
	}
	r.Target, other.Target = nil, nil
	r.ObservedActorTemplate, other.ObservedActorTemplate = nil, nil
	return reflect.DeepEqual(r, other)
}

func (r AgentReconciliation) desiredRevision() string {
	if r.Target == nil {
		return ""
	}
	return r.Target.RevisionID.String()
}

// ReconciliationFailure identifies the condition stage blocked by an Agent.
type ReconciliationFailure struct {
	Condition string
	Reason    string
	Message   string
	Retryable bool
}

func newAgentReconciliations(
	agents krt.Collection[*kagentv1alpha3.Agent],
	collections translator.Collections,
	agentRuntimeObservations krt.Collection[AgentRuntimeObservation],
	opts krt.OptionsBuilder,
	settings *configv1.RuntimeDefaultsConfig,
	images *configv1.HarnessImagesConfig,
) krt.Collection[AgentReconciliation] {
	return krt.NewCollection(agents, func(ctx krt.HandlerContext, agent *kagentv1alpha3.Agent) *AgentReconciliation {
		state := &AgentReconciliation{Agent: agent}
		if !agent.DeletionTimestamp.IsZero() {
			return state
		}
		resolution := translator.CompileEgressPolicies(ctx, collections, agent.Namespace, agent.Spec.EgressPolicyRefs)
		state.EgressGrant, state.EgressIssues = resolution.Access, resolution.Issues
		state.Target, state.Warnings, state.CompilationFailure = compileAgentTarget(ctx, collections, agent, settings, images)
		observed := krt.FetchOne(ctx, agentRuntimeObservations, krt.FilterKey(state.ResourceName()))
		if state.Target != nil {
			state.IntegrationGrants = state.Target.IntegrationGrants
		} else if observed != nil && observed.AgentUID == string(agent.UID) {
			state.IntegrationGrants = observed.IntegrationGrants
		}
		integrations, applied := translator.ResolveIntegrationGrants(ctx, collections, agent.Namespace, state.IntegrationGrants)
		state.IntegrationGrants = applied
		state.EgressGrant.Destinations = append(state.EgressGrant.Destinations, integrations.Access.Destinations...)
		state.EgressGrant.TokenPassthroughTargets = append(state.EgressGrant.TokenPassthroughTargets, integrations.Access.TokenPassthroughTargets...)
		state.EgressIssues = append(state.EgressIssues, integrations.Issues...)
		var err error
		if state.Target == nil {
			// Unrelated compilation failures must not freeze grant updates.
			_, err = egress.CanonicalAccess(state.EgressGrant.Destinations, state.EgressGrant.Credentials, state.EgressGrant.TokenPassthroughTargets)
		} else {
			revision := &state.Target.Revision
			base := egress.Access{Destinations: revision.EgressDestinations, Credentials: revision.Credentials, TokenPassthroughTargets: revision.TokenPassthroughTargets}
			_, err = egress.MergeAccess(base, state.EgressGrant)
		}
		if err != nil {
			state.EgressConflict = err.Error()
		}
		if state.CompilationFailure != nil {
			return state
		}

		if observed == nil || observed.RevisionID != state.Target.RevisionID {
			return state
		}
		if observed.Failure != nil {
			state.PreparationFailure = observed.Failure
			return state
		}
		state.ObservedActorTemplate = (*observed).Template
		if !substrate.ActorTemplateSpecEqual(state.ObservedActorTemplate, state.Target.ActorTemplate) {
			state.PreparationFailure = &ReconciliationFailure{
				Condition: kagentv1alpha3.AgentConditionReady,
				Reason:    "ActorTemplateConflict",
				Message:   "existing immutable ActorTemplate differs from the compiled revision",
			}
		} else if message := state.ObservedActorTemplate.GetStatus().GetGoldenSnapshotStatus().GetErrorMessage(); message != "" {
			state.PreparationFailure = &ReconciliationFailure{Condition: kagentv1alpha3.AgentConditionReady, Reason: "ActorTemplateFailed", Message: message}
		}
		return state
	}, opts.WithName("AgentReconciliations")...)
}

func compileAgentTarget(ctx krt.HandlerContext, collections translator.Collections, agent *kagentv1alpha3.Agent, settings *configv1.RuntimeDefaultsConfig, images *configv1.HarnessImagesConfig) (*compiledTarget, []string, *ReconciliationFailure) {
	compilation, err := translator.NewCompiler(ctx, collections, map[translator.HarnessType]translator.HarnessCompiler{
		translator.HarnessTypeKagent: kagenttranslator.NewCompiler(ctx, collections, settings),
		translator.HarnessTypeCodex:  codextranslator.NewCompiler(ctx, collections, settings),
		translator.HarnessTypeClaude: claudetranslator.NewCompiler(ctx, collections, settings),
		translator.HarnessTypeBYO:    byotranslator.NewCompiler(ctx, collections, settings),
	}, images, settings).CompileAgent(context.Background(), agent)
	if err != nil {
		return nil, nil, compilationFailure(err)
	}
	revision := &compilation.Revision
	warnings := slices.Clone(compilation.Warnings)
	revisionID, err := revision.Digest()
	if err != nil {
		return nil, warnings, &ReconciliationFailure{Condition: kagentv1alpha3.AgentConditionCompatible, Reason: "RevisionInvalid", Message: err.Error()}
	}
	actorTemplate, err := substrate.ActorTemplateForRevision(revision, revisionID)
	if err != nil {
		return nil, warnings, &ReconciliationFailure{Condition: kagentv1alpha3.AgentConditionCompatible, Reason: "ActorTemplateInvalid", Message: err.Error()}
	}
	return &compiledTarget{Revision: *revision, RevisionID: revisionID, ActorTemplate: actorTemplate, IntegrationGrants: compilation.IntegrationGrants}, warnings, nil
}

func compilationFailure(err error) *ReconciliationFailure {
	condition, reason := kagentv1alpha3.AgentConditionResolvedRefs, "ReferenceResolutionFailed"
	var validation *translator.ValidationError
	var missingPool *translator.WorkerPoolNotFoundError
	switch {
	case errors.As(err, &validation):
		// TODO(tim): Separate structural Agent validation from resolved
		// compatibility failures so invalid sources do not report Accepted=True
		// and ResolvedRefs=True.
		condition, reason = kagentv1alpha3.AgentConditionCompatible, "UnsupportedConfiguration"
	case errors.As(err, &missingPool):
		reason = "WorkerPoolNotFound"
	}
	return &ReconciliationFailure{Condition: condition, Reason: reason, Message: err.Error()}
}

// runtimeRevisionStore is the controller's narrow view of the shared database.
// Substrate owns ActorTemplates; the database retains revisions while an Agent
// or a Session or checkpoint references them.
type runtimeRevisionStore interface {
	actorEgressStore
	UpsertAgentDefinition(context.Context, database.AgentDefinition) error
	GetAgentEgressGrant(context.Context, string, string) (egress.Access, error)
	GetAgentIntegrationGrants(context.Context, string, string) ([]egress.IntegrationGrant, error)
	RecordRuntimeRevision(context.Context, database.RuntimeRevision, database.RecordRevisionOptions) error
	ListAgentEgressSources(context.Context) ([]database.RuntimeEgressSource, error)
	RetireAgentIdentities(ctx context.Context, namespace, name string) error
}

type actorTemplateClient interface {
	actorEgressClient
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

	agents                   controllers.Queue
	agentStatuses            controllers.Queue
	modelConfigStatuses      controllers.Queue
	agentHandler             krt.HandlerRegistration
	agentStatusHandler       krt.HandlerRegistration
	modelConfigStatusHandler krt.HandlerRegistration
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
	r.agents = newReconciliationQueue("agents", func(item any) error {
		return r.reconcileAgent(context.Background(), item.(string))
	})
	r.agentStatuses = newReconciliationQueue("agent-status", func(item any) error {
		return r.reconcileAgentStatus(context.Background(), item.(string))
	})
	r.modelConfigStatuses = newReconciliationQueue("model-config-status", func(item any) error {
		return r.reconcileModelConfigStatus(context.Background(), item.(string))
	})

	r.agentHandler = collections.Reconciliations.Register(func(event krt.Event[AgentReconciliation]) {
		// Desired inputs and cleanup changes bypass failure backoff; observations
		// and status writes do not. Grants must reconcile without a new revision.
		if event.Old != nil && event.New != nil && event.Old.Agent.UID == event.New.Agent.UID &&
			event.Old.desiredRevision() == event.New.desiredRevision() &&
			reflect.DeepEqual(event.Old.EgressGrant, event.New.EgressGrant) &&
			slices.EqualFunc(event.Old.IntegrationGrants, event.New.IntegrationGrants, func(a, b egress.IntegrationGrant) bool { return reflect.DeepEqual(a, b) }) &&
			reflect.DeepEqual(event.Old.EgressIssues, event.New.EgressIssues) &&
			event.Old.EgressConflict == event.New.EgressConflict && event.Old.Agent.DeletionTimestamp.Equal(event.New.Agent.DeletionTimestamp) {
			return
		}
		r.agents.Add(krt.GetKey(event.Latest()))
	})
	r.agentStatusHandler = collections.AgentStatuses.Register(func(event krt.Event[krt.ObjectWithStatus[*kagentv1alpha3.Agent, kagentv1alpha3.AgentStatus]]) {
		status := event.Latest()
		if apiequality.Semantic.DeepEqual(statusWithTransitionTimes(status.Status, status.Obj.Status), status.Obj.Status) {
			return
		}
		r.agentStatuses.Add(status.ResourceName())
	})
	r.modelConfigStatusHandler = collections.ModelConfigStatuses.Register(func(event krt.Event[krt.ObjectWithStatus[*kagentv1alpha3.ModelConfig, kagentv1alpha3.ModelConfigStatus]]) {
		status := event.Latest()
		if apiequality.Semantic.DeepEqual(modelConfigStatusWithTransitionTimes(status.Status, status.Obj.Status), status.Obj.Status) {
			return
		}
		r.modelConfigStatuses.Add(status.ResourceName())
	})
	return r
}

// Keep retrying at a capped interval so prerequisites can recover without a
// Kubernetes event. Istio's queue requires a positive attempt limit; MaxInt
// makes that limit unreachable during a controller's lifetime.
func newReconciliationQueue(name string, reconcile func(any) error) controllers.Queue {
	return controllers.NewQueue(name, controllers.WithGenericReconciler(reconcile),
		controllers.WithMaxAttempts(math.MaxInt),
		controllers.WithRateLimiter(workqueue.NewTypedItemExponentialFailureRateLimiter[any](time.Second, 30*time.Second)),
	)
}

// Start waits for initial Kubernetes state, recovers offline deletions, then
// processes Agent preparation and status writes until cancellation.
func (r *Reconciler) Start(ctx context.Context) error {
	stop := ctx.Done()
	if !r.agentHandler.WaitUntilSynced(stop) || !r.agentStatusHandler.WaitUntilSynced(stop) || !r.modelConfigStatusHandler.WaitUntilSynced(stop) {
		r.agents.ShutDownEarly()
		r.agentStatuses.ShutDownEarly()
		r.modelConfigStatuses.ShutDownEarly()
		return nil
	}
	if err := r.enqueueDeletedAgents(ctx); err != nil {
		r.agents.ShutDownEarly()
		r.agentStatuses.ShutDownEarly()
		r.modelConfigStatuses.ShutDownEarly()
		return err
	}
	go r.pollPendingTemplates(stop)
	go r.agentStatuses.Run(stop)
	go r.modelConfigStatuses.Run(stop)
	r.agents.Run(stop)
	return nil
}

func (r *Reconciler) enqueueDeletedAgents(ctx context.Context) error {
	sources, err := r.store.ListAgentEgressSources(ctx)
	if err != nil {
		return fmt.Errorf("recover deleted Agents: %w", err)
	}
	for _, source := range sources {
		if len(r.collections.watchNamespaces) != 0 && !slices.Contains(r.collections.watchNamespaces, source.Namespace) {
			continue
		}
		key := source.Namespace + "/" + source.Name
		if r.collections.Agents.GetKey(key) == nil {
			r.agents.Add(key)
		}
	}
	return nil
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
				if state.Target != nil && state.PreparationFailure == nil && state.ObservedActorTemplate != nil && golden.GetGoldenTag() == nil {
					r.agents.Add(state.ResourceName())
				}
			}
		}
	}
}

func (r *Reconciler) NeedLeaderElection() bool { return true }

func (r *Reconciler) reconcileAgent(ctx context.Context, key string) error {
	state := r.collections.Reconciliations.GetKey(key)
	if observation := r.collections.AgentRuntimeObservations.GetKey(key); observation != nil &&
		(state == nil || state.Target == nil || observation.RevisionID != state.Target.RevisionID) {
		if state != nil && observation.AgentUID == string(state.Agent.UID) && len(observation.IntegrationGrants) > 0 {
			r.collections.AgentRuntimeObservations.ConditionalUpdateObject(AgentRuntimeObservation{
				Namespace: state.Agent.Namespace, AgentName: state.Agent.Name, AgentUID: observation.AgentUID, IntegrationGrants: observation.IntegrationGrants,
			})
		} else {
			r.collections.AgentRuntimeObservations.DeleteObject(key)
		}
	}
	if state == nil || !state.Agent.DeletionTimestamp.IsZero() {
		parts := strings.Split(key, "/")
		if len(parts) != 2 {
			return fmt.Errorf("invalid Agent key %q", key)
		}
		err := r.store.RetireAgentIdentities(ctx, parts[0], parts[1])
		if err != nil {
			return fmt.Errorf("retire Agent %s: %w", key, err)
		}
		if err := applySourceEgress(ctx, r.store, r.templates, database.RuntimeEgressSource{Kind: database.RuntimeEgressAgent, Namespace: parts[0], Name: parts[1]}); err != nil {
			return err
		}
		return nil
	}
	if state.Target == nil && state.IntegrationGrants == nil {
		// Recover applied integration authority before replacing the grant union.
		// Feeding it into the graph also watches the retained sources when the
		// failed compiler never reaches their current resource references.
		applied, err := r.store.GetAgentIntegrationGrants(ctx, state.Agent.Namespace, string(state.Agent.UID))
		if err != nil {
			return fmt.Errorf("read Agent %s applied integrations: %w", key, err)
		}
		if len(applied) > 0 {
			r.collections.AgentRuntimeObservations.ConditionalUpdateObject(AgentRuntimeObservation{
				Namespace: state.Agent.Namespace, AgentName: state.Agent.Name, AgentUID: string(state.Agent.UID), IntegrationGrants: applied,
			})
			return nil
		}
	}
	grants := state.EgressGrant
	if state.EgressConflict != "" {
		previous, err := r.store.GetAgentEgressGrant(ctx, state.Agent.Namespace, string(state.Agent.UID))
		if err != nil {
			return r.observePreparationError(*state, fmt.Errorf("read Agent %s grants: %w", key, err))
		}
		grants = egress.IntersectAccess(previous, grants)
		if state.Target != nil {
			revision := &state.Target.Revision
			base := egress.Access{Destinations: revision.EgressDestinations, Credentials: revision.Credentials, TokenPassthroughTargets: revision.TokenPassthroughTargets}
			grants, _ = egress.FilterConflicts(base, grants)
		}
	}
	definition := database.AgentDefinition{
		Namespace: state.Agent.Namespace, AgentName: state.Agent.Name,
		AgentUID: string(state.Agent.UID), DesiredRevision: state.desiredRevision(), EgressGrant: grants, IntegrationGrants: state.IntegrationGrants,
	}

	// Store the desired edge before creating compute so a concurrent collector
	// cannot mistake the revision for abandoned state. Unresolved inputs clear
	// the old desired edge without inventing a runtime revision;
	// the upsert preserves the current UID's last-good runtime in either case.
	if err := r.store.UpsertAgentDefinition(ctx, definition); err != nil {
		// A write error can follow a committed revocation. Reload its durable
		// state before using the cache again, or require hydration on retry if
		// the outcome cannot yet be read. Older observations cannot restore it.
		applied, readErr := r.store.GetAgentIntegrationGrants(ctx, definition.Namespace, definition.AgentUID)
		if readErr != nil {
			r.collections.AgentRuntimeObservations.DeleteObject(key)
			err = errors.Join(err, fmt.Errorf("reload applied integrations: %w", readErr))
		} else {
			r.observeAppliedIntegrations(*state, applied)
		}
		return r.observePreparationError(*state, fmt.Errorf("store Agent %s: %w", key, err))
	}
	r.observeAppliedIntegrations(*state, state.IntegrationGrants)
	if err := applySourceEgress(ctx, r.store, r.templates, database.RuntimeEgressSource{Kind: database.RuntimeEgressAgent, Namespace: definition.Namespace, Name: definition.AgentName}); err != nil {
		return r.observePreparationError(*state, err)
	}
	if state.Target == nil {
		return nil
	}
	if state.PreparationFailure != nil && !state.PreparationFailure.Retryable {
		return nil
	}
	target := state.Target
	desiredRef := target.ActorTemplate.GetMetadata()
	observed, err := r.templates.GetActorTemplate(ctx, desiredRef.GetAtespace(), desiredRef.GetName())
	if status.Code(err) == codes.NotFound {
		if err := r.templates.EnsureAtespace(ctx, desiredRef.GetAtespace()); err != nil {
			return r.observePreparationError(*state, fmt.Errorf("ensure Atespace %s: %w", desiredRef.GetAtespace(), err))
		}
		observed, err = r.templates.CreateActorTemplate(ctx, target.ActorTemplate)
		if status.Code(err) == codes.InvalidArgument {
			// Validation rejects the immutable request, so retries cannot repair
			// it. Backend messages may quote configuration; keep status safe.
			r.observePreparation(*state, nil, &ReconciliationFailure{
				Condition: kagentv1alpha3.AgentConditionReady,
				Reason:    "ActorTemplateRejected",
				Message:   "Substrate rejected the compiled ActorTemplate (InvalidArgument); verify the Agent configuration against Substrate's validation requirements",
			})
			return nil
		}
		if status.Code(err) == codes.AlreadyExists {
			observed, err = r.templates.GetActorTemplate(ctx, desiredRef.GetAtespace(), desiredRef.GetName())
		}
	}
	if err != nil {
		return r.observePreparationError(*state, fmt.Errorf("reconcile ActorTemplate %s/%s: %w", desiredRef.GetAtespace(), desiredRef.GetName(), err))
	}
	if !substrate.ActorTemplateSpecEqual(observed, target.ActorTemplate) {
		r.observePreparation(*state, observed, nil)
		return nil
	}

	revision := database.RuntimeRevision{
		Revision: target.RevisionID.String(), Namespace: definition.Namespace,
		AgentName: definition.AgentName, AgentUID: definition.AgentUID,
		SourceSnapshot: target.Revision.Provenance, AgentCard: target.Revision.AgentCard,
		EgressDestinations:      target.Revision.EgressDestinations,
		Credentials:             target.Revision.Credentials,
		TokenPassthroughTargets: target.Revision.TokenPassthroughTargets,
		ActorTemplateAtespace:   observed.GetMetadata().GetAtespace(), ActorTemplateName: observed.GetMetadata().GetName(), ActorTemplateUID: observed.GetMetadata().GetUid(),
	}
	ready := observed.GetStatus().GetGoldenSnapshotStatus().GetGoldenTag() != nil
	if err := r.store.RecordRuntimeRevision(ctx, revision, database.RecordRevisionOptions{Ready: ready}); err != nil {
		return r.observePreparationError(*state, fmt.Errorf("store runtime revision %s: %w", target.RevisionID, err))
	}

	// This observation drives Kubernetes Ready status on a separate queue.
	// Publish it only after session creation can select the persisted revision.
	r.observePreparation(*state, observed, nil)
	return nil
}

func (r *Reconciler) observePreparationError(state AgentReconciliation, err error) error {
	if state.Target == nil {
		return err
	}
	// Preserve terminal diagnostics if persisting the desired edge fails.
	if state.PreparationFailure != nil && !state.PreparationFailure.Retryable {
		return err
	}
	r.observePreparation(state, nil, runtimePreparationFailure(err))
	return err
}

// observeAppliedIntegrations caches only known durable source state. Keep runtime
// preparation observations independent from live-source revocations.
func (r *Reconciler) observeAppliedIntegrations(state AgentReconciliation, applied []egress.IntegrationGrant) {
	observation := AgentRuntimeObservation{Namespace: state.Agent.Namespace, AgentName: state.Agent.Name, AgentUID: string(state.Agent.UID)}
	if previous := r.collections.AgentRuntimeObservations.GetKey(state.ResourceName()); previous != nil && previous.AgentUID == observation.AgentUID {
		observation = *previous
	}
	observation.IntegrationGrants = applied
	if len(observation.IntegrationGrants) > 0 || observation.Template != nil || observation.Failure != nil {
		r.collections.AgentRuntimeObservations.ConditionalUpdateObject(observation)
	} else {
		r.collections.AgentRuntimeObservations.DeleteObject(state.ResourceName())
	}
}

func runtimePreparationFailure(err error) *ReconciliationFailure {
	// Backend errors can contain credentials or infrastructure details. Publish
	// only the code, never the raw error or details.
	message := fmt.Sprintf("Runtime preparation failed (%s); check controller logs for details", status.Code(err))
	return &ReconciliationFailure{
		Condition: kagentv1alpha3.AgentConditionReady,
		Reason:    "RuntimePreparationFailed",
		Message:   message,
		Retryable: true,
	}
}

// Runtime observations belong to the Agent's current preparation. Applied
// integration sources are preserved independently until a new configuration is
// successfully stored, even when preparation fails before a revision is recorded.
func (r *Reconciler) observePreparation(state AgentReconciliation, template *ateapipb.ActorTemplate, failure *ReconciliationFailure) {
	observation := AgentRuntimeObservation{
		Namespace: state.Agent.Namespace, AgentName: state.Agent.Name,
		AgentUID:   string(state.Agent.UID),
		RevisionID: state.Target.RevisionID,
		Template:   template,
		Failure:    failure,
	}
	if previous := r.collections.AgentRuntimeObservations.GetKey(state.ResourceName()); previous != nil && previous.AgentUID == observation.AgentUID {
		observation.IntegrationGrants = previous.IntegrationGrants
	}
	r.collections.AgentRuntimeObservations.ConditionalUpdateObject(observation)
}

func (r *Reconciler) reconcileAgentStatus(ctx context.Context, key string) error {
	desired := r.collections.AgentStatuses.GetKey(key)
	template := r.collections.Agents.GetKey(key)
	if desired == nil || template == nil {
		return nil
	}
	updated := (*template).DeepCopy()
	updated.Status = statusWithTransitionTimes(desired.Status, updated.Status)
	if apiequality.Semantic.DeepEqual(updated.Status, (*template).Status) {
		return nil
	}
	if _, err := r.status.Agents(updated.Namespace).UpdateStatus(ctx, updated, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update Agent %s status: %w", key, err)
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

func statusWithTransitionTimes(desired, current kagentv1alpha3.AgentStatus) kagentv1alpha3.AgentStatus {
	desired.Conditions = append([]metav1.Condition(nil), desired.Conditions...)
	for i := range desired.Conditions {
		condition := &desired.Conditions[i]
		previous := apimeta.FindStatusCondition(current.Conditions, condition.Type)
		if previous != nil && previous.Status == condition.Status {
			condition.LastTransitionTime = previous.LastTransitionTime
		} else {
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
