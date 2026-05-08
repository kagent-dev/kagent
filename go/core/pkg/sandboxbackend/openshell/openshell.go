// Package openshell implements sandboxbackend.AsyncBackend against an external
// OpenShell gateway over gRPC.
//
// Use Dial to obtain OpenShellClients (shared connection for openshell.v1.OpenShell
// and openshell.inference.v1.Inference).
//
// • NewOpenShellBackend — generic AgentHarness resources with spec.backend=openshell:
// user image/env mapping per translate.go; spec.modelConfigRef is ignored for pre-create and bootstrap.
//
// • NewOpenClawBackend — pin the sandbox image to NemoclawSandboxBaseImage, translateModelConfig
// when modelConfigRef is set, run OpenClaw bootstrap after Ready. The same instance
// is registered for spec.backend=openclaw and nemoclaw (see app wiring).
//
// Unlike agentsxk8s, these backends do not emit Kubernetes workload objects —
// sandbox lifecycle goes through the gateway over gRPC.
package openshell

import (
	"context"
	"fmt"

	openshellv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/openshellv1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PreCreateSandboxFunc is an optional hook: runs after GetSandbox returns NotFound and before
// CreateSandbox on the OpenShell-backed control plane. Each AgentHarness backend passes nil or a
// backend-specific function (e.g. OpenClaw uses translateModelConfig to apply spec.modelConfigRef; a
// future Hermes backend would pass its own implementation). A separate interface would be a single-method
// duplicate of this func type, so the hook stays a function.
type PreCreateSandboxFunc func(ctx context.Context, ah *v1alpha2.AgentHarness, kube client.Client, oc *OpenShellClients) error

type agentHarnessOpenShellBackend struct {
	*AgentHarnessOpenShellClient
	kubeClient       client.Client
	backendName      v1alpha2.AgentHarnessBackendType
	buildCreate      func(*v1alpha2.AgentHarness) (*openshellv1.CreateSandboxRequest, []string)
	preCreateSandbox PreCreateSandboxFunc
}

func newAgentHarnessOpenShellBackend(
	kubeClient client.Client,
	clients *OpenShellClients,
	cfg Config,
	recorder record.EventRecorder,
	name v1alpha2.AgentHarnessBackendType,
	build func(*v1alpha2.AgentHarness) (*openshellv1.CreateSandboxRequest, []string),
	preCreateSandbox PreCreateSandboxFunc,
) *agentHarnessOpenShellBackend {
	return &agentHarnessOpenShellBackend{
		AgentHarnessOpenShellClient: newAgentHarnessOpenShellClient(clients, cfg, recorder),
		kubeClient:                  kubeClient,
		backendName:                 name,
		buildCreate:                 build,
		preCreateSandbox:            preCreateSandbox,
	}
}

// Name implements AsyncBackend.
func (b *agentHarnessOpenShellBackend) Name() v1alpha2.AgentHarnessBackendType {
	return b.backendName
}

// EnsureAgentHarness implements AsyncBackend. Idempotent: GetSandbox short-circuits before CreateSandbox.
func (b *agentHarnessOpenShellBackend) EnsureAgentHarness(ctx context.Context, ah *v1alpha2.AgentHarness) (sandboxbackend.EnsureResult, error) {
	if ah == nil {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("AgentHarness is required")
	}

	ctx, cancel := b.CallCtx(ctx)
	defer cancel()
	ctx = withAuth(ctx, b.cfg.Token)

	osCli := b.openShell()
	if osCli == nil {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("openshell: OpenShell client is required (use Dial or non-nil OpenShellClients.OpenShell)")
	}

	name := agentHarnessGatewayName(ah)

	getResp, err := osCli.GetSandbox(ctx, &openshellv1.GetSandboxRequest{Name: name})
	if err == nil && getResp != nil && getResp.GetSandbox() != nil {
		handleID := sandboxBackendHandleID(getResp.GetSandbox())
		return sandboxbackend.EnsureResult{
			Handle:   sandboxbackend.Handle{ID: handleID},
			Endpoint: endpointFor(b.cfg.GatewayURL, handleID),
		}, nil
	}
	if err != nil && status.Code(err) != codes.NotFound {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("openshell GetSandbox %s: %w", name, err)
	}

	if b.preCreateSandbox != nil {
		if err := b.preCreateSandbox(ctx, ah, b.kubeClient, b.clients); err != nil {
			return sandboxbackend.EnsureResult{}, err
		}
	}

	req, unsupported := b.buildCreate(ah)
	return b.CreateAgentHarnessSandbox(ctx, ah, req, unsupported)
}

// GetStatus implements AsyncBackend.
func (b *agentHarnessOpenShellBackend) GetStatus(ctx context.Context, h sandboxbackend.Handle) (metav1.ConditionStatus, string, string) {
	return b.GetSandboxStatus(ctx, h)
}

// DeleteAgentHarness implements AsyncBackend.
func (b *agentHarnessOpenShellBackend) DeleteAgentHarness(ctx context.Context, h sandboxbackend.Handle) error {
	return b.DeleteAgentHarnessSandbox(ctx, h)
}

// OpenShellBackend implements AsyncBackend for spec.backend=openshell (generic
// OpenShell sandbox provisioning without OpenClaw/Nemo bootstrap behavior).
type OpenShellBackend struct {
	*agentHarnessOpenShellBackend
}

var _ sandboxbackend.AsyncBackend = (*OpenShellBackend)(nil)

// NewOpenShellBackend returns a backend for AgentHarness.spec.backend=openshell.
func NewOpenShellBackend(kubeClient client.Client, clients *OpenShellClients, cfg Config, recorder record.EventRecorder) *OpenShellBackend {
	return &OpenShellBackend{
		agentHarnessOpenShellBackend: newAgentHarnessOpenShellBackend(
			kubeClient, clients, cfg, recorder,
			v1alpha2.AgentHarnessBackendOpenshell,
			buildAgentHarnessOpenshellCreateRequest,
			nil,
		),
	}
}
