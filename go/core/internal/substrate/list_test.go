package substrate

import (
	"strings"
	"testing"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

func TestActorStatusLabelNeverReturnsAWireConstant(t *testing.T) {
	// Every value the enum defines, including any added since this was written: callers
	// sort on the label and display it, so a constant sorts by a prefix nobody sees.
	names := ateapipb.ActorState_name
	if len(names) == 0 {
		t.Fatal("ActorState has no values; the enum this reads moved")
	}
	for number, name := range names {
		label := ActorStatusLabel(ateapipb.ActorState(number))
		if strings.Contains(label, "_") {
			t.Errorf("%s reads as %q; it should be words", name, label)
		}
		if label == name {
			t.Errorf("%s reaches a reader as its own wire constant", name)
		}
	}

	for state, want := range map[ateapipb.ActorState]string{
		ateapipb.ActorState_ACTOR_STATE_CRASHED:     "Crashed",
		ateapipb.ActorState_ACTOR_STATE_DELETING:    "Deleting",
		ateapipb.ActorState_ACTOR_STATE_RUNNING:     "Running",
		ateapipb.ActorState_ACTOR_STATE_UNSPECIFIED: "Unknown",
	} {
		if got := ActorStatusLabel(state); got != want {
			t.Errorf("ActorStatusLabel(%v) = %q, want %q", state, got, want)
		}
	}
}
