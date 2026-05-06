package openshell

import (
	"context"
	"fmt"
	"strings"
	"time"

	openshellv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/openshellv1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// NemoclawSandboxBaseImage is the container image used for OpenClaw and NemoClaw
const NemoclawSandboxBaseImage = "ghcr.io/nvidia/nemoclaw/sandbox-base:latest"

// ClawBackend implements AsyncBackend and PostReadyBackend for OpenClaw- and
// NemoClaw-typed Sandbox resources: gateway provider registration, fixed sandbox
// image, and post-ready OpenClaw bootstrap when modelConfigRef is set.
type ClawBackend struct {
	*grpcBackend
}

var (
	_ sandboxbackend.AsyncBackend     = (*ClawBackend)(nil)
	_ sandboxbackend.PostReadyBackend = (*ClawBackend)(nil)
)

func newClawBackend(
	kubeClient client.Client,
	clients *OpenShellClients,
	cfg Config,
	recorder record.EventRecorder,
	name v1alpha2.SandboxBackendType,
) (*ClawBackend, error) {
	if name != v1alpha2.SandboxBackendOpenClaw && name != v1alpha2.SandboxBackendNemoClaw {
		return nil, fmt.Errorf("openshell: claw backend type must be openclaw or nemoclaw, got %q", name)
	}
	return &ClawBackend{
		grpcBackend: newGRPCBackend(kubeClient, clients, cfg, recorder, name, buildClawCreateRequest, true),
	}, nil
}

func defaultOpenclawAPIKeyEnvVar(provider v1alpha2.ModelProvider) string {
	return fmt.Sprintf("%s_API_KEY", strings.ToUpper(string(provider)))
}

const (
	defaultOpenclawGatewayPort      = 18800
	defaultOpenclawInferenceBaseURL = "https://inference.local/v1"
)

// OnSandboxReady writes ~/.openclaw/openclaw.json from ModelConfig and spec.channels,
// then runs `openclaw gateway start` in the background with injected env (API key + channel secrets).
// No-ops when modelConfigRef is empty.
func (b *ClawBackend) OnSandboxReady(ctx context.Context, sbx *v1alpha2.Sandbox, h sandboxbackend.Handle) error {
	ref := strings.TrimSpace(sbx.Spec.ModelConfigRef)
	if ref == "" {
		return nil
	}
	if h.ID == "" {
		return fmt.Errorf("sandbox backend handle id is empty")
	}
	if b.kubeClient == nil {
		return fmt.Errorf("kubernetes client is required for openclaw bootstrap")
	}

	modelConfigRef, err := utils.ParseRefString(ref, sbx.Namespace)
	if err != nil {
		return fmt.Errorf("parse modelConfigRef: %w", err)
	}
	mc := &v1alpha2.ModelConfig{}
	if err := b.kubeClient.Get(ctx, modelConfigRef, mc); err != nil {
		return fmt.Errorf("get ModelConfig: %w", err)
	}

	providerRecord := gatewayProviderRecordName(mc.Spec.Provider)
	gwPort := defaultOpenclawGatewayPort
	token := b.cfg.Token

	jsonBytes, env, err := buildOpenClawBootstrapJSON(ctx, b.kubeClient, sbx.Namespace, sbx, mc, gwPort)
	if err != nil {
		return fmt.Errorf("build openclaw config: %w", err)
	}

	idCtx, cancelID := b.callCtx(ctx)
	defer cancelID()
	execID, err := b.execSandboxID(withAuth(idCtx, token), h.ID)
	if err != nil {
		return fmt.Errorf("resolve sandbox exec id: %w", err)
	}

	if sandboxHasChannelType(sbx, v1alpha2.SandboxChannelTypeDiscord) {
		dCtx, cancelDiscord := context.WithTimeout(ctx, 120*time.Second+15*time.Second)
		code, stderr, errExec := b.execSandbox(withAuth(dCtx, token), execID, []string{"sh", "-c", "openclaw plugins enable discord"}, nil, nil, 120)
		cancelDiscord()
		if errExec != nil {
			return fmt.Errorf("exec openclaw plugins enable discord: %w", errExec)
		}
		if code != 0 {
			return fmt.Errorf("openclaw plugins enable discord: exit %d: %s", code, strings.TrimSpace(stderr))
		}
	}

	installCmd := []string{"sh", "-c", `mkdir -p "$HOME/.openclaw" && cat > "$HOME/.openclaw/openclaw.json"`}
	installCtx, cancelInstall := context.WithTimeout(ctx, 120*time.Second+15*time.Second)
	defer cancelInstall()
	code, stderr, err := b.execSandbox(withAuth(installCtx, token), execID, installCmd, jsonBytes, env, 120)
	if err != nil {
		return fmt.Errorf("install openclaw.json: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("install openclaw.json: exit %d: %s", code, strings.TrimSpace(stderr))
	}

	gatewayScript := fmt.Sprintf(
		`openclaw gateway run --port %d >>/tmp/openclaw-gateway.log 2>&1 &`,
		gwPort,
	)
	gatewayCmd := []string{"sh", "-c", gatewayScript}
	gwCtx, cancelGW := context.WithTimeout(ctx, 90*time.Second+15*time.Second)
	defer cancelGW()
	code, stderr, err = b.execSandbox(withAuth(gwCtx, token), execID, gatewayCmd, nil, env, 90)
	if err != nil {
		return fmt.Errorf("exec openclaw gateway run: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("openclaw gateway run: exit %d: %s", code, strings.TrimSpace(stderr))
	}

	ctrllog.FromContext(ctx).Info("openclaw bootstrap completed",
		"sandbox", sbx.Namespace+"/"+sbx.Name, "providerRecord", providerRecord)
	return nil
}

func buildClawCreateRequest(sbx *v1alpha2.Sandbox) (*openshellv1.CreateSandboxRequest, []string) {
	req, unsupported := buildOpenshellCreateRequest(sbx)
	if req.GetSpec().GetTemplate() == nil {
		req.Spec.Template = &openshellv1.SandboxTemplate{}
	}

	// If the user has set an image, use it. Otherwise, use our nemoclaw image.
	if sbx.Spec.Image == "" {
		req.Spec.Template.Image = NemoclawSandboxBaseImage
	}
	return req, unsupported
}

// NewOpenClawBackend returns a backend for Sandbox.spec.backend=openclaw.
func NewOpenClawBackend(kubeClient client.Client, clients *OpenShellClients, cfg Config, recorder record.EventRecorder) (*ClawBackend, error) {
	return newClawBackend(kubeClient, clients, cfg, recorder, v1alpha2.SandboxBackendOpenClaw)
}
