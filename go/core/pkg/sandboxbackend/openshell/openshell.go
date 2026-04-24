// Package openshell implements a sandboxbackend.AsyncBackend backing the
// kagent.dev/v1alpha2 Sandbox CRD against an external OpenShell gateway.
//
// Unlike agentsxk8s (which emits agents.x-k8s.io Sandbox CRs for SandboxAgent
// workloads), this backend does not emit any Kubernetes resources — all
// sandbox lifecycle goes through the gateway over gRPC: EnsureSandbox calls
// CreateSandbox (idempotent via a prior GetSandbox), DeleteSandbox calls
// DeleteSandbox, GetStatus maps SandboxPhase onto a Ready condition.
package openshell

import (
	"context"
	"errors"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	openshellv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/openshellv1"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const backendName = v1alpha2.SandboxBackendOpenshell

// Backend is the gRPC-backed openshell implementation of
// sandboxbackend.AsyncBackend.
type Backend struct {
	client   openshellv1.OpenShellClient
	cfg      Config
	recorder record.EventRecorder
}

var _ sandboxbackend.AsyncBackend = (*Backend)(nil)

// New returns a Backend using the provided gRPC client. Obtain one via Dial.
// recorder is optional; if nil, unsupported-field warnings go to the logger
// but no Events are emitted.
func New(c openshellv1.OpenShellClient, cfg Config, recorder record.EventRecorder) *Backend {
	return &Backend{client: c, cfg: cfg, recorder: recorder}
}

// Name implements AsyncBackend.
func (b *Backend) Name() v1alpha2.SandboxBackendType { return backendName }

// EnsureSandbox implements AsyncBackend. Idempotent: a prior GetSandbox
// short-circuits the CreateSandbox when the gateway already has it.
func (b *Backend) EnsureSandbox(ctx context.Context, sbx *v1alpha2.Sandbox) (sandboxbackend.EnsureResult, error) {
	if sbx == nil {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("sandbox is required")
	}
	ctx, cancel := b.callCtx(ctx)
	defer cancel()
	ctx = withAuth(ctx, b.cfg.Token)

	name := sandboxName(sbx)

	getResp, err := b.client.GetSandbox(ctx, &openshellv1.GetSandboxRequest{Name: name})
	if err == nil && getResp != nil && getResp.GetSandbox() != nil {
		id := getResp.GetSandbox().GetName()
		return sandboxbackend.EnsureResult{
			Handle:   sandboxbackend.Handle{ID: id},
			Endpoint: endpointFor(b.cfg.GatewayURL, id),
		}, nil
	}
	if err != nil && status.Code(err) != codes.NotFound {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("openshell GetSandbox %s: %w", name, err)
	}

	req, unsupported := buildCreateRequest(sbx)
	b.warnUnsupported(ctx, sbx, unsupported)

	createResp, err := b.client.CreateSandbox(ctx, req)
	if err != nil {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("openshell CreateSandbox %s: %w", name, err)
	}
	if createResp.GetSandbox() == nil {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("openshell CreateSandbox %s: %w", name, ErrEmptyResponse)
	}
	id := createResp.GetSandbox().GetName()
	return sandboxbackend.EnsureResult{
		Handle:   sandboxbackend.Handle{ID: id},
		Endpoint: endpointFor(b.cfg.GatewayURL, id),
	}, nil
}

// GetStatus implements AsyncBackend.
func (b *Backend) GetStatus(ctx context.Context, h sandboxbackend.Handle) (metav1.ConditionStatus, string, string) {
	if h.ID == "" {
		return metav1.ConditionUnknown, "SandboxHandleMissing", "no openshell sandbox handle recorded yet"
	}
	ctx, cancel := b.callCtx(ctx)
	defer cancel()
	ctx = withAuth(ctx, b.cfg.Token)

	resp, err := b.client.GetSandbox(ctx, &openshellv1.GetSandboxRequest{Name: h.ID})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return metav1.ConditionUnknown, "SandboxNotFound", fmt.Sprintf("openshell sandbox %q not found", h.ID)
		}
		return metav1.ConditionUnknown, "SandboxGetFailed", err.Error()
	}
	return phaseToCondition(resp.GetSandbox())
}

// DeleteSandbox implements AsyncBackend. NotFound is success.
func (b *Backend) DeleteSandbox(ctx context.Context, h sandboxbackend.Handle) error {
	if h.ID == "" {
		return nil
	}
	ctx, cancel := b.callCtx(ctx)
	defer cancel()
	ctx = withAuth(ctx, b.cfg.Token)

	_, err := b.client.DeleteSandbox(ctx, &openshellv1.DeleteSandboxRequest{Name: h.ID})
	if err == nil {
		return nil
	}
	if status.Code(err) == codes.NotFound {
		return nil
	}
	return fmt.Errorf("openshell DeleteSandbox %s: %w", h.ID, err)
}

func (b *Backend) callCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if b.cfg.CallTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, b.cfg.CallTimeout)
}

func (b *Backend) warnUnsupported(ctx context.Context, sbx *v1alpha2.Sandbox, fields []string) {
	if len(fields) == 0 {
		return
	}
	msg := fmt.Sprintf("OpenShell backend ignored unsupported Sandbox fields: %v", fields)
	if b.recorder != nil && sbx != nil {
		b.recorder.Event(sbx, "Warning", "OpenshellUnsupportedField", msg)
		return
	}
	ctrllog.FromContext(ctx).Info(msg, "sandbox", sbx.Namespace+"/"+sbx.Name)
}

// ErrEmptyResponse is returned when the gateway returns success with an empty
// Sandbox payload. Exposed for tests to assert.
var ErrEmptyResponse = errors.New("openshell: empty sandbox in response")
