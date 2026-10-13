package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestSessionEgressPolicyPinsAdditionalDestinations(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		target := interactionTarget(t)
		mcpURL, mcpServer := startMCPMock(t)
		denied, err := url.Parse(mcpURL)
		require.NoError(t, err)
		// Give the destination a distinct hostname even when host mocks use
		// host.docker.internal. It must not share the model or configured MCP origin.
		kube := interactionKubeClient(t)
		alias := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "denied-origin-", Namespace: "kagent"},
			Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName, ExternalName: denied.Hostname()},
		}
		require.NoError(t, kube.Create(t.Context(), alias))
		t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), alias)) })
		denied.Host = net.JoinHostPort(alias.Name+".kagent.svc.cluster.local", denied.Port())

		toolCalls := func() int {
			calls := 0
			for _, request := range mcpServer.Requests() {
				var message struct {
					Method string `json:"method"`
				}
				if json.Unmarshal(request.Body, &message) == nil && message.Method == "tools/call" {
					calls++
				}
			}
			return calls
		}
		const prompt = "Add 3 and 5 using the configured MCP server."
		// Prove the destination is reachable when explicitly configured.
		controlName, _ := createMCPInteractionTemplate(t, harness, denied.String(), false)
		control := newInteractionFixtureForTemplate(t, harness, target, controlName)
		_, _, task := control.send(t, prompt)
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
		require.Contains(t, taskText(task), "result is 8")
		controlCalls := toolCalls()
		require.Positive(t, controlCalls)

		upstream := *denied
		upstream.Host, upstream.Path = net.JoinHostPort("127.0.0.1", denied.Port()), ""
		proxy := httputil.NewSingleHostReverseProxy(&upstream)
		var redirects atomic.Int64
		server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = r.Body.Close()
			var message struct {
				Method string `json:"method"`
			}
			if json.Unmarshal(body, &message) == nil && message.Method == "tools/call" {
				redirects.Add(1)
				http.Redirect(w, r, denied.String(), http.StatusTemporaryRedirect)
				return
			}
			// Keep initialization and discovery on the configured endpoint so
			// only the runtime's tool call exercises the additional destination.
			r.Body = io.NopCloser(bytes.NewReader(body))
			proxy.ServeHTTP(w, r)
		}))
		_ = server.Listener.Close()
		listener, err := net.Listen("tcp", "0.0.0.0:0")
		if err != nil {
			t.Fatal(err)
		}
		server.Listener = listener
		server.Start()
		t.Cleanup(server.Close)
		name, _ := createMCPInteractionTemplate(t, harness, reachableServerURL(t, server.URL, denied.Path), false)
		fixture := newInteractionFixtureForTemplate(t, harness, target, name)
		fixture.send(t, prompt)
		require.Positive(t, redirects.Load(), "the runtime must attempt the MCP tool call")
		require.Equal(t, controlCalls, toolCalls(), "redirect reached an unconfigured destination")

		grant := &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{GenerateName: "redirect-access-", Namespace: "kagent"}, Spec: v1alpha3.EgressPolicySpec{
			Destinations: []v1alpha3.EgressDestination{{Origin: denied.Scheme + "://" + denied.Host}},
		}}
		require.NoError(t, kube.Create(t.Context(), grant))
		t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), grant)) })
		agent := &v1alpha3.Agent{}
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
		_, _, task = allowed.send(t, prompt)
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, "policy-granted redirect text = %q", taskText(task))
		require.Contains(t, taskText(task), "result is 8")
		allowedCalls := toolCalls()
		require.Greater(t, allowedCalls, controlCalls, "the new session must reach the policy-granted destination")
		before := redirects.Load()
		fixture.send(t, prompt)
		require.Greater(t, redirects.Load(), before, "the existing session must retry the tool call")
		require.Equal(t, allowedCalls, toolCalls(), "existing session must retain its earlier grant")
	})
}
