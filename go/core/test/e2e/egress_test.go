package e2e_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSessionEgressDeniesUnconfiguredDestination(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		target := interactionTarget(t)
		var reachedDenied atomic.Bool
		origin := startModelRecorder(t, startMockLLMServer(t, interactionMocks, "mocks/invoke_agent.json"), func([]byte) error {
			reachedDenied.Store(true)
			return nil
		})
		denied, err := url.Parse(reachableModelURL(t, origin.URL))
		require.NoError(t, err)
		// Give the destination a distinct hostname even when host mocks use
		// host.docker.internal. The gateway allowlist is keyed by hostname.
		kube := interactionKubeClient(t)
		alias := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "denied-origin-", Namespace: "kagent"},
			Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName, ExternalName: denied.Hostname()},
		}
		require.NoError(t, kube.Create(t.Context(), alias))
		t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), alias)) })
		denied.Host = net.JoinHostPort(alias.Name+".kagent.svc.cluster.local", denied.Port())

		// First prove the destination is reachable and speaks this harness's
		// model protocol when explicitly configured.
		control := newInteractionFixture(t, harness, target, denied.String())
		_, _, task := control.send(t, "What is 2+2?")
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
		require.Contains(t, taskText(task), "The answer is 4.")
		require.True(t, reachedDenied.Load())
		reachedDenied.Store(false)

		var reachedAllowed atomic.Bool
		server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reachedAllowed.Store(true)
			// The model host is allowed, but its redirect must not expand the
			// Actor's policy to another reachable, unconfigured destination.
			redirect := *denied
			redirect.Path, redirect.RawQuery = r.URL.Path, r.URL.RawQuery
			http.Redirect(w, r, redirect.String(), http.StatusTemporaryRedirect)
		}))
		_ = server.Listener.Close()
		listener, err := net.Listen("tcp", "0.0.0.0:0")
		if err != nil {
			t.Fatal(err)
		}
		server.Listener = listener
		server.Start()
		t.Cleanup(server.Close)
		fixture := newInteractionFixture(t, harness, target, reachableModelURL(t, server.URL))
		_, _, task = fixture.send(t, "What is 2+2?")
		if !reachedAllowed.Load() {
			t.Fatal("the configured model destination was not reached")
		}
		require.Equal(t, a2atype.TaskStateFailed, task.Status.State, "redirect task text = %q", taskText(task))
		require.False(t, reachedDenied.Load(), "redirect reached an unconfigured destination")
	})
}
