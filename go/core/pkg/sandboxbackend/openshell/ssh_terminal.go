package openshell

import (
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openclaw"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/hermes"
)

// ResolveSSHRemoteCommand decides whether to run an interactive shell or a harness CLI.
// plainShell: client requested bash only.
// launchOverride: non-empty client launch_command wins.
// harnessBackend: wire string from the WebSocket start frame (e.g. "hermes").
func ResolveSSHRemoteCommand(plainShell bool, launchOverride, harnessBackend string) (useShell bool, execCmd string) {
	if plainShell {
		return true, ""
	}
	cmd := strings.TrimSpace(launchOverride)
	if cmd != "" {
		return false, cmd
	}
	backend := v1alpha2.AgentHarnessBackendType(strings.TrimSpace(harnessBackend))
	if v1alpha2.IsKnownAgentHarnessBackend(backend) {
		switch {
		case hermes.IsHermesSandboxBackend(backend):
			return false, hermes.DefaultSSHLaunchCommand()
		case openclaw.IsClawSandboxBackend(backend):
			return false, openclaw.DefaultSSHLaunchCommand()
		}
	}
	return false, openclaw.DefaultSSHLaunchCommand()
}
