package e2e_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestSessionEgressDeniesUnconfiguredDestination(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
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
		harness = egressHarnessWithoutRetries(t, kube, harness)
		alias := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "denied-origin-", Namespace: "kagent"},
			Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName, ExternalName: denied.Hostname()},
		}
		require.NoError(t, kube.Create(t.Context(), alias))
		t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), alias)) })
		denied.Host = net.JoinHostPort(alias.Name+".kagent.svc.cluster.local.", denied.Port())

		// First prove the destination is reachable and speaks this harness's
		// model protocol when explicitly configured.
		control := newInteractionFixture(t, harness, target, denied.String())
		_, _, task := control.send(t, "What is 2+2?")
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
		require.Contains(t, taskText(task), "The answer is 4.")
		require.True(t, reachedDenied.Load())
		reachedDenied.Store(false)

		var modelRequests atomic.Int32
		server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Claude also sends a HEAD probe during startup.
			if r.Method == http.MethodPost {
				modelRequests.Add(1)
			}
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
		// One denied request must terminate without waiting through the native
		// clients' production retry budgets or canceling the task to pass.
		ctx, cancel := context.WithTimeout(fixture.ctx, 30*time.Second)
		t.Cleanup(cancel)
		fixture.ctx = ctx
		t.Cleanup(func() {
			t.Logf("redirected model requests = %d, denied destination reached = %v", modelRequests.Load(), reachedDenied.Load())
		})
		_, _, task = fixture.send(t, "What is 2+2?")
		require.Equal(t, int32(1), modelRequests.Load(), "expected one model request without retries")
		require.Equal(t, a2atype.TaskStateFailed, task.Status.State, "redirect task text = %q", taskText(task))
		require.False(t, reachedDenied.Load(), "redirect reached an unconfigured destination")
	})
}

// Use native retry controls only in this fixture. The allowed control request
// also uses this Harness, proving the overrides preserve working model calls.
func egressHarnessWithoutRetries(t *testing.T, kube ctrlclient.Client, original testHarness) testHarness {
	t.Helper()
	if original.runtimeLabel != "codex" && original.runtimeLabel != "claude" {
		return original
	}
	base := &v1alpha3.Harness{}
	require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "kagent", Name: original.name}, base))
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "egress-" + original.runtimeLabel + "-", Namespace: base.Namespace},
		Spec:       *base.Spec.DeepCopy(),
	}
	switch original.runtimeLabel {
	case "claude":
		harness.Spec.Env = append(harness.Spec.Env, v1alpha3.RuntimeEnvVar{Name: "CLAUDE_CODE_MAX_RETRIES", Value: "0"})
	case "codex":
		// Codex exposes these controls through CLI flags. A fixture-local
		// launcher supplies them without adding production configuration.
		harness.Spec.Workload.Command = []string{"/bin/sh", "-ec"}
		harness.Spec.Workload.Args = []string{`mkdir -p /data/egress-test-bin
cat > /data/egress-test-bin/codex <<'EOF'
#!/bin/sh
exec /usr/local/bin/codex -c model_providers.kagent-openai.request_max_retries=0 -c model_providers.kagent-openai.stream_max_retries=0 "$@"
EOF
chmod 700 /data/egress-test-bin/codex
export PATH="/data/egress-test-bin:$PATH"
exec /usr/local/bin/kagent-codex
`}
	}
	require.NoError(t, kube.Create(t.Context(), harness))
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), harness); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete egress Harness: %v", err)
		}
	})
	return testHarness{name: harness.Name, runtimeLabel: original.runtimeLabel}
}
