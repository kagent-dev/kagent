package substrate

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

func TestActorID(t *testing.T) {
	ah := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "kagent",
			Name:      "my-claw",
			UID:       "00000000-0000-0000-0000-000000000001",
		},
	}
	id := ActorID(ah)
	if !dns1123Label.MatchString(id) {
		t.Fatalf("ActorID %q is not DNS-1123", id)
	}
	if id == "" {
		t.Fatal("expected non-empty actor id")
	}
}

func TestActorHost(t *testing.T) {
	got := ActorHost("ahr-kagent-my-claw", "")
	if got != "ahr-kagent-my-claw.actors.resources.substrate.ate.dev" {
		t.Fatalf("ActorHost = %q", got)
	}
}

func TestGeneratedActorTemplateKey(t *testing.T) {
	t.Parallel()
	ah := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "kagent",
			Name:      "peterj-claw",
		},
	}
	ns, name := generatedActorTemplateKey(ah)
	if ns != "kagent" || name != "peterj-claw" {
		t.Fatalf("got %s/%s, want kagent/peterj-claw", ns, name)
	}
}
