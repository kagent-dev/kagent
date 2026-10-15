package kagent

import (
	"context"
	"slices"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"istio.io/istio/pkg/kube/krt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestCompilerRequiresModelConfig(t *testing.T) {
	_, err := NewCompiler(krt.TestingDummyContext{}, translator.Collections{}, testRuntimeDefaults()).Compile(context.Background(), &translator.HarnessInput{
		Harness: &translator.HarnessConfiguration{Spec: v1alpha3.HarnessSpec{Kagent: &v1alpha3.KagentHarness{}}},
		Root:    &translator.AgentInput{Template: &translator.TemplateConfiguration{}},
	})
	if err == nil || !strings.Contains(err.Error(), "kagent ModelConfig is required") {
		t.Fatalf("Compile() error = %v", err)
	}
}

// TestAgentTemplateCardDeclaresHumanInTheLoop pins discoverability. The compiled
// card is a snapshot stored with the revision, not the runtime's live card, so a
// capability the runtime has is invisible unless this states it. Dropping it
// breaks nothing observable at the API — a reply still works for a client that
// knows to ask — which is exactly why it needs a test: the failure is a client
// that cannot tell an answerable question from an unanswerable one.
func TestAgentTemplateCardDeclaresHumanInTheLoop(t *testing.T) {
	card := translator.ManagedAgentCard("pizza-agent", &translator.TemplateConfiguration{
		Name: "pizza-agent", Namespace: "team-a", Source: &metav1.ObjectMeta{Name: "pizza-agent", Namespace: "team-a"},
	})

	if !card.Capabilities.Streaming {
		t.Fatalf("capabilities = %#v, want streaming", card.Capabilities)
	}
	var found bool
	for _, extension := range card.Capabilities.Extensions {
		if extension.URI == apia2a.HITLExtensionURI {
			found = true
			if extension.Required {
				t.Fatal("the HITL extension must be optional; requiring it would refuse clients that cannot answer questions")
			}
		}
	}
	if !found {
		t.Fatalf("extensions = %#v, want HITL declared", card.Capabilities.Extensions)
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

// TestAgentCardDeclaresUsage keeps the usage extension on the kagent card only:
// the Codex and Claude harnesses share the managed card but do not emit usage.
func TestAgentCardDeclaresUsage(t *testing.T) {
	template := &translator.TemplateConfiguration{
		Name: "pizza-agent", Namespace: "team-a", Source: &metav1.ObjectMeta{Name: "pizza-agent", Namespace: "team-a"},
	}
	declares := func(card *a2atype.AgentCard) bool {
		return slices.ContainsFunc(card.Capabilities.Extensions, func(extension a2atype.AgentExtension) bool {
			return extension.URI == apia2a.UsageExtensionURI && !extension.Required
		})
	}

	if !declares(agentCard("pizza-agent", template)) {
		t.Fatal("kagent card does not declare the optional usage extension")
	}
	if declares(translator.ManagedAgentCard("pizza-agent", template)) {
		t.Fatal("the shared managed card declares usage, which the Codex and Claude harnesses do not emit")
	}
}

func testRuntimeDefaults() *configv1.RuntimeDefaultsConfig {
	return &configv1.RuntimeDefaultsConfig{ControlPlaneUrl: new("http://kagent-controller.kagent:8083"), GatewayUrl: new("http://kagent-controller.kagent:8083"), Logging: &configv1.LoggingConfig{Level: new("info")}}
}
