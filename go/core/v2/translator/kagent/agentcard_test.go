package kagent

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestAgentTemplateCardDeclaresHumanInTheLoop pins discoverability. The compiled
// card is a snapshot stored with the revision, not the runtime's live card, so a
// capability the runtime has is invisible unless this states it. Dropping it
// breaks nothing observable at the API — a reply still works for a client that
// knows to ask — which is exactly why it needs a test: the failure is a client
// that cannot tell an answerable question from an unanswerable one.
func TestAgentTemplateCardDeclaresHumanInTheLoop(t *testing.T) {
	card := agentTemplateCard(&v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "pizza-agent", Namespace: "team-a"},
	})

	if !card.Capabilities.Streaming {
		t.Fatalf("capabilities = %#v, want streaming", card.Capabilities)
	}
	var found bool
	for _, extension := range card.Capabilities.Extensions {
		if extension.URI == hitlExtensionURI {
			found = true
			if extension.Required {
				t.Fatal("the HITL extension must be optional; requiring it would refuse clients that cannot answer questions")
			}
		}
	}
	if !found {
		t.Fatalf("extensions = %#v, want %s declared", card.Capabilities.Extensions, hitlExtensionURI)
	}
	// The card must stay free of cluster-specific addresses; the gateway supplies
	// the public interface.
	if len(card.SupportedInterfaces) != 1 || card.SupportedInterfaces[0].URL != "http://127.0.0.1:80" {
		t.Fatalf("supported interfaces = %#v", card.SupportedInterfaces)
	}
	// The name is normalised for ADK, which rejects hyphens.
	if card.Name != "pizza_agent" {
		t.Fatalf("card name = %q, want the ADK-safe form", card.Name)
	}
}
