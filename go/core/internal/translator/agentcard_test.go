package translator

import (
	"testing"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestManagedAgentCardDeclaresHITL(t *testing.T) {
	card := ManagedAgentCard("managed-agent", &TemplateConfiguration{
		Name: "managed-agent", Namespace: "", Source: &metav1.ObjectMeta{Name: "managed-agent"},
	})
	if !card.Capabilities.Streaming || len(card.Capabilities.Extensions) != 1 {
		t.Fatalf("capabilities = %#v", card.Capabilities)
	}
	extension := card.Capabilities.Extensions[0]
	if extension.URI != apia2a.HITLExtensionURI || extension.Required {
		t.Fatalf("HITL extension = %#v", extension)
	}
}
