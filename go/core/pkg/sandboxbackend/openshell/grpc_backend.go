package openshell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/kagent-dev/kagent/go/api/openshell/gen/datamodelv1"
	"github.com/kagent-dev/kagent/go/api/openshell/gen/inferencev1"
	openshellv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/openshellv1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/openclaw"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// grpcBackend holds shared OpenShell gateway client logic for sandbox lifecycle.
// OpenshellBackend and ClawBackend embed it with different create-request builders
// and optional gateway prep (model config / inference) before CreateSandbox.
type grpcBackend struct {
	kubeClient client.Client
	clients    *OpenShellClients
	cfg        Config
	recorder   record.EventRecorder

	backendName               v1alpha2.AgentHarnessBackendType
	buildCreate               func(*v1alpha2.AgentHarness) (*openshellv1.CreateSandboxRequest, []string)
	ensureGatewayBeforeCreate bool
}

func newGRPCBackend(
	kubeClient client.Client,
	clients *OpenShellClients,
	cfg Config,
	recorder record.EventRecorder,
	name v1alpha2.AgentHarnessBackendType,
	build func(*v1alpha2.AgentHarness) (*openshellv1.CreateSandboxRequest, []string),
	ensureGatewayBeforeCreate bool,
) *grpcBackend {
	return &grpcBackend{
		kubeClient:                kubeClient,
		clients:                   clients,
		cfg:                       cfg,
		recorder:                  recorder,
		backendName:               name,
		buildCreate:               build,
		ensureGatewayBeforeCreate: ensureGatewayBeforeCreate,
	}
}

func (b *grpcBackend) openShell() openshellv1.OpenShellClient {
	if b.clients == nil {
		return nil
	}
	return b.clients.OpenShell
}

func (b *grpcBackend) inference() inferencev1.InferenceClient {
	if b.clients == nil {
		return nil
	}
	return b.clients.Inference
}

// Name implements AsyncBackend.
func (b *grpcBackend) Name() v1alpha2.AgentHarnessBackendType { return b.backendName }

func (b *grpcBackend) applyClusterInference(ctx context.Context, providerRecordName, model string) error {
	if _, err := b.inference().SetClusterInference(ctx, &inferencev1.SetClusterInferenceRequest{
		ProviderName: providerRecordName,
		ModelId:      model,
		NoVerify:     true,
	}); err != nil {
		return fmt.Errorf("SetClusterInference: %w", err)
	}
	return nil
}

// ensureGatewayFromModelConfig loads the referenced ModelConfig, registers a matching
// gateway provider, and applies SetClusterInference. It is a no-op when spec.modelConfigRef is empty.
func (b *grpcBackend) ensureGatewayFromModelConfig(ctx context.Context, sbx *v1alpha2.AgentHarness, osCli openshellv1.OpenShellClient) error {
	ref := strings.TrimSpace(sbx.Spec.ModelConfigRef)
	if ref == "" {
		return nil
	}
	if b.kubeClient == nil {
		return fmt.Errorf("openshell: Kubernetes client is required when spec.modelConfigRef is set")
	}
	if b.inference() == nil {
		return fmt.Errorf("openshell: inference client is required when spec.modelConfigRef is set")
	}

	modelConfigRef, err := utils.ParseRefString(ref, sbx.Namespace)
	if err != nil {
		return fmt.Errorf("failed to parse ModelConfigRef %s: %w", ref, err)
	}

	modelConfig := &v1alpha2.ModelConfig{}
	if err := b.kubeClient.Get(ctx, modelConfigRef, modelConfig); err != nil {
		return fmt.Errorf("failed to get ModelConfig %s: %w", modelConfigRef.String(), err)
	}
	apiKey, err := openclaw.ResolveModelConfigAPIKey(ctx, b.kubeClient, modelConfig)
	if err != nil {
		return fmt.Errorf("openshell gateway provider: %w", err)
	}

	providerRecordName := openclaw.GatewayProviderRecordName(modelConfig.Spec.Provider)
	model := modelConfig.Spec.Model

	getProviderResp, err := osCli.GetProvider(ctx, &openshellv1.GetProviderRequest{Name: providerRecordName})
	exists := false
	if err != nil {
		if status.Code(err) != codes.NotFound {
			return fmt.Errorf("GetProvider %s: %w", providerRecordName, err)
		}
	} else if getProviderResp.GetProvider() != nil {
		exists = true
	}

	providerProto := &datamodelv1.Provider{
		Metadata: &datamodelv1.ObjectMeta{Name: providerRecordName},
		Type:     providerRecordName,
		Credentials: map[string]string{
			"apiKey": apiKey,
		},
	}

	if exists {
		if _, err := osCli.UpdateProvider(ctx, &openshellv1.UpdateProviderRequest{Provider: providerProto}); err != nil {
			return fmt.Errorf("UpdateProvider %s: %w", providerRecordName, err)
		}
		ctrllog.FromContext(ctx).Info("updated gateway provider", "name", providerRecordName)
	} else {
		if _, err := osCli.CreateProvider(ctx, &openshellv1.CreateProviderRequest{Provider: providerProto}); err != nil {
			return fmt.Errorf("CreateProvider %s: %w", providerRecordName, err)
		}
		ctrllog.FromContext(ctx).Info("created gateway provider", "name", providerRecordName)
	}

	if err := b.applyClusterInference(ctx, providerRecordName, model); err != nil {
		return fmt.Errorf("cluster inference for model %s: %w", model, err)
	}
	ctrllog.FromContext(ctx).Info("set cluster inference", "provider", providerRecordName, "model", model)
	return nil
}

// EnsureAgentHarness implements AsyncBackend. Idempotent: a prior GetSandbox
// short-circuits the CreateSandbox when the gateway already has it.
func (b *grpcBackend) EnsureAgentHarness(ctx context.Context, sbx *v1alpha2.AgentHarness) (sandboxbackend.EnsureResult, error) {
	if sbx == nil {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("sandbox is required")
	}
	ctx, cancel := b.callCtx(ctx)
	defer cancel()
	ctx = withAuth(ctx, b.cfg.Token)

	osCli := b.openShell()
	if osCli == nil {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("openshell: OpenShell client is required (use Dial or non-nil OpenShellClients.OpenShell)")
	}

	name := sandboxName(sbx)

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

	if b.ensureGatewayBeforeCreate {
		if err := b.ensureGatewayFromModelConfig(ctx, sbx, osCli); err != nil {
			return sandboxbackend.EnsureResult{}, err
		}
	}

	req, unsupported := b.buildCreate(sbx)
	b.warnUnsupported(ctx, sbx, unsupported)

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
		Endpoint: endpointFor(b.cfg.GatewayURL, handleID),
	}, nil
}

// GetStatus implements AsyncBackend.
func (b *grpcBackend) GetStatus(ctx context.Context, h sandboxbackend.Handle) (metav1.ConditionStatus, string, string) {
	if h.ID == "" {
		return metav1.ConditionUnknown, "SandboxHandleMissing", "no openshell sandbox handle recorded yet"
	}
	ctx, cancel := b.callCtx(ctx)
	defer cancel()
	ctx = withAuth(ctx, b.cfg.Token)

	osCli := b.openShell()
	if osCli == nil {
		return metav1.ConditionUnknown, "OpenShellClientMissing", "openshell gateway client is not configured"
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

// DeleteAgentHarness implements AsyncBackend. NotFound is success.
func (b *grpcBackend) DeleteAgentHarness(ctx context.Context, h sandboxbackend.Handle) error {
	if h.ID == "" {
		return nil
	}
	ctx, cancel := b.callCtx(ctx)
	defer cancel()
	ctx = withAuth(ctx, b.cfg.Token)

	osCli := b.openShell()
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

func (b *grpcBackend) callCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if b.cfg.CallTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, b.cfg.CallTimeout)
}

func (b *grpcBackend) warnUnsupported(ctx context.Context, sbx *v1alpha2.AgentHarness, fields []string) {
	if len(fields) == 0 {
		return
	}
	msg := fmt.Sprintf("OpenShell backend ignored unsupported AgentHarness fields: %v", fields)
	if b.recorder != nil && sbx != nil {
		b.recorder.Event(sbx, "Warning", "OpenshellUnsupportedField", msg)
		return
	}
	ctrllog.FromContext(ctx).Info(msg, "sandbox", sbx.Namespace+"/"+sbx.Name)
}

// ErrEmptyResponse is returned when the gateway returns success with an empty
// Sandbox payload. Exposed for tests to assert.
var ErrEmptyResponse = errors.New("openshell: empty sandbox in response")

// execSandboxID resolves metadata.id for ExecSandbox and similar RPCs.
// BackendRef.ID on the AgentHarness CR stores the gateway name (GetSandbox/DeleteSandbox);
// ExecSandboxRequest.sandbox_id is the stable object id per openshell.proto.
func (b *grpcBackend) execSandboxID(ctx context.Context, gatewaySandboxName string) (string, error) {
	name := strings.TrimSpace(gatewaySandboxName)
	if name == "" {
		return "", fmt.Errorf("gateway sandbox name is empty")
	}
	osCli := b.openShell()
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

func (b *grpcBackend) execSandbox(ctx context.Context, sandboxID string, command []string, stdin []byte, env map[string]string, timeoutSec uint32) (int32, string, error) {
	osCli := b.openShell()
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
