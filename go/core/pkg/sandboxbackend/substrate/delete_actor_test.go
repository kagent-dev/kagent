package substrate

import (
	"testing"
	"time"

	"github.com/agent-substrate/substrate/proto/ateapipb"
)

func TestEnsureActorSuspendedAlreadySuspended(t *testing.T) {
	t.Parallel()
	c := &Client{}
	deadline := time.Now().Add(time.Minute)
	err := c.ensureActorSuspended(t.Context(), "ahr-test", ateapipb.Actor_STATUS_SUSPENDED, deadline)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
