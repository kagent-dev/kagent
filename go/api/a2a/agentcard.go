package a2a

import (
	"fmt"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"google.golang.org/protobuf/proto"
)

// FromProtoAgentCard converts a stored card for the A2A protocol.
// Binary protobuf does not distinguish absent and empty repeated fields. The
// upstream converter requires non-nil skills/tags and a description, while our
// templates allow empty skills and descriptions. Normalize its input and retain
// the template's description in the protocol response.
func FromProtoAgentCard(stored *a2apb.AgentCard) (*a2atype.AgentCard, error) {
	if stored == nil {
		return nil, fmt.Errorf("missing Agent Card")
	}
	value := proto.Clone(stored).(*a2apb.AgentCard)
	if value.Skills == nil {
		value.Skills = []*a2apb.AgentSkill{}
	}
	for _, skill := range value.Skills {
		if skill.Tags == nil {
			skill.Tags = []string{}
		}
	}
	if value.Description == "" {
		value.Description = value.Name
	}
	card, err := pbconv.FromProtoAgentCard(value)
	if err != nil {
		return nil, err
	}
	card.Description = stored.Description
	return card, nil
}
