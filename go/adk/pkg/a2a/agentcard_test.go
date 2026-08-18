package a2a

import (
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

func TestEnsureHITLExtension(t *testing.T) {
	t.Run("attaches when absent", func(t *testing.T) {
		card := &a2atype.AgentCard{Name: "agent"}
		EnsureHITLExtension(card)
		if !hasHITLExtension(card.Capabilities.Extensions) {
			t.Fatalf("extensions = %#v, want the HITL extension", card.Capabilities.Extensions)
		}
	})

	t.Run("idempotent", func(t *testing.T) {
		card := &a2atype.AgentCard{Name: "agent"}
		EnsureHITLExtension(card)
		EnsureHITLExtension(card)
		if len(card.Capabilities.Extensions) != 1 {
			t.Errorf("len(extensions) = %d, want 1", len(card.Capabilities.Extensions))
		}
	})

	t.Run("preserves unrelated extensions", func(t *testing.T) {
		card := &a2atype.AgentCard{
			Name: "agent",
			Capabilities: a2atype.AgentCapabilities{
				Extensions: []a2atype.AgentExtension{{URI: "https://example.com/extensions/other/v1"}},
			},
		}
		EnsureHITLExtension(card)
		if len(card.Capabilities.Extensions) != 2 {
			t.Fatalf("len(extensions) = %d, want 2", len(card.Capabilities.Extensions))
		}
		if card.Capabilities.Extensions[0].URI != "https://example.com/extensions/other/v1" {
			t.Errorf("extensions[0].URI = %q, want the pre-existing extension", card.Capabilities.Extensions[0].URI)
		}
	})

	t.Run("nil card", func(t *testing.T) {
		EnsureHITLExtension(nil)
	})
}
