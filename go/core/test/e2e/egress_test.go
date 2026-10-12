package e2e_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestSessionEgressPolicyPinsAdditionalDestinations(t *testing.T) {
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
		// host.docker.internal. Keep the redirect visibly distinct from the model origin.
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

		grant := &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{GenerateName: "redirect-access-", Namespace: "kagent"}, Spec: v1alpha3.EgressPolicySpec{
			Destinations: []v1alpha3.EgressDestination{{Origin: denied.Scheme + "://" + denied.Host}},
		}}
		require.NoError(t, kube.Create(t.Context(), grant))
		t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), grant)) })
		agent := &v1alpha3.Agent{}
		name := strings.TrimPrefix(fixture.tenant, "kagent/")
		key := ctrlclient.ObjectKey{Namespace: "kagent", Name: name}
		require.NoError(t, kube.Get(t.Context(), key, agent))
		agent.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: grant.Name}}
		require.NoError(t, kube.Update(t.Context(), agent))
		require.NoError(t, wait.PollUntilContextTimeout(t.Context(), time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
			if err := kube.Get(ctx, key, agent); err != nil {
				return false, err
			}
			for _, condition := range agent.Status.Conditions {
				if condition.Type == v1alpha3.AgentConditionReady && condition.ObservedGeneration == agent.Generation {
					return condition.Status == metav1.ConditionTrue, nil
				}
			}
			return false, nil
		}))
		allowed := newInteractionFixtureForTemplate(t, harness, target, name)
		_, _, task = allowed.send(t, "What is 2+2?")
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, "policy-granted redirect text = %q", taskText(task))
		require.True(t, reachedDenied.Load())
		reachedDenied.Store(false)
		_, _, task = fixture.send(t, "What is 2+2?")
		require.Equal(t, a2atype.TaskStateFailed, task.Status.State)
		require.False(t, reachedDenied.Load(), "existing session must retain its earlier grant")
	})
}
