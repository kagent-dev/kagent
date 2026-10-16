package controller

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"k8s.io/apimachinery/pkg/util/cache"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

type runtimeEgressStore interface {
	GetRuntimeEgress(context.Context, string) (*database.RuntimeEgress, error)
	SetRuntimeEgressPolicy(context.Context, string, egress.Access) (bool, error)
	ListRuntimeEgressRevisions(context.Context, string, int) ([]string, error)
	ListRuntimeEgressSourceRevisions(context.Context, database.RuntimeEgressSource, string, int) ([]string, error)
	ListRuntimeEgressTargets(context.Context, string, string, int) ([]database.RuntimeEgressTarget, error)
}

// revisionEgressReconciler publishes the initial policy before preparation can
// promote a revision. Later egress-only translations use the same writer.
type revisionEgressReconciler interface {
	ReconcileRevision(context.Context, string) error
}

type policyGrants struct {
	Source database.RuntimeEgressSource
	UID    string
	Access egress.Access
}

func (p policyGrants) ResourceName() string {
	return string(p.Source.Kind) + "/" + p.Source.Namespace + "/" + p.Source.Name
}

func newEgressGrants(runtime *Runtime) krt.Collection[policyGrants] {
	inputs := runtime.Collections
	collections := translator.Collections{EgressPolicies: inputs.EgressPolicies, Secrets: inputs.Secrets}
	agents := krt.NewCollection(inputs.Agents, func(ctx krt.HandlerContext, agent *kagentv1alpha3.Agent) *policyGrants {
		grants := &policyGrants{Source: database.RuntimeEgressSource{Kind: database.RuntimeEgressAgent, Namespace: agent.Namespace, Name: agent.Name}, UID: string(agent.UID)}
		if agent.DeletionTimestamp.IsZero() {
			// CompileEgressPolicies returns empty access on every failure. Runtime
			// compilation reports resolution errors through the source's status.
			access, err := translator.CompileEgressPolicies(ctx, collections, agent.Namespace, agent.Spec.EgressPolicyRefs)
			if err == nil {
				grants.Access = access
			}
		}
		return grants
	}, runtime.Options.WithName("AgentEgressGrants")...)
	sandboxes := krt.NewCollection(inputs.SandboxTemplates, func(ctx krt.HandlerContext, template *kagentv1alpha3.SandboxTemplate) *policyGrants {
		grants := &policyGrants{Source: database.RuntimeEgressSource{Kind: database.RuntimeEgressSandbox, Namespace: template.Namespace, Name: template.Name}, UID: string(template.UID)}
		if template.DeletionTimestamp.IsZero() {
			access, err := translator.CompileEgressPolicies(ctx, collections, template.Namespace, template.Spec.EgressPolicyRefs)
			if err == nil {
				grants.Access = access
			}
		}
		return grants
	}, runtime.Options.WithName("SandboxEgressGrants")...)
	return krt.JoinCollection([]krt.Collection[policyGrants]{agents, sandboxes}, runtime.Options.WithName("RuntimeEgressGrants")...)
}

type actorEgressClient interface {
	GetActor(context.Context, string, string) (*ateapipb.Actor, error)
	EnsureActorEgressPolicy(context.Context, string, string, string) error
}

// EgressReconciler publishes KRT's current grants per retained revision. Actors
// and API replicas apply this persisted output without translating Kubernetes.
// Recovery has separate workers so a full sweep cannot delay changed sources.
type EgressReconciler struct {
	grants     krt.Collection[policyGrants]
	store      runtimeEgressStore
	actors     actorEgressClient
	namespaces []string
	warnings   *cache.LRUExpireCache
	// Initial preparation and source/recovery queues share a publisher. Serialize
	// cache lookup through commit so an older translation cannot commit last.
	publishMu sync.Mutex
	sources   controllers.Queue
	pages     controllers.Queue
	updates   controllers.Queue
	recovery  controllers.Queue
	sweeps    controllers.Queue
}

type egressSourcePage struct {
	Source database.RuntimeEgressSource
	After  string
}

type egressActorPage struct {
	Revision string
	After    string
	Recovery bool
}

type egressSweepPage struct {
	After         string
	RecoverActors bool
}

var _ manager.LeaderElectionRunnable = (*EgressReconciler)(nil)

func NewEgressReconciler(ctx context.Context, runtime *Runtime, store runtimeEgressStore, actors actorEgressClient, namespaces []string) *EgressReconciler {
	e := &EgressReconciler{grants: newEgressGrants(runtime), store: store, actors: actors,
		namespaces: slices.Clone(namespaces), warnings: cache.NewLRUExpireCache(1024)}
	reconcile := func(item any) error {
		err := e.reconcileActor(ctx, item.(database.RuntimeEgressTarget))
		if status.Code(err) == codes.NotFound {
			return nil
		}
		return err
	}
	e.updates = newReconciliationQueue("live-egress-actors", reconcile)
	e.recovery = newReconciliationQueue("live-egress-recovery", reconcile)
	const pageSize = 100
	e.pages = newReconciliationQueue("live-egress-revision-actors", func(item any) error {
		page := item.(egressActorPage)
		targets, err := e.store.ListRuntimeEgressTargets(ctx, page.Revision, page.After, pageSize)
		if err != nil {
			return err
		}
		queue := e.updates
		if page.Recovery {
			queue = e.recovery
		}
		for _, target := range targets {
			queue.Add(target)
		}
		if len(targets) == pageSize {
			page.After = targets[len(targets)-1].ID
			e.pages.Add(page)
		}
		return nil
	})
	e.sources = newReconciliationQueue("live-egress-sources", func(item any) error {
		page := item.(egressSourcePage)
		revisions, err := e.store.ListRuntimeEgressSourceRevisions(ctx, page.Source, page.After, pageSize)
		if err != nil {
			return err
		}
		for _, revision := range revisions {
			if err := e.ReconcileRevision(ctx, revision); err != nil && !errors.Is(err, database.ErrNotFound) {
				return err
			}
		}
		if len(revisions) == pageSize {
			page.After = revisions[len(revisions)-1]
			e.sources.Add(page)
		}
		return nil
	})
	e.sweeps = newReconciliationQueue("live-egress-sweeps", func(item any) error {
		page := item.(egressSweepPage)
		revisions, err := e.store.ListRuntimeEgressRevisions(ctx, page.After, pageSize)
		if err != nil {
			return err
		}
		for _, revision := range revisions {
			err := e.publishRevision(ctx, revision, page.RecoverActors)
			if errors.Is(err, database.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
		}
		if len(revisions) == pageSize {
			page.After = revisions[len(revisions)-1]
			e.sweeps.Add(page)
		}
		return nil
	})
	return e
}

// ReconcileRevision publishes current intended access before making a new
// revision ready, and schedules existing actors independently of preparation.
func (e *EgressReconciler) ReconcileRevision(ctx context.Context, revision string) error {
	if !e.grants.WaitUntilSynced(ctx.Done()) {
		return ctx.Err()
	}
	return e.publishRevision(ctx, revision, false)
}

func (e *EgressReconciler) publishRevision(ctx context.Context, revision string, recoverActors bool) error {
	e.publishMu.Lock()
	defer e.publishMu.Unlock()
	pinned, err := e.store.GetRuntimeEgress(ctx, revision)
	if err != nil {
		return err
	}
	if len(e.namespaces) > 0 && !slices.Contains(e.namespaces, pinned.Namespace) {
		return nil
	}
	base := egress.Access{Destinations: pinned.EgressDestinations, Credentials: pinned.Credentials}
	var grants egress.Access
	key := policyGrants{Source: database.RuntimeEgressSource{Kind: pinned.Kind, Namespace: pinned.Namespace, Name: pinned.SourceName}}
	if current := e.grants.GetKey(key.ResourceName()); current != nil && current.UID == pinned.SourceUID {
		grants = current.Access
	}
	access, err := egress.MergeAccess(base, grants, pinned.TokenPassthroughOrigins)
	if err != nil {
		// Bound warning state while letting every revision report its own failures.
		if _, warned := e.warnings.Get(revision); !warned {
			logging.FromContext(ctx).WarnContext(ctx, "explicit egress grants withheld", "revision", revision, "namespace", pinned.Namespace, "source", pinned.SourceName, "error", err)
			e.warnings.Add(revision, struct{}{}, 30*time.Second)
		}
		access = base
	} else {
		e.warnings.Remove(revision)
	}
	changed, err := e.store.SetRuntimeEgressPolicy(ctx, revision, access)
	if changed || recoverActors || err != nil {
		// A failed response may hide a committed write. Queue actor application
		// even then: a publication retry could observe an unchanged policy.
		e.pages.Add(egressActorPage{Revision: revision, Recovery: recoverActors})
	}
	return err
}

func (e *EgressReconciler) NeedLeaderElection() bool { return true }

func (e *EgressReconciler) Start(ctx context.Context) error {
	handler := e.grants.RegisterBatch(func(events []krt.Event[policyGrants]) {
		for _, event := range events {
			e.sources.Add(egressSourcePage{Source: event.Latest().Source})
		}
	}, false)
	defer handler.UnregisterHandler()
	queues := []controllers.Queue{e.sources, e.pages, e.updates, e.recovery, e.sweeps}
	if !handler.WaitUntilSynced(ctx.Done()) {
		for _, queue := range queues {
			queue.ShutDownEarly()
		}
		return ctx.Err()
	}
	var workers sync.WaitGroup
	for _, queue := range queues {
		workers.Go(func() { queue.Run(ctx.Done()) })
	}
	defer workers.Wait()
	e.sweeps.Add(egressSweepPage{RecoverActors: true})
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			e.sweeps.Add(egressSweepPage{})
		}
	}
}

func (e *EgressReconciler) reconcileActor(ctx context.Context, target database.RuntimeEgressTarget) error {
	binding := substrate.ActorBinding{Atespace: target.Atespace, Name: substrate.ActorName(target.ID), TemplateAtespace: target.Atespace, TemplateName: target.TemplateName}
	actor, err := e.actors.GetActor(ctx, binding.Atespace, binding.Name)
	if err != nil {
		return err
	}
	if !binding.Matches(actor) || (target.ActorUID != "" && target.ActorUID != actor.GetMetadata().GetUid()) {
		// This queued identity no longer owns the actor. Recovery discovers its
		// current binding; retrying this stale target cannot repair it.
		return nil
	}
	return e.actors.EnsureActorEgressPolicy(ctx, binding.Atespace, binding.Name, target.Revision)
}
