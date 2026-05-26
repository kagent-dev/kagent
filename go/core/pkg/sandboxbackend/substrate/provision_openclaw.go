package substrate

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/openclaw"
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
	gw := openclaw.SubstrateGatewayBootstrap(token, defaultSubstrateOpenClawGatewayPort)

	var jsonBytes []byte
	var envMap map[string]string

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
		jsonBytes, envMap, err = openclaw.BuildBootstrapJSON(ctx, p.Client, ah.Namespace, ah, mc, gw, openclaw.SubstrateBootstrapDefaultBaseURL)
		if err != nil {
			return "", nil, fmt.Errorf("build openclaw bootstrap json: %w", err)
		}
	} else {
		jsonBytes, err = openclaw.BuildGatewayOnlyBootstrapJSON(gw)
		if err != nil {
			return "", nil, fmt.Errorf("build gateway-only openclaw json: %w", err)
		}
		envMap = map[string]string{}
	}

	containerEnv := openClawEnvVars(envMap)
	script = openClawStartupScript(jsonBytes, gw.Port)
	return script, containerEnv, nil
}

func openClawEnvVars(envMap map[string]string) []corev1.EnvVar {
	keys := make([]string, 0, len(envMap))
	for k := range envMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]corev1.EnvVar, 0, len(keys)+1)
	for _, k := range keys {
		out = append(out, corev1.EnvVar{Name: k, Value: envMap[k]})
	}
	out = append(out, corev1.EnvVar{Name: "HOME", Value: "/root"})
	return out
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
