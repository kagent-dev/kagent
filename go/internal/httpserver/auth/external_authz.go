package auth

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/kagent-dev/kagent/go/pkg/auth"
)

// ExternalAuthorizer calls an external HTTP authorization endpoint to make
// authorization decisions. It delegates request/response serialization to a
// Provider, which handles engine-specific wire formats (e.g. OPA wraps
// requests as {"input": ...} and returns {"result": ...}).
type ExternalAuthorizer struct {
	// Endpoint is the URL of the external authorization service,
	// e.g. "http://opa:8181/v1/data/kagent/authz"
	Endpoint string
	// Provider translates between kagent's AuthzRequest/AuthzDecision
	// and the engine's wire format.
	Provider Provider
	Client   *http.Client
}

var _ auth.Authorizer = (*ExternalAuthorizer)(nil)

func (a *ExternalAuthorizer) Check(ctx context.Context, req auth.AuthzRequest) (*auth.AuthzDecision, error) {
	body, err := a.Provider.MarshalRequest(req)
	if err != nil {
		return nil, fmt.Errorf("marshal authz request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create authz request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("authz request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("authz endpoint returned HTTP %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read authz response: %w", err)
	}

	return a.Provider.UnmarshalDecision(respBody)
}
