// Package openshell implements a sandboxbackend.AsyncBackend backing the
// kagent.dev/v1alpha2 Sandbox CRD against an external OpenShell gateway.
//
// Use Dial to obtain OpenShellClients (shared connection for openshell.v1.OpenShell
// and openshell.inference.v1.Inference), then New(kubeClient, clients, cfg, recorder).
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
	"strings"

	"github.com/kagent-dev/kagent/go/api/openshell/gen/datamodelv1"
	"github.com/kagent-dev/kagent/go/api/openshell/gen/inferencev1"
	openshellv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/openshellv1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const backendName = v1alpha2.SandboxBackendOpenshell

// Backend is the gRPC-backed openshell implementation of
// sandboxbackend.AsyncBackend.
type Backend struct {
	kubeClient client.Client
	clients    *OpenShellClients
	cfg        Config
	recorder   record.EventRecorder
}

var (
	_ sandboxbackend.AsyncBackend     = (*Backend)(nil)
	_ sandboxbackend.PostReadyBackend = (*Backend)(nil)
)

// New returns a Backend using gateway clients from Dial (OpenShellClients).
// clients.OpenShell must be non-nil; clients.Inference may be nil when
// spec.modelConfigRef is never used. recorder is optional; if nil,
// unsupported-field warnings go to the logger but no Events are emitted.
func New(kubeClient client.Client, clients *OpenShellClients, cfg Config, recorder record.EventRecorder) *Backend {
	return &Backend{kubeClient: kubeClient, clients: clients, cfg: cfg, recorder: recorder}
}

func (b *Backend) openShell() openshellv1.OpenShellClient {
	if b.clients == nil {
		return nil
	}
	return b.clients.OpenShell
}

func (b *Backend) inference() inferencev1.InferenceClient {
	if b.clients == nil {
		return nil
	}
	return b.clients.Inference
}

// Name implements AsyncBackend.
func (b *Backend) Name() v1alpha2.SandboxBackendType { return backendName }

// sandboxBackendHandleID is ObjectMeta.name — the canonical lookup key for
// GetSandbox / DeleteSandbox (same string as CreateSandboxRequest.Name).
func sandboxBackendHandleID(sb *openshellv1.Sandbox) string {
	if sb == nil || sb.GetMetadata() == nil {
		return ""
	}
	return strings.TrimSpace(sb.GetMetadata().GetName())
}

// execSandboxID resolves metadata.id for ExecSandbox and similar RPCs.
// BackendRef.ID on the Sandbox CR stores the gateway name (GetSandbox/DeleteSandbox);
// ExecSandboxRequest.sandbox_id is the stable object id per openshell.proto.
func (b *Backend) execSandboxID(ctx context.Context, gatewaySandboxName string) (string, error) {
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

func gatewayProviderRecordName(provider v1alpha2.ModelProvider) string {
	return strings.ToLower(string(provider))
}

func resolveModelConfigAPIKey(ctx context.Context, kube client.Client, mc *v1alpha2.ModelConfig) (string, error) {
	if mc.Spec.APIKeyPassthrough {
		return "", fmt.Errorf("APIKeyPassthrough is not supported when registering an OpenShell gateway provider from ModelConfig")
	}
	if mc.Spec.APIKeySecret == "" || mc.Spec.APIKeySecretKey == "" {
		return "", fmt.Errorf("modelConfig %s/%s requires apiKeySecret and apiKeySecretKey", mc.Namespace, mc.Name)
	}
	sec := &corev1.Secret{}
	key := types.NamespacedName{Namespace: mc.Namespace, Name: mc.Spec.APIKeySecret}
	if err := kube.Get(ctx, key, sec); err != nil {
		return "", fmt.Errorf("get API key secret %q: %w", mc.Spec.APIKeySecret, err)
	}
	raw, ok := sec.Data[mc.Spec.APIKeySecretKey]
	if !ok || len(raw) == 0 {
		return "", fmt.Errorf("secret %q missing non-empty key %q", mc.Spec.APIKeySecret, mc.Spec.APIKeySecretKey)
	}
	return string(raw), nil
}

func (b *Backend) applyClusterInference(ctx context.Context, providerRecordName, model string) error {
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
func (b *Backend) ensureGatewayFromModelConfig(ctx context.Context, sbx *v1alpha2.Sandbox, osCli openshellv1.OpenShellClient) error {
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
	apiKey, err := resolveModelConfigAPIKey(ctx, b.kubeClient, modelConfig)
	if err != nil {
		return fmt.Errorf("openshell gateway provider: %w", err)
	}

	providerRecordName := gatewayProviderRecordName(modelConfig.Spec.Provider)
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

// EnsureSandbox implements AsyncBackend. Idempotent: a prior GetSandbox
// short-circuits the CreateSandbox when the gateway already has it.
func (b *Backend) EnsureSandbox(ctx context.Context, sbx *v1alpha2.Sandbox) (sandboxbackend.EnsureResult, error) {
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

	if err := b.ensureGatewayFromModelConfig(ctx, sbx, osCli); err != nil {
		return sandboxbackend.EnsureResult{}, err
	}

	req, unsupported := buildCreateRequest(sbx)
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
func (b *Backend) GetStatus(ctx context.Context, h sandboxbackend.Handle) (metav1.ConditionStatus, string, string) {
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

// DeleteSandbox implements AsyncBackend. NotFound is success.
func (b *Backend) DeleteSandbox(ctx context.Context, h sandboxbackend.Handle) error {
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
