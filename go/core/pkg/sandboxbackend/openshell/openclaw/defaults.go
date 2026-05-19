package openclaw

import (
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/channels"
)

// DefaultAPIKeyEnvVar is the environment variable name used for the model provider API key in the sandbox.
func DefaultAPIKeyEnvVar(provider v1alpha2.ModelProvider) string {
	return fmt.Sprintf("%s_API_KEY", strings.ToUpper(string(provider)))
}

// DefaultSSHLaunchCommand is the interactive CLI started when connecting to an
// OpenClaw or NemoClaw harness sandbox via the UI terminal (unless plain shell is requested).
func DefaultSSHLaunchCommand() string {
	return "openclaw tui"
}

// openshellResolveEnv matches OpenClaw onboard placeholders for OpenShell L7 credential rewrite.
func openshellResolveEnv(envVar string) string {
	return channels.ResolveEnvPlaceholder(envVar)
}
