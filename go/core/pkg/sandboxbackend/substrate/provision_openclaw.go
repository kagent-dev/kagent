package substrate

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openclaw"
	corev1 "k8s.io/api/core/v1"
)

const defaultSubstrateOpenClawGatewayPort = 80

// buildOpenClawActorStartup returns the ateom workload startup script and container env for OpenClaw on Substrate.
// When spec.modelConfigRef is set, openclaw.json includes models/agents/channels like the OpenShell bootstrap path.
func (p *Provisioner) buildOpenClawActorStartup(ctx context.Context, ah *v1alpha2.AgentHarness) (script string, env []corev1.EnvVar, err error) {
	if ah == nil {
		return "", nil, fmt.Errorf("AgentHarness is required")
	}
	if p.Client == nil {
		return "", nil, fmt.Errorf("substrate provisioner kubernetes client is required")
	}

	token, err := ResolveGatewayToken(ctx, p.Client, ah)
	if err != nil {
		return "", nil, fmt.Errorf("resolve gateway token: %w", err)
	}
	gw := openclaw.SubstrateGatewayBootstrap(token, defaultSubstrateOpenClawGatewayPort, openClawControlUIBasePath(ah))

	var jsonBytes []byte
	var containerEnv []corev1.EnvVar

	ref := strings.TrimSpace(ah.Spec.ModelConfigRef)
	if ref != "" {
		mcRef, parseErr := utils.ParseRefString(ref, ah.Namespace)
		if parseErr != nil {
			return "", nil, fmt.Errorf("parse modelConfigRef %q: %w", ref, parseErr)
		}
		mc := &v1alpha2.ModelConfig{}
		if getErr := p.Client.Get(ctx, mcRef, mc); getErr != nil {
			return "", nil, fmt.Errorf("get ModelConfig %s: %w", mcRef, getErr)
		}
		jsonBytes, containerEnv, err = openclaw.BuildSubstrateBootstrapJSON(ctx, p.Client, ah.Namespace, ah, mc, gw)
		if err != nil {
			return "", nil, fmt.Errorf("build openclaw bootstrap json: %w", err)
		}
	} else {
		jsonBytes, err = openclaw.BuildGatewayOnlyBootstrapJSON(gw)
		if err != nil {
			return "", nil, fmt.Errorf("build gateway-only openclaw json: %w", err)
		}
		containerEnv = []corev1.EnvVar{{Name: "HOME", Value: "/root"}}
	}
	script = openClawStartupScript(jsonBytes, gw.Port)
	return script, containerEnv, nil
}

func openClawControlUIBasePath(ah *v1alpha2.AgentHarness) string {
	if ah == nil {
		return ""
	}
	return "/api/agentharnesses/" + ah.Namespace + "/" + ah.Name + "/gateway"
}

func openClawStartupScript(jsonBytes []byte, gwPort int) string {
	b64 := base64.StdEncoding.EncodeToString(jsonBytes)
	return strings.Join([]string{
		"set -e",
		`mkdir -p "${HOME}/.openclaw"`,
		fmt.Sprintf(`echo '%s' | base64 -d > "${HOME}/.openclaw/openclaw.json"`, b64),
		fmt.Sprintf("openclaw gateway run --port %d --allow-unconfigured >>/tmp/openclaw-gateway.log 2>&1 &", gwPort),
		`for i in $(seq 1 60); do`,
		`  curl -sf http://127.0.0.1:80/ >/dev/null 2>&1 && echo "gateway up" && break`,
		"  sleep 1",
		"done",
		"tail -f /tmp/openclaw-gateway.log /dev/null",
	}, "\n")
}
