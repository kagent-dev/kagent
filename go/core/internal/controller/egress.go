package controller

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"istio.io/istio/pkg/kube/krt"
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
	ListRuntimeEgressTargets(context.Context, string, int) ([]database.RuntimeEgressTarget, error)
}

type policyGrants struct {
	Key    string
	UID    string
	Access egress.Access
}

func (p policyGrants) ResourceName() string { return p.Key }

// EgressResolver resolves mutable policy grants independently of runtime
// compilation. Broken templates and pools must not prevent revocation.
type EgressResolver struct {
	grants     krt.Collection[policyGrants]
	store      runtimeEgressStore
	namespaces []string
}

var _ egress.Resolver = (*EgressResolver)(nil)

func NewEgressResolver(runtime *Runtime, store runtimeEgressStore, namespaces []string) *EgressResolver {
	inputs := runtime.Collections
	collections := translator.Collections{EgressPolicies: inputs.EgressPolicies, Secrets: inputs.Secrets}
	agents := krt.NewCollection(inputs.Agents, func(ctx krt.HandlerContext, agent *kagentv1alpha3.Agent) *policyGrants {
		grants := &policyGrants{Key: "agent/" + agent.Namespace + "/" + agent.Name, UID: string(agent.UID)}
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
		grants := &policyGrants{Key: "sandbox/" + template.Namespace + "/" + template.Name, UID: string(template.UID)}
		if template.DeletionTimestamp.IsZero() {
			access, err := translator.CompileEgressPolicies(ctx, collections, template.Namespace, template.Spec.EgressPolicyRefs)
			if err == nil {
				grants.Access = access
			}
		}
		return grants
	}, runtime.Options.WithName("SandboxEgressGrants")...)
	return &EgressResolver{store: store, namespaces: slices.Clone(namespaces), grants: krt.JoinCollection([]krt.Collection[policyGrants]{agents, sandboxes}, runtime.Options.WithName("RuntimeEgressGrants")...)}
}

func (e *EgressResolver) watches(namespace string) bool {
	return len(e.namespaces) == 0 || slices.Contains(e.namespaces, namespace)
}

func (e *EgressResolver) ResolveEgress(ctx context.Context, revision string) (egress.Access, error) {
	if !e.grants.WaitUntilSynced(ctx.Done()) {
		return egress.Access{}, fmt.Errorf("egress policy cache has not synchronized: %w", ctx.Err())
	}
	pinned, err := e.store.GetRuntimeEgress(ctx, revision)
	if err != nil {
		return egress.Access{}, err
	}
	if !e.watches(pinned.Namespace) {
		return egress.Access{}, fmt.Errorf("runtime namespace %q is not watched", pinned.Namespace)
	}
	base := egress.Access{Destinations: pinned.EgressDestinations, Credentials: pinned.Credentials}
	grants := e.grants.GetKey(pinned.Kind + "/" + pinned.Namespace + "/" + pinned.SourceName)
	// Deleted and recreated sources cannot pass their grants to old sessions.
	if grants == nil || grants.UID != pinned.SourceUID {
		return base, nil
	}
	access, err := egress.MergeAccess(base, grants.Access, pinned.PassthroughOrigins)
	if err != nil {
		// A current grant may conflict with an old session's pinned integrations.
		// Revoke its explicit access instead of preserving a stale policy.
		logging.FromContext(ctx).DebugContext(ctx, "live egress grants conflict with pinned runtime", "revision", revision, "error", err)
		return base, nil
	}
	return access, nil
}

type actorEgressClient interface {
	GetActor(context.Context, string, string) (*ateapipb.Actor, error)
	EnsureActorEgressPolicy(context.Context, string, string, string) error
}

// EgressReconciler refreshes every allocated actor, including suspended actors.
// Events apply changes promptly; polling recovers actor-creation races, missed
// events and controller restarts. No model turn or runtime restart is required.
type EgressReconciler struct {
	resolver *EgressResolver
	actors   actorEgressClient
}

var _ manager.LeaderElectionRunnable = (*EgressReconciler)(nil)

func NewEgressReconciler(resolver *EgressResolver, actors actorEgressClient) *EgressReconciler {
	return &EgressReconciler{resolver: resolver, actors: actors}
}

func (e *EgressReconciler) NeedLeaderElection() bool { return true }

func (e *EgressReconciler) Start(ctx context.Context) error {
	wake := make(chan struct{}, 1)
	handler := e.resolver.grants.RegisterBatch(func([]krt.Event[policyGrants]) {
		select {
		case wake <- struct{}{}:
		default:
		}
	}, true)
	defer handler.UnregisterHandler()
	if !handler.WaitUntilSynced(ctx.Done()) {
		return ctx.Err()
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := e.reconcile(ctx); err != nil && ctx.Err() == nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to reconcile live egress", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-wake:
		case <-ticker.C:
		}
	}
}

func (e *EgressReconciler) reconcile(ctx context.Context) error {
	const pageSize = 100
	after := ""
	for {
		targets, err := e.resolver.store.ListRuntimeEgressTargets(ctx, after, pageSize)
		if err != nil {
			return err
		}
		for _, target := range targets {
			if !e.resolver.watches(target.Atespace) {
				continue
			}
			if err := e.reconcileActor(ctx, target); err != nil && status.Code(err) != codes.NotFound {
				logging.FromContext(ctx).ErrorContext(ctx, "failed to update actor egress", "instance_id", target.ID, "error", err)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if len(targets) < pageSize {
			return nil
		}
		after = targets[len(targets)-1].ID
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
