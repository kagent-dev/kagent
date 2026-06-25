package substrate

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	"strings"
	"text/template"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openclaw"
	corev1 "k8s.io/api/core/v1"
)

// OpenClawGatewayPort is the loopback port the OpenClaw gateway listens on
// inside a substrate actor. It is a private implementation detail: the
// in-sandbox `openclaw acp` child connects to it over loopback, while the
// acp-shim owns the atenet ingress port (acpListenPort). kagent never reaches
// the gateway directly.
const OpenClawGatewayPort = 18789

// acpListenPort is the actor port atenet-router routes Host-based traffic to.
const acpListenPort = 80

//go:embed templates/openclaw_startup.sh.tmpl
var openClawStartupScriptTmplContent string

var openClawStartupScriptTmpl = template.Must(template.New("openclaw_startup").Parse(openClawStartupScriptTmplContent))

type openClawStartupScriptData struct {
	OpenClawJSONBase64 string
	ACPPort            int
}

// buildOpenClawActorStartup returns the ateom workload startup script and container env for OpenClaw on Substrate.
// When spec.modelConfigRef is set, openclaw.json includes models/agents/channels.
func (p *Lifecycle) buildOpenClawActorStartup(ctx context.Context, ah *v1alpha2.AgentHarness) (script string, env []atev1alpha1.EnvVar, err error) {
	if ah == nil {
		return "", nil, fmt.Errorf("AgentHarness is required")
	}
	if p.Client == nil {
		return "", nil, fmt.Errorf("substrate lifecycle kubernetes client is required")
	}

	gw := openclaw.SubstrateGatewayBootstrap(OpenClawGatewayPort)

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
		containerEnv = []corev1.EnvVar{{Name: "HOME", Value: openclaw.SubstrateActorHome}}
	}
	containerEnv = append(containerEnv, acpShimEnv(ah, gw.Port)...)
	script, err = openClawStartupScript(jsonBytes)
	if err != nil {
		return "", nil, err
	}
	return script, actorTemplateEnvFromPodEnv(containerEnv), nil
}

// acpShimEnv returns the env vars the image's
// openclaw-gateway-ensure.sh/openclaw-acp-child.sh scripts read. The shim no
// longer authenticates the WebSocket handshake, so no bearer token is passed.
func acpShimEnv(ah *v1alpha2.AgentHarness, gatewayPort int) []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "OPENCLAW_GATEWAY_PORT", Value: fmt.Sprintf("%d", gatewayPort)},
	}
}

func openClawStartupScript(jsonBytes []byte) (string, error) {
	var buf bytes.Buffer
	if err := openClawStartupScriptTmpl.Execute(&buf, openClawStartupScriptData{
		OpenClawJSONBase64: base64.StdEncoding.EncodeToString(jsonBytes),
		ACPPort:            acpListenPort,
	}); err != nil {
		return "", fmt.Errorf("render openclaw startup script: %w", err)
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}
