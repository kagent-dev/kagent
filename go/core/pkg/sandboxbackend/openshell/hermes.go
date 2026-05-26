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
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/hermes"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// HermesBackend implements AsyncBackend and PostReadyBackend for Hermes AgentHarness resources.
type HermesBackend struct {
	*agentHarnessOpenShellBackend
}

var _ sandboxbackend.AsyncBackend = (*HermesBackend)(nil)

// NewHermesBackend returns the Hermes harness backend.
func NewHermesBackend(kubeClient client.Client, clients *OpenShellClients, cfg Config, recorder record.EventRecorder) *HermesBackend {
	return &HermesBackend{
		agentHarnessOpenShellBackend: newAgentHarnessOpenShellBackend(
			kubeClient, clients, cfg, recorder,
			v1alpha2.AgentHarnessBackendHermes,
		),
	}
}

// EnsureAgentHarness syncs ModelConfig then creates the Hermes sandbox.
func (b *HermesBackend) EnsureAgentHarness(ctx context.Context, ah *v1alpha2.AgentHarness) (sandboxbackend.EnsureResult, error) {
	if ah == nil {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("AgentHarness is required")
	}
	ctx, cancel := b.CallCtx(ctx)
	defer cancel()
	ctx = withAuth(ctx, b.cfg.Token)

	if res, found, err := b.findExistingSandbox(ctx, ah); err != nil || found {
		return res, err
	}
	return b.ensureAgentHarnessSandbox(ctx, ah, buildHermesCreateRequest)
}

// OnAgentHarnessReady writes ~/.hermes/config.yaml and .env, updates the config hash, and starts the gateway.
func (b *HermesBackend) OnAgentHarnessReady(ctx context.Context, ah *v1alpha2.AgentHarness, h sandboxbackend.Handle) error {
	ref := strings.TrimSpace(ah.Spec.ModelConfigRef)
	if ref == "" {
		return nil
	}
	if h.ID == "" {
		return fmt.Errorf("sandbox backend handle id is empty")
	}
	if b.kubeClient == nil {
		return fmt.Errorf("kubernetes client is required for hermes bootstrap")
	}

	modelConfigRef, err := utils.ParseRefString(ref, ah.Namespace)
	if err != nil {
		return fmt.Errorf("parse modelConfigRef: %w", err)
	}
	mc := &v1alpha2.ModelConfig{}
	if err := b.kubeClient.Get(ctx, modelConfigRef, mc); err != nil {
		return fmt.Errorf("get ModelConfig: %w", err)
	}

	if _, err := UpsertMessagingProviders(ctx, b.clients, b.kubeClient, ah); err != nil {
		return fmt.Errorf("upsert messaging providers: %w", err)
	}

	configYAML, envFile, execEnv, err := hermes.BuildBootstrapArtifacts(ctx, b.kubeClient, ah.Namespace, ah, mc)
	if err != nil {
		return fmt.Errorf("build hermes config: %w", err)
	}

	token := b.cfg.Token
	idCtx, cancelID := b.CallCtx(ctx)
	defer cancelID()
	execID, err := b.ExecSandboxID(withAuth(idCtx, token), h.ID)
	if err != nil {
		return fmt.Errorf("resolve sandbox exec id: %w", err)
	}

	mkdirScript := fmt.Sprintf(`mkdir -p %s`, hermes.HermesConfigDir)
	installCtx, cancelInstall := context.WithTimeout(ctx, 120*time.Second+15*time.Second)
	defer cancelInstall()
	code, stderr, err := b.ExecSandbox(withAuth(installCtx, token), execID, []string{"sh", "-c", mkdirScript}, nil, execEnv, 30)
	if err != nil {
		return fmt.Errorf("mkdir hermes config dir: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("mkdir hermes config dir: exit %d: %s", code, strings.TrimSpace(stderr))
	}

	configInstall := []string{"sh", "-c", fmt.Sprintf(`cat > %s/config.yaml`, hermes.HermesConfigDir)}
	code, stderr, err = b.ExecSandbox(withAuth(installCtx, token), execID, configInstall, configYAML, execEnv, 60)
	if err != nil {
		return fmt.Errorf("install hermes config.yaml: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("install hermes config.yaml: exit %d: %s", code, strings.TrimSpace(stderr))
	}

	envInstall := []string{"sh", "-c", fmt.Sprintf(`cat > %s/.env`, hermes.HermesConfigDir)}
	code, stderr, err = b.ExecSandbox(withAuth(installCtx, token), execID, envInstall, envFile, execEnv, 60)
	if err != nil {
		return fmt.Errorf("install hermes .env: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("install hermes .env: exit %d: %s", code, strings.TrimSpace(stderr))
	}

	hashScript := fmt.Sprintf(
		`mkdir -p /etc/nemoclaw && sha256sum %s/config.yaml %s/.env > %s && chmod 444 %s 2>/dev/null || true`,
		hermes.HermesConfigDir, hermes.HermesConfigDir, hermes.HermesConfigHashFile, hermes.HermesConfigHashFile,
	)
	hashCtx, cancelHash := context.WithTimeout(ctx, 30*time.Second)
	defer cancelHash()
	code, stderr, err = b.ExecSandbox(withAuth(hashCtx, token), execID, []string{"sh", "-c", hashScript}, nil, execEnv, 30)
	if err != nil {
		return fmt.Errorf("write hermes config hash: %w", err)
	}
	if code != 0 {
		ctrllog.FromContext(ctx).Info("hermes config hash write skipped (non-fatal)", "stderr", strings.TrimSpace(stderr))
	}

	gwCtx, cancelGW := context.WithTimeout(ctx, 90*time.Second+15*time.Second)
	defer cancelGW()
	gatewayStart := fmt.Sprintf(
		`HERMES_HOME=%s nohup hermes gateway run >>/tmp/gateway.log 2>&1 &`,
		hermes.HermesConfigDir,
	)
	code, stderr, err = b.ExecSandbox(withAuth(gwCtx, token), execID, []string{"sh", "-c", gatewayStart}, nil, execEnv, 30)
	if err != nil {
		return fmt.Errorf("start hermes gateway: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("start hermes gateway: exit %d: %s", code, strings.TrimSpace(stderr))
	}

	waitGateway := hermes.GatewayListenWaitScript(hermes.HermesInternalGatewayPort)
	code, stderr, err = b.ExecSandbox(withAuth(gwCtx, token), execID, []string{"sh", "-c", waitGateway}, nil, execEnv, 45)
	if err != nil {
		return fmt.Errorf("wait for hermes gateway listen: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("wait for hermes gateway listen: exit %d: %s", code, strings.TrimSpace(stderr))
	}

	socatStart := fmt.Sprintf(
		`command -v socat >/dev/null 2>&1 && nohup socat TCP-LISTEN:%d,bind=0.0.0.0,fork,reuseaddr TCP:127.0.0.1:%d >>/tmp/socat.log 2>&1 &`,
		hermes.HermesPublicGatewayPort,
		hermes.HermesInternalGatewayPort,
	)
	code, stderr, err = b.ExecSandbox(withAuth(gwCtx, token), execID, []string{"sh", "-c", socatStart}, nil, execEnv, 30)
	if err != nil {
		return fmt.Errorf("start hermes socat forwarder: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("start hermes socat forwarder: exit %d: %s", code, strings.TrimSpace(stderr))
	}

	ctrllog.FromContext(ctx).Info("hermes bootstrap completed", "agentHarness", ah.Namespace+"/"+ah.Name)
	return nil
}

func buildHermesCreateRequest(ah *v1alpha2.AgentHarness, messagingProviders []string) (*openshellv1.CreateSandboxRequest, []string) {
	req, unsupported := buildAgentHarnessOpenshellCreateRequest(ah)
	if req.GetSpec().GetTemplate() == nil {
		req.Spec.Template = &openshellv1.SandboxTemplate{}
	}
	if ah.Spec.Image == "" {
		req.Spec.Template.Image = hermes.HermesSandboxBaseImage
	}
	attachMessagingProviders(req, messagingProviders)
	return req, unsupported
}
