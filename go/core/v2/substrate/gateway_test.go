package substrate

import "testing"

func TestActorHost(t *testing.T) {
	if got := ActorHost("kagent", "actor-1", ""); got != "actor-1.kagent.actors.resources.substrate.ate.dev" {
		t.Fatalf("ActorHost() = %q", got)
	}
}
