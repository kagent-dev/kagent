package substrate

import (
	"regexp"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Golden output for ActorID(kagent/my-claw); catches accidental algorithm changes.
const actorIDGoldenKagentMyClaw = "ahr-a252e34585581de8"

func TestAgentHarnessGatewayPaths(t *testing.T) {
	t.Parallel()
	const ns, name = "kagent", "my-claw"
	wantPublic := "/api/agentharnesses/kagent/my-claw/gateway/"
	wantControlUI := "/api/agentharnesses/kagent/my-claw/gateway"

	if got := AgentHarnessAPIBase(ns, name); got != "/api/agentharnesses/kagent/my-claw" {
		t.Fatalf("APIBase = %q", got)
	}
	if got := AgentHarnessGatewayUIPath(ns, name); got != wantPublic {
		t.Fatalf("GatewayUIPath = %q, want %q", got, wantPublic)
	}
	if got := AgentHarnessGatewayControlUIBasePath(ns, name); got != wantControlUI {
		t.Fatalf("ControlUIBasePath = %q, want %q", got, wantControlUI)
	}
}

func TestConnectionEndpoint(t *testing.T) {
	t.Parallel()
	ah := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Namespace: "kagent", Name: "my-claw"},
	}
	want := "/api/agentharnesses/kagent/my-claw/gateway/"

	if got := connectionEndpoint(nil); got != "" {
		t.Fatalf("nil harness = %q, want empty", got)
	}
	if got := connectionEndpoint(ah); got != want {
		t.Fatalf("connectionEndpoint = %q, want %q", got, want)
	}
}

func TestGatewayPort(t *testing.T) {
	t.Parallel()
	ah := &v1alpha2.AgentHarness{
		Spec: v1alpha2.AgentHarnessSpec{
			Substrate: &v1alpha2.AgentHarnessSubstrateSpec{GatewayPort: 8080},
		},
	}
	if got := GatewayPort(ah); got != 8080 {
		t.Fatalf("GatewayPort = %d, want 8080", got)
	}
	if got := GatewayPort(nil); got != 80 {
		t.Fatalf("GatewayPort(nil) = %d, want 80", got)
	}
	if got := GatewayPort(&v1alpha2.AgentHarness{}); got != 80 {
		t.Fatalf("GatewayPort(empty) = %d, want 80", got)
	}
}

func TestActorID(t *testing.T) {
	if ActorID(nil) != "" {
		t.Fatalf("nil harness: got %q, want empty", ActorID(nil))
	}

	ah := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Namespace: "kagent", Name: "my-claw"},
	}
	id := ActorID(ah)
	if id != actorIDGoldenKagentMyClaw {
		t.Fatalf("ActorID = %q, want golden %q", id, actorIDGoldenKagentMyClaw)
	}
	if ActorID(ah) != id {
		t.Fatal("expected stable id across calls")
	}

	other := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Namespace: "kagent", Name: "other-claw"},
	}
	if ActorID(other) == id {
		t.Fatalf("different harnesses should not share actor id %q", id)
	}

	const wantLen = len(actorIDPrefix) + 1 + actorIDHashHexLen
	if len(id) != wantLen {
		t.Fatalf("id length = %d, want %d (%q)", len(id), wantLen, id)
	}
	if !regexp.MustCompile(`^ahr-[0-9a-f]{16}$`).MatchString(id) {
		t.Fatalf("id %q does not match ahr-<hex> form", id)
	}
}

func TestActorHost(t *testing.T) {
	got := ActorHost("ahr-kagent-my-claw", "")
	if got != "ahr-kagent-my-claw.actors.resources.substrate.ate.dev" {
		t.Fatalf("ActorHost = %q", got)
	}
}

func TestActorTemplateRefManagedProvisioner(t *testing.T) {
	t.Parallel()
	ah := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "kagent",
			Name:      "peterj-claw",
			Annotations: map[string]string{
				AnnotationManagedActorTemplate: "true",
			},
		},
	}
	ns, name := actorTemplateRef(ah, Config{
		DefaultActorTemplateNamespace: "ate-demo-openclaw",
		DefaultActorTemplateName:      "openclaw",
	})
	if ns != "kagent" || name != "peterj-claw" {
		t.Fatalf("got %s/%s, want kagent/peterj-claw", ns, name)
	}
}
