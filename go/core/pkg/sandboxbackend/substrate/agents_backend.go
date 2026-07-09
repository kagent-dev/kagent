package substrate

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	corev1 "k8s.io/api/core/v1"
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

	// Publish the rendered config under the agent's STABLE Secret name, updated in place on
	// every config change (the ActorTemplate references it via secretKeyRef env, re-resolved by
	// ate-api at each Data-scope resume — that's how soft config rollouts reach existing
	// actors). The Secret is owner-referenced to the SandboxAgent, so it is GC'd with the agent.
	configSecret, err := buildSandboxAgentConfigSecret(sa, in)
	if err != nil {
		return nil, err
	}
	if configSecret != nil {
		return []client.Object{configSecret, tmpl}, nil
	}
	return []client.Object{tmpl}, nil
}

// buildSandboxAgentConfigSecret copies the rendered config Secret under the agent's stable
// Secret name, adding AgentConfig.session_db_url for durable-dir agents so the runtime learns
// its session store from the config struct. Returns nil when there is no rendered config.
func buildSandboxAgentConfigSecret(sa *v1alpha2.SandboxAgent, in sandboxbackend.BuildInput) (*corev1.Secret, error) {
	if in.ConfigSecret == nil {
		return nil, nil
	}
	data := in.ConfigSecret.Data
	stringData := in.ConfigSecret.StringData
	if SandboxAgentUsesDurableDirSessions(sa) {
		dbURL := sandboxAgentSessionDBURL(sa)
		if len(data["config.json"]) > 0 {
			patched, err := withSessionDBURL(data["config.json"], dbURL)
			if err != nil {
				return nil, err
			}
			data = maps.Clone(data)
			data["config.json"] = patched
		} else {
			// Covers StringData and an empty/absent rendering alike (withSessionDBURL treats
			// empty as {}): a durable-dir agent must never ship a config without its store URL.
			patched, err := withSessionDBURL([]byte(stringData["config.json"]), dbURL)
			if err != nil {
				return nil, err
			}
			if stringData == nil {
				stringData = map[string]string{}
			} else {
				stringData = maps.Clone(stringData)
			}
			stringData["config.json"] = string(patched)
		}
	}
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      sandboxAgentConfigSecretName(sa),
			Namespace: sa.Namespace,
			Labels:    sandboxAgentLifecycleLabels(sa),
		},
		Type:       in.ConfigSecret.Type,
		Data:       data,
		StringData: stringData,
	}, nil
}

// withSessionDBURL sets session_db_url in the rendered config JSON. A generic map round-trip
// (not adk.AgentConfig) so fields this package does not know about survive unchanged.
func withSessionDBURL(configJSON []byte, dbURL string) ([]byte, error) {
	if len(configJSON) == 0 {
		configJSON = []byte("{}")
	}
	var cfg map[string]any
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return nil, fmt.Errorf("parse rendered config.json to set session_db_url: %w", err)
	}
	cfg["session_db_url"] = dbURL
	return json.Marshal(cfg)
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
