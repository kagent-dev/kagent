package openclaw

import (
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

// openshellResolveEnv matches OpenClaw onboard --custom-api-key: credentials resolve via
// OpenShell’s env path inside the sandbox (same as literal OPENAI_API_KEY etc. still injected by kagent on ExecSandbox).
func openshellResolveEnv(envVar string) string {
	return "openshell:resolve:env:" + envVar
}

// DefaultAPIKeyEnvVar is the environment variable name used for the model provider API key in the sandbox.
func DefaultAPIKeyEnvVar(provider v1alpha2.ModelProvider) string {
	return fmt.Sprintf("%s_API_KEY", strings.ToUpper(string(provider)))
}
