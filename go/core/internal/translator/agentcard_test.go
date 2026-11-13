package translator

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

func TestManagedAgentCardDeclaresItsExtensions(t *testing.T) {
	card := ManagedAgentCard("managed-agent", &TemplateConfiguration{
		Name: "managed-agent", Namespace: "", Source: &metav1.ObjectMeta{Name: "managed-agent"},
	})
	if !card.Capabilities.Streaming || len(card.Capabilities.Extensions) != 2 {
		t.Fatalf("capabilities = %#v", card.Capabilities)
	}
	// Both are optional: requiring one would refuse clients that do not know it.
	for index, uri := range []string{apia2a.HITLExtensionURI, apia2a.UsageExtensionURI} {
		if extension := card.Capabilities.Extensions[index]; extension.URI != uri || extension.Required {
			t.Fatalf("extension %d = %#v, want optional %s", index, extension, uri)
		}
	}
}
