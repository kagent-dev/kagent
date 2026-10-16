package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"golang.org/x/time/rate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
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
	ListRuntimeEgressTargets(context.Context, database.RuntimeEgressSource, string, int) ([]database.RuntimeEgressTarget, error)
}

type policyGrants struct {
	Source database.RuntimeEgressSource
	UID    string
	Access egress.Access
}

func (p policyGrants) ResourceName() string {
	return p.Source.Kind + "/" + p.Source.Namespace + "/" + p.Source.Name
}

// EgressResolver resolves mutable policy grants independently of runtime
// compilation. Broken templates and pools must not prevent revocation.
type EgressResolver struct {
	grants     krt.Collection[policyGrants]
	store      runtimeEgressStore
	namespaces []string
	reader     ctrlclient.Reader
	warnings   *rate.Limiter
}

var _ egress.Resolver = (*EgressResolver)(nil)

func NewEgressResolver(runtime *Runtime, store runtimeEgressStore, reader ctrlclient.Reader, namespaces []string) *EgressResolver {
	inputs := runtime.Collections
	collections := translator.Collections{EgressPolicies: inputs.EgressPolicies, Secrets: inputs.Secrets}
	agents := krt.NewCollection(inputs.Agents, func(ctx krt.HandlerContext, agent *kagentv1alpha3.Agent) *policyGrants {
		grants := &policyGrants{Source: database.RuntimeEgressSource{Kind: "agent", Namespace: agent.Namespace, Name: agent.Name}, UID: string(agent.UID)}
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
		grants := &policyGrants{Source: database.RuntimeEgressSource{Kind: "sandbox", Namespace: template.Namespace, Name: template.Name}, UID: string(template.UID)}
		if template.DeletionTimestamp.IsZero() {
			access, err := translator.CompileEgressPolicies(ctx, collections, template.Namespace, template.Spec.EgressPolicyRefs)
			if err == nil {
				grants.Access = access
			}
		}
		return grants
	}, runtime.Options.WithName("SandboxEgressGrants")...)
	return &EgressResolver{reader: reader, warnings: rate.NewLimiter(rate.Every(30*time.Second), 5), store: store, namespaces: slices.Clone(namespaces), grants: krt.JoinCollection([]krt.Collection[policyGrants]{agents, sandboxes}, runtime.Options.WithName("RuntimeEgressGrants")...)}
}

func (e *EgressResolver) watches(namespace string) bool {
	return len(e.namespaces) == 0 || slices.Contains(e.namespaces, namespace)
}

func (e *EgressResolver) ResolveEgress(ctx context.Context, revision string) (egress.Access, error) {
	pinned, err := e.store.GetRuntimeEgress(ctx, revision)
	if err != nil {
		return egress.Access{}, err
	}
	if !e.watches(pinned.Namespace) {
		return egress.Access{}, fmt.Errorf("runtime namespace %q is not watched", pinned.Namespace)
	}
	base := egress.Access{Destinations: pinned.EgressDestinations, Credentials: pinned.Credentials}
	grants, err := e.currentGrants(ctx, pinned)
	if err != nil {
		return egress.Access{}, err
	}
	access, err := egress.MergeAccess(base, grants, pinned.PassthroughOrigins)
	if err != nil {
		// A current grant may conflict with an old session's pinned integrations.
		// Revoke its explicit access instead of preserving a stale policy.
		if e.warnings.Allow() {
			logging.FromContext(ctx).WarnContext(ctx, "explicit egress grants withheld", "revision", revision, "namespace", pinned.Namespace, "source", pinned.SourceName, "error", err)
		}
		return base, nil
	}
	return access, nil
}

// Read through the API server, never an informer cache: create and resume may
// run on a replica whose watches have not observed a revocation yet.
func (e *EgressResolver) currentGrants(ctx context.Context, pinned *database.RuntimeEgress) (egress.Access, error) {
	var source ctrlclient.Object
	var refs []corev1.LocalObjectReference
	key := ctrlclient.ObjectKey{Namespace: pinned.Namespace, Name: pinned.SourceName}
	switch pinned.Kind {
	case "agent":
		source = &kagentv1alpha3.Agent{}
	case "sandbox":
		source = &kagentv1alpha3.SandboxTemplate{}
	default:
		return egress.Access{}, fmt.Errorf("unknown runtime kind %q", pinned.Kind)
	}
	if err := e.reader.Get(ctx, key, source); err != nil {
		if apierrors.IsNotFound(err) {
			return egress.Access{}, nil
		}
		return egress.Access{}, fmt.Errorf("read egress source %s: %w", key, err)
	}
	if string(source.GetUID()) != pinned.SourceUID || !source.GetDeletionTimestamp().IsZero() {
		return egress.Access{}, nil
	}
	switch object := source.(type) {
	case *kagentv1alpha3.Agent:
		refs = object.Spec.EgressPolicyRefs
	case *kagentv1alpha3.SandboxTemplate:
		refs = object.Spec.EgressPolicyRefs
	}
	access, err := translator.ResolveEgressPolicies(ctx, e.reader, pinned.Namespace, refs)
	var invalid *translator.ValidationError
	if apierrors.IsNotFound(err) || errors.As(err, &invalid) {
		return egress.Access{}, nil
	}
	return access, err
}

type actorEgressClient interface {
	GetActor(context.Context, string, string) (*ateapipb.Actor, error)
	EnsureActorEgressPolicy(context.Context, string, string, string) error
}

// EgressReconciler queues only instances of changed sources. A slow sweep
// recovers missed events and actor-creation races, including suspended actors.
// Pages and actors have independent retry queues so failures cannot stall grants
// for another source or cause successful actors to be retried with a failed one.
type EgressReconciler struct {
	resolver *EgressResolver
	actors   actorEgressClient
}

type egressPage struct {
	Source database.RuntimeEgressSource
	After  string
}

var _ manager.LeaderElectionRunnable = (*EgressReconciler)(nil)

func NewEgressReconciler(resolver *EgressResolver, actors actorEgressClient) *EgressReconciler {
	return &EgressReconciler{resolver: resolver, actors: actors}
}

func (e *EgressReconciler) NeedLeaderElection() bool { return true }

func (e *EgressReconciler) Start(ctx context.Context) error {
	reconcile := func(item any) error {
		err := e.reconcileActor(ctx, item.(database.RuntimeEgressTarget))
		if status.Code(err) == codes.NotFound {
			return nil
		}
		return err
	}
	actors := newReconciliationQueue("live-egress-actors", reconcile)
	// Recovery has its own worker so a large sweep cannot delay event-driven revocations.
	recovery := newReconciliationQueue("live-egress-recovery", reconcile)
	var pages controllers.Queue
	pages = newReconciliationQueue("live-egress-sources", func(item any) error {
		page := item.(egressPage)
		const pageSize = 100
		targets, err := e.resolver.store.ListRuntimeEgressTargets(ctx, page.Source, page.After, pageSize)
		if err != nil {
			return err
		}
		for _, target := range targets {
			if e.resolver.watches(target.Atespace) {
				if page.Source.Kind == "" {
					recovery.Add(target)
				} else {
					actors.Add(target)
				}
			}
		}
		if len(targets) == pageSize {
			pages.Add(egressPage{Source: page.Source, After: targets[len(targets)-1].ID})
		}
		return nil
	})
	defer actors.ShutDownEarly()
	defer recovery.ShutDownEarly()
	defer pages.ShutDownEarly()
	handler := e.resolver.grants.RegisterBatch(func(events []krt.Event[policyGrants]) {
		for _, event := range events {
			pages.Add(egressPage{Source: event.Latest().Source})
		}
	}, false)
	defer handler.UnregisterHandler()
	if !handler.WaitUntilSynced(ctx.Done()) {
		return ctx.Err()
	}
	var workers sync.WaitGroup
	workers.Go(func() { actors.Run(ctx.Done()) })
	workers.Go(func() { recovery.Run(ctx.Done()) })
	workers.Go(func() { pages.Run(ctx.Done()) })
	defer workers.Wait()
	pages.Add(egressPage{})
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			pages.Add(egressPage{})
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
		return fmt.Errorf("actor identity or template changed")
	}
	return e.actors.EnsureActorEgressPolicy(ctx, binding.Atespace, binding.Name, target.Revision)
}
