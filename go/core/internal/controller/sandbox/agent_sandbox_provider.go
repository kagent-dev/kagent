package sandbox

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "sigs.k8s.io/agent-sandbox/api/v1alpha1"
	extv1alpha1 "sigs.k8s.io/agent-sandbox/extensions/api/v1alpha1"
)

const (
	// Label used to associate SandboxClaims with kagent sessions.
	labelSessionID = "kagent.dev/session-id"

	// Port exposed by the kagent-sandbox-mcp container in sandbox pods.
	defaultMCPPort = 8080

	// How often to re-check the SandboxClaim while waiting for it to become ready.
	pollInterval = time.Second / 4
)

// Terminal failure reasons on a SandboxClaim's Ready condition that indicate
// the sandbox will never become ready. When we see one of these we stop
// waiting and return an error immediately.
var terminalFailureReasons = map[string]bool{
	"TemplateNotFound": true,
	"ReconcilerError":  true,
	"SandboxExpired":   true,
	"ClaimExpired":     true,
}

// AgentSandboxProvider implements SandboxProvider using the
// kubernetes-sigs/agent-sandbox SandboxClaim CRD. It creates a SandboxClaim
// per session referencing the SandboxTemplate from the workspace ref, and
// derives the endpoint URL from the underlying Sandbox's ServiceFQDN.
//
// The agent-sandbox controller creates a Sandbox (and its Pod + headless
// Service) with the same name as the SandboxClaim. The Service FQDN is
// {name}.{namespace}.svc.cluster.local.
type AgentSandboxProvider struct {
	client client.Client
}

var _ SandboxProvider = (*AgentSandboxProvider)(nil)

// NewAgentSandboxProvider creates a provider that manages agent-sandbox SandboxClaims.
func NewAgentSandboxProvider(c client.Client) *AgentSandboxProvider {
	return &AgentSandboxProvider{client: c}
}

// claimName returns a deterministic name for the SandboxClaim for a session.
func claimName(sessionID string) string {
	name := fmt.Sprintf("kagent-%s", sessionID)
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

func (p *AgentSandboxProvider) GetOrCreate(ctx context.Context, opts CreateSandboxOptions) (*SandboxEndpoint, error) {
	ns := opts.Namespace
	if ns == "" {
		ns = opts.WorkspaceRef.Namespace
	}
	if ns == "" {
		return nil, fmt.Errorf("namespace is required for agent-sandbox provider")
	}

	name := claimName(opts.SessionID)
	key := types.NamespacedName{Name: name, Namespace: ns}

	// Try to get existing claim first.
	existing := &extv1alpha1.SandboxClaim{}
	err := p.client.Get(ctx, key, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get SandboxClaim %s/%s: %w", ns, name, err)
		}

		// Create the SandboxClaim.
		claim := p.buildClaim(name, ns, opts)
		if err := p.client.Create(ctx, claim); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return nil, fmt.Errorf("failed to create SandboxClaim: %w", err)
			}
			// Race — another caller created it first.
			if err := p.client.Get(ctx, key, existing); err != nil {
				return nil, fmt.Errorf("failed to get SandboxClaim after conflict: %w", err)
			}
		}
	}

	// Wait until the sandbox is ready or a terminal failure is detected.
	// The caller controls the deadline via ctx.
	return p.waitForReady(ctx, key)
}

// waitForReady uses wait.PollUntilContextCancel to periodically check the
// SandboxClaim until it is ready, a terminal failure is detected, or the
// context is cancelled/expired.
func (p *AgentSandboxProvider) waitForReady(ctx context.Context, key types.NamespacedName) (*SandboxEndpoint, error) {
	var result *SandboxEndpoint

	err := wait.PollUntilContextCancel(ctx, pollInterval, true, func(ctx context.Context) (bool, error) {
		claim := &extv1alpha1.SandboxClaim{}
		if err := p.client.Get(ctx, key, claim); err != nil {
			if apierrors.IsNotFound(err) {
				// Cache may not have synced yet after creation — retry.
				return false, nil
			}
			return false, fmt.Errorf("failed to get SandboxClaim %s: %w", key, err)
		}

		// Check for terminal failure on the Ready condition.
		if cond := meta.FindStatusCondition(claim.Status.Conditions, string(sandboxv1alpha1.SandboxConditionReady)); cond != nil {
			if cond.Status == metav1.ConditionFalse && terminalFailureReasons[cond.Reason] {
				return false, fmt.Errorf("sandbox %s failed: %s — %s", key.Name, cond.Reason, cond.Message)
			}
		}

		ep, err := p.endpointFromClaim(ctx, claim)
		if err != nil {
			return false, err
		}
		if ep.Ready {
			result = ep
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("timed out waiting for sandbox %s to become ready: %w", key.Name, ctx.Err())
		}
		return nil, err
	}

	return result, nil
}

func (p *AgentSandboxProvider) Get(ctx context.Context, sessionID string) (*SandboxEndpoint, error) {
	list := &extv1alpha1.SandboxClaimList{}
	if err := p.client.List(ctx, list, client.MatchingLabels{labelSessionID: sessionID}); err != nil {
		return nil, fmt.Errorf("failed to list SandboxClaims for session %s: %w", sessionID, err)
	}

	if len(list.Items) == 0 {
		return nil, nil
	}

	return p.endpointFromClaim(ctx, &list.Items[0])
}

func (p *AgentSandboxProvider) Destroy(ctx context.Context, sessionID string) error {
	list := &extv1alpha1.SandboxClaimList{}
	if err := p.client.List(ctx, list, client.MatchingLabels{labelSessionID: sessionID}); err != nil {
		return fmt.Errorf("failed to list SandboxClaims for session %s: %w", sessionID, err)
	}

	for i := range list.Items {
		if err := p.client.Delete(ctx, &list.Items[i]); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete SandboxClaim %s: %w", list.Items[i].Name, err)
		}
	}

	return nil
}

// buildClaim constructs a SandboxClaim referencing the workspace template.
func (p *AgentSandboxProvider) buildClaim(name, namespace string, opts CreateSandboxOptions) *extv1alpha1.SandboxClaim {
	return &extv1alpha1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				labelSessionID: opts.SessionID,
			},
			Annotations: map[string]string{
				"kagent.dev/agent-name": opts.AgentName,
				"kagent.dev/session-id": opts.SessionID,
			},
		},
		Spec: extv1alpha1.SandboxClaimSpec{
			TemplateRef: extv1alpha1.SandboxTemplateRef{
				Name: opts.WorkspaceRef.Name,
			},
		},
	}
}

// endpointFromClaim reads the SandboxClaim and its underlying Sandbox to
// build a SandboxEndpoint.
//
// The agent-sandbox controller creates a Sandbox with the same name as the
// claim. That Sandbox controller in turn creates a headless Service (also
// same name) and sets Sandbox.Status.ServiceFQDN once the Service exists and
// the Pod is ready.
func (p *AgentSandboxProvider) endpointFromClaim(ctx context.Context, claim *extv1alpha1.SandboxClaim) (*SandboxEndpoint, error) {
	ep := &SandboxEndpoint{
		ID:       claim.Name,
		Protocol: "streamable-http",
	}

	// Check if the claim's Ready condition is true.
	ready := meta.IsStatusConditionTrue(claim.Status.Conditions, string(sandboxv1alpha1.SandboxConditionReady))
	if !ready {
		return ep, nil
	}

	// The Sandbox has the same name as the claim. Look it up directly to
	// get the authoritative ServiceFQDN from its status.
	sb := &sandboxv1alpha1.Sandbox{}
	if err := p.client.Get(ctx, types.NamespacedName{Name: claim.Name, Namespace: claim.Namespace}, sb); err != nil {
		if apierrors.IsNotFound(err) {
			return ep, nil
		}
		return nil, fmt.Errorf("failed to get Sandbox %s/%s: %w", claim.Namespace, claim.Name, err)
	}

	if sb.Status.ServiceFQDN != "" {
		ep.MCPUrl = fmt.Sprintf("http://%s:%d/mcp", sb.Status.ServiceFQDN, defaultMCPPort)
		ep.Ready = true
	}

	return ep, nil
}
