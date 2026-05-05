package openshell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	openshellv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/openshellv1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

func defaultOpenclawAPIKeyEnvVar(provider v1alpha2.ModelProvider) string {
	return fmt.Sprintf("%s_API_KEY", strings.ToUpper(string(provider)))
}

const (
	defaultOpenclawGatewayPort      = 18800
	defaultOpenclawInferenceBaseURL = "https://inference.local/v1"
)

// OnSandboxReady starts openclaw gateway (background) and runs openclaw onboard when
// spec.modelConfigRef is set. No-ops when modelConfigRef is empty.
func (b *Backend) OnSandboxReady(ctx context.Context, sbx *v1alpha2.Sandbox, h sandboxbackend.Handle) error {
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

	apiKeyEnv := defaultOpenclawAPIKeyEnvVar(mc.Spec.Provider)
	gwPort := defaultOpenclawGatewayPort
	baseURL := defaultOpenclawInferenceBaseURL

	ctx, cancel := b.callCtx(ctx)
	defer cancel()
	ctx = withAuth(ctx, b.cfg.Token)

	execID, err := b.execSandboxID(ctx, h.ID)
	if err != nil {
		return fmt.Errorf("resolve sandbox exec id: %w", err)
	}

	// gateway run is long-running; start detached so this exec returns immediately.
	gatewayScript := fmt.Sprintf(
		`openclaw gateway run --port %d --allow-unconfigured --auth none >>/tmp/openclaw-gateway.log 2>&1 &`,
		gwPort,
	)
	gatewayCmd := []string{"sh", "-c", gatewayScript}
	code, stderr, err := b.execSandbox(ctx, execID, gatewayCmd, nil, 60)
	if err != nil {
		return fmt.Errorf("exec openclaw gateway run: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("openclaw gateway run: exit %d: %s", code, strings.TrimSpace(stderr))
	}

	customAPIKey := fmt.Sprintf("openshell:resolve:env:%s", apiKeyEnv)
	onboard := []string{
		"openclaw", "onboard",
		"--non-interactive",
		"--accept-risk",
		"--auth-choice", "custom-api-key",
		"--custom-base-url", baseURL,
		"--custom-api-key", customAPIKey,
		"--custom-model-id", providerRecord,
		"--gateway-port", strconv.Itoa(gwPort),
		"--skip-health",
	}
	code, stderr, err = b.execSandbox(ctx, execID, onboard, nil, 600)
	if err != nil {
		return fmt.Errorf("exec openclaw onboard: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("openclaw onboard: exit %d: %s", code, strings.TrimSpace(stderr))
	}

	ctrllog.FromContext(ctx).Info("openclaw bootstrap completed",
		"sandbox", sbx.Namespace+"/"+sbx.Name, "providerRecord", providerRecord)
	return nil
}

func (b *Backend) execSandbox(ctx context.Context, sandboxID string, command []string, stdin []byte, timeoutSec uint32) (int32, string, error) {
	osCli := b.openShell()
	if osCli == nil {
		return -1, "", fmt.Errorf("openshell client is nil")
	}
	stream, err := osCli.ExecSandbox(ctx, &openshellv1.ExecSandboxRequest{
		SandboxId:      sandboxID,
		Command:        command,
		Stdin:          stdin,
		TimeoutSeconds: timeoutSec,
	})
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
