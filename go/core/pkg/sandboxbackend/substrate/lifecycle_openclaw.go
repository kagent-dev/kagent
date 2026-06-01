package substrate

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	"strings"
	"text/template"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openclaw"
	corev1 "k8s.io/api/core/v1"
)

const defaultSubstrateOpenClawGatewayPort = 80

//go:embed templates/openclaw_startup.sh.tmpl
var openClawStartupScriptTmplContent string

var openClawStartupScriptTmpl = template.Must(template.New("openclaw_startup").Parse(openClawStartupScriptTmplContent))

type openClawStartupScriptData struct {
	OpenClawJSONBase64 string
	GatewayPort        int
}

// buildOpenClawActorStartup returns the ateom workload startup script and container env for OpenClaw on Substrate.
// When spec.modelConfigRef is set, openclaw.json includes models/agents/channels like the OpenShell bootstrap path.
func (p *Lifecycle) buildOpenClawActorStartup(ctx context.Context, ah *v1alpha2.AgentHarness) (script string, env []corev1.EnvVar, err error) {
	if ah == nil {
		return "", nil, fmt.Errorf("AgentHarness is required")
	}
	if p.Client == nil {
		return "", nil, fmt.Errorf("substrate lifecycle kubernetes client is required")
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
	script, err = openClawStartupScript(jsonBytes, gw.Port)
	if err != nil {
		return "", nil, err
	}
	return script, containerEnv, nil
}

func openClawControlUIBasePath(ah *v1alpha2.AgentHarness) string {
	if ah == nil {
		return ""
	}
	return "/api/agentharnesses/" + ah.Namespace + "/" + ah.Name + "/gateway"
}

func openClawStartupScript(jsonBytes []byte, gwPort int) (string, error) {
	var buf bytes.Buffer
	if err := openClawStartupScriptTmpl.Execute(&buf, openClawStartupScriptData{
		OpenClawJSONBase64: base64.StdEncoding.EncodeToString(jsonBytes),
		GatewayPort:        gwPort,
	}); err != nil {
		return "", fmt.Errorf("render openclaw startup script: %w", err)
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}
