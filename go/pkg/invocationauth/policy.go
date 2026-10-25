package invocationauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

const EgressPolicyPath = "/runtime/egress"

type Target struct {
	Origin      string                                 `json:"origin"`
	Headers     map[string]string                      `json:"headers,omitempty"`
	Passthrough []*configv1.InvocationCredentialHeader `json:"passthrough,omitempty"`
}

type policyURLKey struct{}

type policyInterceptor struct {
	a2asrv.PassthroughCallInterceptor
	url string
}

var _ a2asrv.CallInterceptor = (*policyInterceptor)(nil)

// PolicyCallInterceptor provides the configured control-plane endpoint to the
// invocation. Caller headers cannot choose where policy is fetched.
func PolicyCallInterceptor(controlPlaneURL string) a2asrv.CallInterceptor {
	var endpoint string
	if controlPlaneURL != "" {
		endpoint = strings.TrimRight(controlPlaneURL, "/") + EgressPolicyPath
	}
	return &policyInterceptor{url: endpoint}
}

func (p *policyInterceptor) Before(ctx context.Context, _ *a2asrv.CallContext, _ *a2asrv.Request) (context.Context, any, error) {
	return context.WithValue(ctx, policyURLKey{}, p.url), nil, nil
}

// targets reads live, non-secret header bindings using the runtime's Actor JWT.
// Caller tokens never leave the invocation to resolve policy.
func (s Source) targets(ctx context.Context) ([]Target, error) {
	u, err := url.Parse(s.policyURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid runtime egress policy URL")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.policyURL, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid runtime egress policy URL")
	}
	req.Header.Set("Authorization", "Bearer kagent-credential-injected")
	client := *http.DefaultClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("runtime egress policy unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("runtime egress policy unavailable (HTTP %d)", response.StatusCode)
	}
	var targets []Target
	if err := json.NewDecoder(response.Body).Decode(&targets); err != nil {
		return nil, fmt.Errorf("invalid runtime egress policy")
	}
	return targets, nil
}
