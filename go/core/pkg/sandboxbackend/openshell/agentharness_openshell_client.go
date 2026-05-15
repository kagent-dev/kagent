package openshell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	openshellv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/openshellv1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// AgentHarnessOpenShellClient performs OpenShell gRPC operations for AgentHarness
// lifecycle (sandbox create/get/delete/exec). It wraps *OpenShellClients from Dial
// (client.go) together with Config and optional events: per-RPC timeouts, bearer
// auth on the context, and helpers that map responses to sandboxbackend types.
// It does not run backend-specific pre-create work (e.g. translateModelConfig);
// concrete backends compose findExistingSandbox + their own translation step +
// createSandbox in their own EnsureAgentHarness implementation.
//
// Named with an AgentHarness prefix to avoid confusion with OpenShellClients (dial bundle) and
// openshellv1.OpenShellClient (generated gRPC interface).
type AgentHarnessOpenShellClient struct {
	clients  *OpenShellClients
	cfg      Config
	recorder record.EventRecorder
}

func newAgentHarnessOpenShellClient(clients *OpenShellClients, cfg Config, recorder record.EventRecorder) *AgentHarnessOpenShellClient {
	return &AgentHarnessOpenShellClient{
		clients:  clients,
		cfg:      cfg,
		recorder: recorder,
	}
}

func (c *AgentHarnessOpenShellClient) openShell() openshellv1.OpenShellClient {
	if c.clients == nil {
		return nil
	}
	return c.clients.OpenShell
}

// CallCtx applies CallTimeout from Config when positive.
func (c *AgentHarnessOpenShellClient) CallCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.cfg.CallTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, c.cfg.CallTimeout)
}

func (c *AgentHarnessOpenShellClient) warnUnsupportedAgentHarnessFields(ctx context.Context, ah *v1alpha2.AgentHarness, fields []string) {
	if len(fields) == 0 {
		return
	}
	msg := fmt.Sprintf("OpenShell backend ignored unsupported AgentHarness fields: %v", fields)
	if c.recorder != nil && ah != nil {
		c.recorder.Event(ah, "Warning", "OpenshellUnsupportedField", msg)
		return
	}
	ctrllog.FromContext(ctx).Info(msg, "agentHarness", ah.Namespace+"/"+ah.Name)
}

// CreateAgentHarnessSandbox runs CreateSandbox for an AgentHarness after idempotency has been checked upstream.
func (c *AgentHarnessOpenShellClient) CreateAgentHarnessSandbox(
	ctx context.Context,
	ah *v1alpha2.AgentHarness,
	req *openshellv1.CreateSandboxRequest,
	unsupported []string,
) (sandboxbackend.EnsureResult, error) {
	if ah == nil {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("AgentHarness is required")
	}
	ctx, cancel := c.CallCtx(ctx)
	defer cancel()
	ctx = withAuth(ctx, c.cfg.Token)

	osCli := c.openShell()
	if osCli == nil {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("openshell: OpenShell client is required (use Dial or non-nil OpenShellClients.OpenShell)")
	}

	name := agentHarnessGatewayName(ah)
	c.warnUnsupportedAgentHarnessFields(ctx, ah, unsupported)

	createResp, err := osCli.CreateSandbox(ctx, req)
	if err != nil {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("openshell CreateSandbox %s: %w", name, err)
	}
	if createResp.GetSandbox() == nil {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("openshell CreateSandbox %s: %w", name, ErrEmptyResponse)
	}
	handleID := sandboxBackendHandleID(createResp.GetSandbox())
	return sandboxbackend.EnsureResult{
		Handle:   sandboxbackend.Handle{ID: handleID},
		Endpoint: endpointFor(c.cfg.GatewayURL, handleID),
	}, nil
}

// GetSandboxStatus maps OpenShell sandbox phase to Ready condition pieces for AgentHarness status.
func (c *AgentHarnessOpenShellClient) GetSandboxStatus(ctx context.Context, h sandboxbackend.Handle) (metav1.ConditionStatus, string, string) {
	if h.ID == "" {
		return metav1.ConditionUnknown, "SandboxHandleMissing", "no openshell sandbox handle recorded yet"
	}
	ctx, cancel := c.CallCtx(ctx)
	defer cancel()
	ctx = withAuth(ctx, c.cfg.Token)

	osCli := c.openShell()
	if osCli == nil {
		return metav1.ConditionUnknown, "OpenShellClientMissing", "openshell OpenShell gRPC client is not configured"
	}

	resp, err := osCli.GetSandbox(ctx, &openshellv1.GetSandboxRequest{Name: h.ID})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return metav1.ConditionUnknown, "SandboxNotFound", fmt.Sprintf("openshell sandbox %q not found", h.ID)
		}
		return metav1.ConditionUnknown, "SandboxGetFailed", err.Error()
	}
	return phaseToCondition(resp.GetSandbox())
}

// DeleteAgentHarnessSandbox deletes the OpenShell sandbox; NotFound is success.
func (c *AgentHarnessOpenShellClient) DeleteAgentHarnessSandbox(ctx context.Context, h sandboxbackend.Handle) error {
	if h.ID == "" {
		return nil
	}
	ctx, cancel := c.CallCtx(ctx)
	defer cancel()
	ctx = withAuth(ctx, c.cfg.Token)

	osCli := c.openShell()
	if osCli == nil {
		return fmt.Errorf("openshell: OpenShell client is required")
	}

	_, err := osCli.DeleteSandbox(ctx, &openshellv1.DeleteSandboxRequest{Name: h.ID})
	if err == nil {
		return nil
	}
	if status.Code(err) == codes.NotFound {
		return nil
	}
	return fmt.Errorf("openshell DeleteSandbox %s: %w", h.ID, err)
}

// ExecSandboxID resolves metadata.id for ExecSandbox RPCs.
func (c *AgentHarnessOpenShellClient) ExecSandboxID(ctx context.Context, sandboxHandleName string) (string, error) {
	name := strings.TrimSpace(sandboxHandleName)
	if name == "" {
		return "", fmt.Errorf("sandbox handle name is empty")
	}
	osCli := c.openShell()
	if osCli == nil {
		return "", fmt.Errorf("openshell client is nil")
	}
	resp, err := osCli.GetSandbox(ctx, &openshellv1.GetSandboxRequest{Name: name})
	if err != nil {
		return "", fmt.Errorf("GetSandbox %q for exec sandbox_id: %w", name, err)
	}
	sb := resp.GetSandbox()
	if sb == nil || sb.GetMetadata() == nil {
		return "", fmt.Errorf("GetSandbox %q: empty sandbox", name)
	}
	id := strings.TrimSpace(sb.GetMetadata().GetId())
	if id != "" {
		return id, nil
	}
	return name, nil
}

// ExecSandbox runs a command inside the sandbox via OpenShell ExecSandbox streaming RPC.
func (c *AgentHarnessOpenShellClient) ExecSandbox(ctx context.Context, sandboxID string, command []string, stdin []byte, env map[string]string, timeoutSec uint32) (int32, string, error) {
	osCli := c.openShell()
	if osCli == nil {
		return -1, "", fmt.Errorf("openshell client is nil")
	}
	req := &openshellv1.ExecSandboxRequest{
		SandboxId:      sandboxID,
		Command:        command,
		Stdin:          stdin,
		TimeoutSeconds: timeoutSec,
	}
	if len(env) > 0 {
		req.Environment = env
	}
	stream, err := osCli.ExecSandbox(ctx, req)
	if err != nil {
		return -1, "", err
	}
	var stderr strings.Builder
	var exitCode int32 = -1
	for {
		ev, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return exitCode, stderr.String(), err
		}
		switch p := ev.GetPayload().(type) {
		case *openshellv1.ExecSandboxEvent_Stdout:
		case *openshellv1.ExecSandboxEvent_Stderr:
			if p.Stderr != nil {
				stderr.Write(p.Stderr.GetData())
			}
		case *openshellv1.ExecSandboxEvent_Exit:
			if p.Exit != nil {
				exitCode = p.Exit.GetExitCode()
			}
		}
	}
	if exitCode == -1 {
		return exitCode, stderr.String(), fmt.Errorf("ExecSandbox finished without exit status")
	}
	return exitCode, stderr.String(), nil
}

// ErrEmptyResponse is returned when OpenShell returns success with an empty Sandbox payload.
var ErrEmptyResponse = errors.New("openshell: empty sandbox in response")
