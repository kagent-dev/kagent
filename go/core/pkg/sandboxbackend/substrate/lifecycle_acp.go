package substrate

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openclaw"
	corev1 "k8s.io/api/core/v1"
)

// Default Substrate workload images for the generic acp-shim agent targets live
// in constants.go (acpSandboxHermesImage, etc.).

// acpAgentSpec describes how to run one stdio ACP agent behind the acp-shim
// inside a Substrate actor.
type acpAgentSpec struct {
	// DefaultImage resolves the digest-pinned acp-sandbox target image used when
	// neither the harness nor cluster defaults specify a workload image. It
	// composes the ref from the runtime registry/repository and errors when the
	// link-time digest was not injected.
	DefaultImage func(acpSandboxImageConfig) (string, error)
	// ChildCommand is the stdio ACP agent command the shim spawns
	// (shell-safe words, joined with spaces).
	ChildCommand []string
}

// acpAgentSpecs maps non-OpenClaw substrate backends to their agent commands.
//
// The acp-shim keeps a single long-lived child across bridge reconnects. This
// matters for Hermes: its ACP SessionManager keeps live sessions (history,
// cwd, model, cancel event) in memory inside the running `hermes acp`
// process, so a single child must persist across reconnects (page refreshes)
// so list/load/resume/fork stay scoped to that process and find the prior
// conversation. (See the hermes target in docker/acp-sandbox/Dockerfile.)
//
// Session history is also durable: Hermes persists ACP sessions to
// ~/.hermes/state.db, which lives on the actor's home dir and therefore
// survives a Substrate checkpoint/restore, and Hermes transparently restores
// those sessions across process restarts. The long-lived child is about live
// in-memory fidelity and avoiding a per-reconnect reload, not about durability.
var acpAgentSpecs = map[v1alpha2.AgentHarnessBackendType]acpAgentSpec{
	v1alpha2.AgentHarnessBackendHermes: {
		DefaultImage: acpSandboxHermesImage,
		ChildCommand: []string{"hermes", "acp"},
	},
}

// buildAcpAgentActorStartup returns the ateom workload startup script and
// container env for a generic stdio ACP agent (hermes) on
// Substrate. Unlike OpenClaw there is no in-sandbox gateway: the shim owns
// the atenet ingress port and bridges WebSocket frames to a long-lived
// agent child. Model credentials come from the harness ModelConfig as a
// provider-conventional env var (e.g. OPENAI_API_KEY, ANTHROPIC_API_KEY)
// resolved by ate-api from the referenced Secret.
func (p *Lifecycle) buildAcpAgentActorStartup(ctx context.Context, ah *v1alpha2.AgentHarness, spec acpAgentSpec) (script string, env []atev1alpha1.EnvVar, err error) {
	if ah == nil {
		return "", nil, fmt.Errorf("AgentHarness is required")
	}

	containerEnv := []corev1.EnvVar{
		{Name: "HOME", Value: openclaw.SubstrateActorHome},
		// Substrate actors do not inherit the image's ENV (unlike docker run),
		// so the shim's exec.LookPath and any subprocesses the agent spawns
		// need an explicit PATH. Includes the hermes image's venv bin dir.
		{Name: "PATH", Value: openclaw.SubstrateActorHome + "/.venv/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"},
	}

	prelude := ""
	if ref := strings.TrimSpace(ah.Spec.ModelConfigRef); ref != "" {
		mcRef, parseErr := utils.ParseRefString(ref, ah.Namespace)
		if parseErr != nil {
			return "", nil, fmt.Errorf("parse modelConfigRef %q: %w", ref, parseErr)
		}
		mc := &v1alpha2.ModelConfig{}
		if getErr := p.Client.Get(ctx, mcRef, mc); getErr != nil {
			return "", nil, fmt.Errorf("get ModelConfig %s: %w", mcRef, getErr)
		}
		apiKeyEnv, keyErr := openclaw.ModelConfigAPIKeyEnvVar(mc)
		if keyErr != nil {
			return "", nil, keyErr
		}
		containerEnv = append(containerEnv, apiKeyEnv)

		if ah.Spec.Backend == v1alpha2.AgentHarnessBackendHermes {
			prelude = hermesConfigPrelude(mc)
		}
	}

	// Hermes messaging channels (Slack/Telegram) run in a persistent gateway
	// process alongside the long-lived acp child, mirroring the OpenClaw
	// gateway. Translate channel credentials/allowlists into the unsuffixed
	// env contract the gateway auto-detects.
	runGateway := false
	if ah.Spec.Backend == v1alpha2.AgentHarnessBackendHermes && len(ah.Spec.Channels) > 0 {
		channelEnv, chErr := buildHermesChannelEnv(ctx, p.Client, ah.Namespace, ah.Spec.Channels)
		if chErr != nil {
			return "", nil, chErr
		}
		containerEnv = append(containerEnv, channelEnv...)
		runGateway = true
	}

	// Backend-agnostic env passthrough from the harness spec. Appended last so
	// it cannot shadow HOME/PATH.
	containerEnv = append(containerEnv, ah.Spec.Env...)

	script = buildAcpStartupScript(prelude, spec.ChildCommand, runGateway)
	return script, actorTemplateEnvFromPodEnv(containerEnv), nil
}

// buildAcpStartupScript renders the ateom workload startup script for an
// acp-shim agent. When runGateway is set (Hermes with channels) it ensures the
// persistent messaging gateway whenever the shim (re)spawns the child.
//
// The gateway is deliberately NOT pre-warmed into the golden snapshot: gVisor
// checkpoint/restore preserves the gateway process but its Telegram/Slack
// long-poll TCP connections are dead in the restored actor's fresh network
// namespace, and the PID-file liveness guard would then wrongly treat that
// zombie as healthy and never relaunch it (Telegram goes silent). Instead the
// gateway is launched fresh on the first post-restore connection — when the
// shim spawns the child — so it dials the platforms in the live network. The
// gateway launcher never fails the acp child, so the UI path is unaffected if a
// channel misbehaves.
func buildAcpStartupScript(prelude string, child []string, runGateway bool) string {
	childCmd := strings.Join(child, " ")
	if !runGateway {
		return fmt.Sprintf(
			"set -e\n%sexec /usr/local/bin/acp-shim \\\n  --listen :%d \\\n  -- %s",
			prelude, acpListenPort, childCmd)
	}
	return fmt.Sprintf(
		"set -e\n%sexec /usr/local/bin/acp-shim \\\n  --listen :%d \\\n  -- /bin/sh -c '/usr/local/bin/hermes-gateway-ensure.sh || true; exec %s'",
		prelude, acpListenPort, childCmd)
}

// hermesProviderSlugs maps kagent ModelConfig providers to hermes provider
// slugs (hermes_cli CANONICAL_PROVIDERS). Hermes authenticates these via the
// provider-conventional env var already injected from the ModelConfig secret.
var hermesProviderSlugs = map[v1alpha2.ModelProvider]string{
	v1alpha2.ModelProviderOpenAI:    "openai-api",
	v1alpha2.ModelProviderAnthropic: "anthropic",
	v1alpha2.ModelProviderGemini:    "gemini",
}

// hermesConfigPrelude returns shell lines that write ~/.hermes/config.yaml
// selecting the ModelConfig's model and provider. Without it hermes defaults
// to an unauthenticated provider and prompts silently produce no output.
// Returns "" when the ModelConfig provider has no hermes equivalent.
func hermesConfigPrelude(mc *v1alpha2.ModelConfig) string {
	slug, ok := hermesProviderSlugs[mc.Spec.Provider]
	if !ok || strings.TrimSpace(mc.Spec.Model) == "" {
		return ""
	}
	cfg := fmt.Sprintf("model:\n  default: %q\n  provider: %q\n", mc.Spec.Model, slug)
	if mc.Spec.Provider == v1alpha2.ModelProviderOpenAI {
		// Hermes auto-upgrades direct api.openai.com to its codex_responses
		// transport, which requests reasoning.encrypted_content — rejected
		// with HTTP 400 by non-reasoning models (e.g. gpt-4.1-mini), and the
		// turn ends silently. Pin the plain chat-completions transport.
		cfg += "  api_mode: chat_completions\n"
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(cfg))
	return fmt.Sprintf("mkdir -p \"$HOME/.hermes\"\necho %s | base64 -d > \"$HOME/.hermes/config.yaml\"\n", encoded)
}
