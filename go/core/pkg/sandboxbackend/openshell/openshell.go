// Package openshell implements sandboxbackend.AsyncBackend against an external
// OpenShell gateway over gRPC.
//
// Use Dial to obtain OpenShellClients (shared connection for openshell.v1.OpenShell
// and openshell.inference.v1.Inference).
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

type agentHarnessOpenShellBackend struct {
	*AgentHarnessOpenShellClient
	kubeClient  client.Client
	backendName v1alpha2.AgentHarnessBackendType
	buildCreate func(*v1alpha2.AgentHarness) (*openshellv1.CreateSandboxRequest, []string)
}

func newAgentHarnessOpenShellBackend(
	kubeClient client.Client,
	clients *OpenShellClients,
	cfg Config,
	recorder record.EventRecorder,
	name v1alpha2.AgentHarnessBackendType,
	build func(*v1alpha2.AgentHarness) (*openshellv1.CreateSandboxRequest, []string),
) *agentHarnessOpenShellBackend {
	return &agentHarnessOpenShellBackend{
		AgentHarnessOpenShellClient: newAgentHarnessOpenShellClient(clients, cfg, recorder),
		kubeClient:                  kubeClient,
		backendName:                 name,
		buildCreate:                 build,
	}
}

// Name implements AsyncBackend.
func (b *agentHarnessOpenShellBackend) Name() v1alpha2.AgentHarnessBackendType {
	return b.backendName
}

// findExistingSandbox is the GetSandbox/NotFound idempotency probe shared across backends. It
// returns (res, true, nil) when the gateway already has a sandbox under this AgentHarness's
// deterministic name, (zero, false, nil) when the sandbox does not yet exist, or
// (zero, false, err) on any other gateway error. Callers must already have applied CallCtx and
// withAuth to ctx.
func (b *agentHarnessOpenShellBackend) findExistingSandbox(ctx context.Context, ah *v1alpha2.AgentHarness) (sandboxbackend.EnsureResult, bool, error) {
	osCli := b.openShell()
	if osCli == nil {
		return sandboxbackend.EnsureResult{}, false, fmt.Errorf("openshell: OpenShell client is required (use Dial or non-nil OpenShellClients.OpenShell)")
	}
	name := agentHarnessGatewayName(ah)
	getResp, err := osCli.GetSandbox(ctx, &openshellv1.GetSandboxRequest{Name: name})
	if err == nil && getResp != nil && getResp.GetSandbox() != nil {
		handleID := sandboxBackendHandleID(getResp.GetSandbox())
		return sandboxbackend.EnsureResult{
			Handle:   sandboxbackend.Handle{ID: handleID},
			Endpoint: endpointFor(b.cfg.GatewayURL, handleID),
		}, true, nil
	}
	if err != nil && status.Code(err) != codes.NotFound {
		return sandboxbackend.EnsureResult{}, false, fmt.Errorf("openshell GetSandbox %s: %w", name, err)
	}
	return sandboxbackend.EnsureResult{}, false, nil
}

// createSandbox builds the gateway create request via b.buildCreate and submits it. Callers must
// already have applied CallCtx and withAuth to ctx and confirmed via findExistingSandbox that no
// sandbox exists yet.
func (b *agentHarnessOpenShellBackend) createSandbox(ctx context.Context, ah *v1alpha2.AgentHarness) (sandboxbackend.EnsureResult, error) {
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
