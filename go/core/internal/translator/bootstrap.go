package translator

import (
	"encoding/json"
	"fmt"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/api/bootstrap"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
)

// BootstrapBundle renders the immutable payload served to a revision's actors.
func (r *Revision) BootstrapBundle(id RevisionID) (*runtimev1.BootstrapServiceGetBootstrapConfigResponse, error) {
	card, err := apia2a.FromProtoAgentCard(r.AgentCard)
	if err != nil {
		return nil, fmt.Errorf("convert bootstrap Agent Card: %w", err)
	}
	raw, err := json.Marshal(card)
	if err != nil {
		return nil, fmt.Errorf("render bootstrap Agent Card: %w", err)
	}
	return bootstrap.NewBundle(id.String(), r.ConfigJSON, raw)
}
