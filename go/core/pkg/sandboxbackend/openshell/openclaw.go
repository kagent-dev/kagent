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
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/openclaw"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// NemoclawSandboxBaseImage is the container image used for OpenClaw/NemoClaw harness sandboxes.
const NemoclawSandboxBaseImage = "ghcr.io/kagent-dev/nemoclaw/sandbox-base:2026.5.4"

// ClawBackend implements AsyncBackend and PostReadyBackend for OpenClaw- and
// NemoClaw-typed AgentHarness resources: sync ModelConfig to the OpenShell control plane before create,
// fixed sandbox image, and post-ready OpenClaw bootstrap when modelConfigRef is set.
type ClawBackend struct {
	*agentHarnessOpenShellBackend
}

var _ sandboxbackend.AsyncBackend = (*ClawBackend)(nil)

// NewOpenClawBackend returns the shared OpenClaw/NemoClaw harness backend. Register the same
// instance under AgentHarnessBackendOpenClaw and AgentHarnessBackendNemoClaw; the controller
// records status.backendRef.backend from spec.backend so both types stay distinguishable.
func NewOpenClawBackend(kubeClient client.Client, clients *OpenShellClients, cfg Config, recorder record.EventRecorder) *ClawBackend {
	return &ClawBackend{
		agentHarnessOpenShellBackend: newAgentHarnessOpenShellBackend(
			kubeClient, clients, cfg, recorder,
			v1alpha2.AgentHarnessBackendOpenClaw,
			buildClawCreateRequest,
		),
	}
}

// EnsureAgentHarness is the OpenClaw/NemoClaw EnsureAgentHarness flow: idempotent gateway lookup,
// then translateModelConfig (apply ModelConfigRef onto the gateway) before CreateSandbox.
// Future backends (e.g. Hermes) follow the same pattern: findExistingSandbox + own translation step
// + createSandbox, with no callback fields.
func (b *ClawBackend) EnsureAgentHarness(ctx context.Context, ah *v1alpha2.AgentHarness) (sandboxbackend.EnsureResult, error) {
	if ah == nil {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("AgentHarness is required")
	}
	ctx, cancel := b.CallCtx(ctx)
	defer cancel()
	ctx = withAuth(ctx, b.cfg.Token)

	if res, found, err := b.findExistingSandbox(ctx, ah); err != nil || found {
		return res, err
	}
	if err := translateModelConfig(ctx, ah, b.kubeClient, b.clients); err != nil {
		return sandboxbackend.EnsureResult{}, err
	}
	return b.createSandbox(ctx, ah)
}

const defaultOpenclawGatewayPort = 18800

// OnAgentHarnessReady writes ~/.openclaw/openclaw.json from ModelConfig and spec.channels,
// then runs `openclaw gateway start` in the background with injected env (API key + channel secrets).
// No-ops when modelConfigRef is empty.
func (b *ClawBackend) OnAgentHarnessReady(ctx context.Context, ah *v1alpha2.AgentHarness, h sandboxbackend.Handle) error {
	ref := strings.TrimSpace(ah.Spec.ModelConfigRef)
	if ref == "" {
		return nil
	}
	if h.ID == "" {
		return fmt.Errorf("sandbox backend handle id is empty")
	}
	if b.kubeClient == nil {
		return fmt.Errorf("kubernetes client is required for openclaw bootstrap")
	}

	modelConfigRef, err := utils.ParseRefString(ref, ah.Namespace)
	if err != nil {
		return fmt.Errorf("parse modelConfigRef: %w", err)
	}
	mc := &v1alpha2.ModelConfig{}
	if err := b.kubeClient.Get(ctx, modelConfigRef, mc); err != nil {
		return fmt.Errorf("get ModelConfig: %w", err)
	}

	providerRecord := openclaw.GatewayProviderRecordName(mc.Spec.Provider)
	gwPort := defaultOpenclawGatewayPort
	token := b.cfg.Token

	jsonBytes, env, err := openclaw.BuildBootstrapJSON(ctx, b.kubeClient, ah.Namespace, ah, mc, gwPort)
	if err != nil {
		return fmt.Errorf("build openclaw config: %w", err)
	}

	idCtx, cancelID := b.CallCtx(ctx)
	defer cancelID()
	execID, err := b.ExecSandboxID(withAuth(idCtx, token), h.ID)
	if err != nil {
		return fmt.Errorf("resolve sandbox exec id: %w", err)
	}

	installCmd := []string{"sh", "-c", `mkdir -p "$HOME/.openclaw" && cat > "$HOME/.openclaw/openclaw.json"`}
	installCtx, cancelInstall := context.WithTimeout(ctx, 120*time.Second+15*time.Second)
	defer cancelInstall()
	code, stderr, err := b.ExecSandbox(withAuth(installCtx, token), execID, installCmd, jsonBytes, env, 120)
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
	code, stderr, err = b.ExecSandbox(withAuth(gwCtx, token), execID, gatewayCmd, nil, env, 90)
	if err != nil {
		return fmt.Errorf("exec openclaw gateway run: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("openclaw gateway run: exit %d: %s", code, strings.TrimSpace(stderr))
	}

	ctrllog.FromContext(ctx).Info("openclaw bootstrap completed",
		"agentHarness", ah.Namespace+"/"+ah.Name, "providerRecord", providerRecord)
	return nil
}

func buildClawCreateRequest(ah *v1alpha2.AgentHarness) (*openshellv1.CreateSandboxRequest, []string) {
	req, unsupported := buildAgentHarnessOpenshellCreateRequest(ah)
	if req.GetSpec().GetTemplate() == nil {
		req.Spec.Template = &openshellv1.SandboxTemplate{}
	}

	if ah.Spec.Image == "" {
		req.Spec.Template.Image = NemoclawSandboxBaseImage
	}
	return req, unsupported
}
