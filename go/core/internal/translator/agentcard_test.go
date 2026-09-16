package translator

import (
	"testing"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestManagedAgentCardDeclaresHITL(t *testing.T) {
	card := ManagedAgentCard(&v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "managed-agent"},
	})
	if !card.Capabilities.Streaming || len(card.Capabilities.Extensions) != 1 {
		t.Fatalf("capabilities = %#v", card.Capabilities)
	}
	extension := card.Capabilities.Extensions[0]
	if extension.URI != apia2a.HITLExtensionURI || extension.Required {
		t.Fatalf("HITL extension = %#v", extension)
	}
}
