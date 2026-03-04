package auth

import (
	"encoding/json"
	"fmt"

	"github.com/kagent-dev/kagent/go/pkg/auth"
)

// OPAProvider translates between kagent's authorization types and
// OPA's wire format. Requests are wrapped as {"input": <AuthzRequest>}
// and responses are unwrapped from {"result": <AuthzDecision>}.
type OPAProvider struct{}

var _ Provider = (*OPAProvider)(nil)

type opaRequest struct {
	Input auth.AuthzRequest `json:"input"`
}

type opaResponse struct {
	Result auth.AuthzDecision `json:"result"`
}

func (p *OPAProvider) Name() string { return "opa" }

func (p *OPAProvider) MarshalRequest(req auth.AuthzRequest) ([]byte, error) {
	return json.Marshal(opaRequest{Input: req})
}

func (p *OPAProvider) UnmarshalDecision(data []byte) (*auth.AuthzDecision, error) {
	var resp opaResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decode OPA response: %w", err)
	}
	return &resp.Result, nil
}
