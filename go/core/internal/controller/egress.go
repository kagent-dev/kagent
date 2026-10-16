package controller

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"istio.io/istio/pkg/kube/controllers"
	"k8s.io/apimachinery/pkg/util/cache"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

type actorEgressStore interface {
	egress.PolicyStore
	ListRuntimeEgressTargets(context.Context, string, int) ([]database.RuntimeEgressTarget, error)
	ListRuntimeEgressSourceTargets(context.Context, database.RuntimeEgressSource, string, int) ([]database.RuntimeEgressTarget, error)
}

type actorEgressClient interface {
	GetActor(context.Context, string, string) (*ateapipb.Actor, error)
	EnsureActorEgressPolicy(context.Context, string, string, string) error
}

// ActorEgressApplier applies persisted effective policy independently of runtime
// preparation. It never translates Kubernetes inputs or writes source grants.
type ActorEgressApplier struct {
	store      actorEgressStore
	actors     actorEgressClient
	namespaces []string
	warnings   *cache.LRUExpireCache
	pages      controllers.Queue
	updates    controllers.Queue
	recovery   controllers.Queue
}

type egressSourcePage struct {
	Source database.RuntimeEgressSource
	After  string
}

type egressRecoveryPage struct{ After string }

var _ manager.LeaderElectionRunnable = (*ActorEgressApplier)(nil)

func NewActorEgressApplier(ctx context.Context, store actorEgressStore, actors actorEgressClient, namespaces []string) *ActorEgressApplier {
	a := &ActorEgressApplier{store: store, actors: actors, namespaces: slices.Clone(namespaces), warnings: cache.NewLRUExpireCache(1024)}
	reconcile := func(item any) error {
		err := a.reconcileActor(ctx, item.(database.RuntimeEgressTarget))
		if status.Code(err) == codes.NotFound {
			return nil
		}
		return err
	}
	a.updates = newReconciliationQueue("live-egress-actors", reconcile)
	a.recovery = newReconciliationQueue("live-egress-recovery", reconcile)
	const pageSize = 100
	a.pages = newReconciliationQueue("live-egress-pages", func(item any) error {
		switch page := item.(type) {
		case egressSourcePage:
			if !a.watches(page.Source.Namespace) {
				return nil
			}
			targets, err := a.store.ListRuntimeEgressSourceTargets(ctx, page.Source, page.After, pageSize)
			if err != nil {
				return err
			}
			for _, target := range targets {
				a.updates.Add(target)
			}
			if len(targets) == pageSize {
				page.After = targets[len(targets)-1].ID
				a.pages.Add(page)
			}
		case egressRecoveryPage:
			targets, err := a.store.ListRuntimeEgressTargets(ctx, page.After, pageSize)
			if err != nil {
				return err
			}
			for _, target := range targets {
				if a.watches(target.Namespace) {
					a.recovery.Add(target)
				}
			}
			if len(targets) == pageSize {
				page.After = targets[len(targets)-1].ID
				a.pages.Add(page)
			}
		}
		return nil
	})
	return a
}

func (a *ActorEgressApplier) watches(namespace string) bool {
	return len(a.namespaces) == 0 || slices.Contains(a.namespaces, namespace)
}

// EnqueueSource schedules all allocated instances of this source, across UIDs.
func (a *ActorEgressApplier) EnqueueSource(source database.RuntimeEgressSource) {
	a.pages.Add(egressSourcePage{Source: source})
}

func (a *ActorEgressApplier) NeedLeaderElection() bool { return true }

func (a *ActorEgressApplier) Start(ctx context.Context) error {
	var workers sync.WaitGroup
	workers.Go(func() { a.updates.Run(ctx.Done()) })
	workers.Go(func() { a.recovery.Run(ctx.Done()) })
	a.pages.Add(egressRecoveryPage{})
	a.pages.Run(ctx.Done())
	workers.Wait()
	return nil
}

func (a *ActorEgressApplier) reconcileActor(ctx context.Context, target database.RuntimeEgressTarget) error {
	if !a.watches(target.Namespace) {
		return nil
	}
	binding := substrate.ActorBinding{Atespace: target.Atespace, Name: substrate.ActorName(target.ID), TemplateAtespace: target.Atespace, TemplateName: target.TemplateName}
	actor, err := a.actors.GetActor(ctx, binding.Atespace, binding.Name)
	if err != nil {
		return err
	}
	if !binding.Matches(actor) || (target.ActorUID != "" && target.ActorUID != actor.GetMetadata().GetUid()) {
		return nil
	}
	policy, err := a.store.GetRuntimeEgressPolicy(ctx, target.Revision)
	if err != nil {
		return err
	}
	if policy.WithheldGrants != nil {
		if _, warned := a.warnings.Get(target.Revision); !warned {
			logging.FromContext(ctx).WarnContext(ctx, "explicit egress grants withheld", "revision", target.Revision, "namespace", target.Namespace, "error", policy.WithheldGrants)
			a.warnings.Add(target.Revision, struct{}{}, 30*time.Second)
		}
	} else {
		a.warnings.Remove(target.Revision)
	}
	return a.actors.EnsureActorEgressPolicy(ctx, binding.Atespace, binding.Name, target.Revision)
}
