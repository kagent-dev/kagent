package e2e_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

func TestAgentInstanceEgressDeniesUnconfiguredDestination(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	var reachedAllowed atomic.Bool
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reachedAllowed.Store(true)
		// The model host is allowed, but its redirect must not expand the
		// Actor's policy to this unconfigured documentation-only address.
		http.Redirect(w, r, "http://192.0.2.1/v1/chat/completions", http.StatusTemporaryRedirect)
	}))
	_ = server.Listener.Close()
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	fixture := newInteractionFixture(t, target, reachableModelURL(t, server.URL))
	_, _, task := fixture.send(t, "Follow the model redirect")
	if !reachedAllowed.Load() {
		t.Fatal("the configured model destination was not reached")
	}
	if task.Status.State != a2atype.TaskStateFailed || !strings.Contains(taskText(task), "403") {
		t.Fatalf("redirect task state = %s, text = %q; want egress denial (403)", task.Status.State, taskText(task))
	}
}
