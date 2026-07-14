package substrate

import (
	"context"
	"fmt"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AgentsBackend implements sandboxbackend.Backend for declarative/BYO SandboxAgents on Agent Substrate.
type AgentsBackend struct {
	Lifecycle *Lifecycle
	AteClient *Client
}

var _ sandboxbackend.Backend = (*AgentsBackend)(nil)

// NewAgentsBackend returns a substrate sandbox backend for SandboxAgent resources.
func NewAgentsBackend(lifecycle *Lifecycle, ate *Client) *AgentsBackend {
	return &AgentsBackend{Lifecycle: lifecycle, AteClient: ate}
}

func (b *AgentsBackend) GetOwnedResourceTypes() []client.Object {
	return []client.Object{&atev1alpha1.ActorTemplate{}}
}

// OwnedResourceTypesFor returns no types: substrate ActorTemplates are intentionally excluded
// from the reconciler's generic prune so a config change does not delete the currently-serving
// template. A config change creates a new config-hashed template; superseded templates and their
// (suspended) goldens are stateful and pin no workers, so they are retained — not retired — and
// removed only when the SandboxAgent is deleted (DeleteAllSandboxAgentActors +
// CleanupSandboxAgentTemplate, plus owner-reference GC of the template objects). ActorTemplate
// remains in GetOwnedResourceTypes for watches.
func (b *AgentsBackend) OwnedResourceTypesFor(_ v1alpha2.AgentObject) ([]client.Object, error) {
	return nil, nil
}

func (b *AgentsBackend) BuildSandbox(ctx context.Context, in sandboxbackend.BuildInput) ([]client.Object, error) {
	sa, ok := in.Agent.(*v1alpha2.SandboxAgent)
	if !ok || sa == nil {
		return nil, fmt.Errorf("substrate sandbox backend requires a SandboxAgent")
	}
	if b.Lifecycle == nil {
		return nil, fmt.Errorf("substrate lifecycle is not configured")
	}
	var workerPoolRef *v1alpha2.TypedLocalReference
	if sa.Spec.Substrate != nil {
		workerPoolRef = sa.Spec.Substrate.WorkerPoolRef
	}
	wpKey, err := b.Lifecycle.resolveWorkerPoolRefFor(ctx, sa.Namespace, workerPoolRef)
	if err != nil {
		return nil, err
	}
	tmpl, err := b.Lifecycle.buildSandboxAgentActorTemplate(sa, wpKey, in.PodTemplate)
	if err != nil {
		return nil, err
	}
	return []client.Object{tmpl}, nil
}

// SessionDBURL returns the durable-dir session-store URL the translator bakes into the rendered
// config (AgentConfig.session_db_url) before building the config Secret. The value is
// runtime-specific: python's google-adk DatabaseSessionService uses SQLAlchemy's async engine,
// so the URL must name an async driver (aiosqlite, a core google-adk dependency); the Go ADK's
// local store parses either form.
func (b *AgentsBackend) SessionDBURL(agent v1alpha2.AgentObject) string {
	if v1alpha2.EffectiveDeclarativeRuntime(agent.GetAgentSpec()) == v1alpha2.DeclarativeRuntime_Go {
		return sessionDBURLGo
	}
	return sessionDBURLPython
}

func (b *AgentsBackend) ReconcileActorTemplate(ctx context.Context, desired client.Object) error {
	tmpl, ok := desired.(*atev1alpha1.ActorTemplate)
	if !ok {
		return fmt.Errorf("substrate sandbox backend cannot reconcile %T as an ActorTemplate", desired)
	}
	if b.Lifecycle == nil || b.Lifecycle.Client == nil {
		return fmt.Errorf("substrate lifecycle is not configured")
	}
	return reconcileActorTemplate(ctx, b.Lifecycle.Client, b.AteClient, tmpl)
}

func (b *AgentsBackend) ComputeReady(ctx context.Context, cl client.Client, nn types.NamespacedName) (metav1.ConditionStatus, string, string) {
	sa := &v1alpha2.SandboxAgent{}
	if err := cl.Get(ctx, nn, sa); err != nil {
		if apierrors.IsNotFound(err) {
			return metav1.ConditionUnknown, "SandboxAgentNotFound", err.Error()
		}
		return metav1.ConditionUnknown, "SandboxAgentGetFailed", err.Error()
	}
	if b.Lifecycle == nil {
		return metav1.ConditionUnknown, "SubstrateLifecycleNotConfigured", "substrate lifecycle is not configured"
	}
	tmpl, err := ResolveCurrentActorTemplate(ctx, cl, nn.Namespace, sa.Name)
	if err != nil {
		return metav1.ConditionUnknown, "ActorTemplateListFailed", err.Error()
	}
	if tmpl == nil {
		return metav1.ConditionFalse, "ActorTemplateNotFound", "ActorTemplate has not been generated yet"
	}
	if tmpl.Status.Phase != atev1alpha1.PhaseReady {
		return metav1.ConditionFalse, "ActorTemplateNotReady", "ActorTemplate golden snapshot is not ready"
	}
	return metav1.ConditionTrue, "ActorTemplateReady", "ActorTemplate golden snapshot is ready"
}
