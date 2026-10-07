package connection

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/transport"

	// The kubeconfig's oidc auth-provider, so an id-token it holds is read, and
	// refreshed when expired, the way kubectl does.
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

const bearerPrefix = "Bearer "

// callerTokenAdvice names every way to supply a caller token, for a refusal
// the person can act on.
var callerTokenAdvice = "pass --" + flagCallerToken + ", set " + env.KagentToken.Name() +
	", or select a kubeconfig context whose credential is a token (an exec plugin or an id-token)"

// callerToken is the bearer the CLI sends with every controller call, and
// where it came from: messages name the source, never the value.
type callerToken struct {
	value  string
	source string
}

func (t callerToken) isSet() bool {
	return t.value != ""
}

// resolveCallerToken picks the caller token: the flag, then KAGENT_TOKEN, then
// the credential of the current kubeconfig context. An empty token with no
// error means no source offered one.
func resolveCallerToken(flagValue string) (callerToken, error) {
	if flagValue != "" {
		return callerToken{value: flagValue, source: "--" + flagCallerToken}, nil
	}
	if value := env.KagentToken.Get(); value != "" {
		return callerToken{value: value, source: env.KagentToken.Name()}, nil
	}
	return kubeconfigCallerToken()
}

// kubeconfigCallerToken is the bearer kubectl would send for the current
// context: a static token, an exec plugin's credential or an oidc id-token.
// client-go's transport wrappers resolve all three, an exec plugin's run and an
// expired id-token's refresh included, so the credential is read off a request
// that never leaves the process instead of being resolved a second way.
func kubeconfigCallerToken() (callerToken, error) {
	loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	)
	rawConfig, err := loader.RawConfig()
	if err != nil {
		return callerToken{}, fmt.Errorf("failed to load kubeconfig: %w", err)
	}
	if rawConfig.CurrentContext == "" {
		return callerToken{}, nil
	}
	source := "kubeconfig context " + rawConfig.CurrentContext

	restConfig, err := loader.ClientConfig()
	if err != nil {
		return callerToken{}, fmt.Errorf("failed to load %s: %w", source, err)
	}
	transportConfig, err := restConfig.TransportConfig()
	if err != nil {
		return callerToken{}, fmt.Errorf("failed to read the credential of %s: %w", source, err)
	}
	capture := &authorizationCapture{}
	roundTripper, err := transport.HTTPWrappersForConfig(transportConfig, capture)
	if err != nil {
		return callerToken{}, fmt.Errorf("failed to read the credential of %s: %w", source, err)
	}
	request, err := http.NewRequest(http.MethodGet, "https://kubernetes.invalid/", nil)
	if err != nil {
		return callerToken{}, fmt.Errorf("failed to read the credential of %s: %w", source, err)
	}
	if _, err := roundTripper.RoundTrip(request); err != nil {
		return callerToken{}, fmt.Errorf("failed to read the credential of %s: %w", source, err)
	}
	token, isBearer := strings.CutPrefix(capture.authorization, bearerPrefix)
	if !isBearer || token == "" {
		// A client certificate, or no credential at all.
		return callerToken{}, nil
	}
	return callerToken{value: token, source: source}, nil
}

// authorizationCapture is the innermost round tripper under client-go's
// credential wrappers: it records the Authorization header they set and
// answers without sending anything.
type authorizationCapture struct {
	authorization string
}

var _ http.RoundTripper = (*authorizationCapture)(nil)

func (c *authorizationCapture) RoundTrip(request *http.Request) (*http.Response, error) {
	c.authorization = request.Header.Get("Authorization")
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: request}, nil
}
