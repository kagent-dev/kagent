package e2e_test

import (
	"context"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

// harnessExitBound is how long a turn whose Harness process exited may take to
// end. Substrate crashes the Actor about a second after the exit and the
// gateway fails the turn once it reads the crash; without that report the turn
// waits out the caller's whole deadline.
const harnessExitBound = 2 * time.Minute

// A Harness process that exits mid-turn while its sandbox keeps running ends
// the turn FAILED with the exit named, instead of leaving the task working
// with nothing behind it, and the Session settles and stays deletable (the
// fixture's cleanup deletes it).
func TestHarnessExitMidTurnFailsTheTurn(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	fixture := newBashToolFixture(t)
	session := newInteractionFixtureForTemplate(t, goADKHarness, target, fixture.templateFor(t, goADKHarness.name))

	// The bash tool sends SIGQUIT to the Harness, its parent: the Go runtime
	// exits with status 2 and the container's process is gone.
	bounded := *session
	ctx, cancel := context.WithTimeout(session.ctx, harnessExitBound)
	defer cancel()
	bounded.ctx = ctx
	_, _, task := bounded.send(t, "Exit the harness.")

	text := taskText(task)
	if task.Status.State != a2atype.TaskStateFailed || !strings.Contains(text, `container "kagent"`) {
		t.Fatalf("task %s with %q, want %s naming the exited container; %s", task.Status.State, text, a2atype.TaskStateFailed, session.actorReport())
	}
}
