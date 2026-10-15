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
	"github.com/google/uuid"
	"github.com/kagent-dev/mockllm"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestSessionEgressPolicyPinsAdditionalDestinations(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		if harness.name == codexE2EHarness {
			testCodexSessionEgressPolicy(t, harness)
			return
		}
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
		testSessionEgressPolicyPinsDestination(t, harness, name, denied.Scheme+"://"+denied.Host, func(fixture *interactionFixture, allowed bool) {
			beforeRedirects, beforeCalls := redirects.Load(), toolCalls()
			_, _, task := fixture.send(t, prompt)
			require.Greater(t, redirects.Load(), beforeRedirects, "the runtime must attempt the MCP tool call")
			if allowed {
				require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, "policy-granted redirect text = %q", taskText(task))
				require.Contains(t, taskText(task), "result is 8")
				require.Greater(t, toolCalls(), beforeCalls, "the session must reach the policy-granted destination")
			} else {
				require.Equal(t, beforeCalls, toolCalls(), "redirect reached an unconfigured destination")
			}
		})
	})
}

func testCodexSessionEgressPolicy(t *testing.T, harness testHarness) {
	t.Helper()
	interactionTarget(t)
	// Codex's MCP client rejects cross-origin redirects before the gateway can
	// authorize them. Exercise additional destinations through its shell tool.
	var requests atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, "egress destination reached")
	}))
	_ = server.Listener.Close()
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	destination, err := url.Parse(reachableServerURL(t, server.URL, "/"))
	require.NoError(t, err)
	// Keep the destination distinct from the model origin on Docker Desktop too.
	kube := interactionKubeClient(t)
	alias := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "shell-origin-", Namespace: "kagent"},
		Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName, ExternalName: destination.Hostname()},
	}
	require.NoError(t, kube.Create(t.Context(), alias))
	t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), alias)) })
	destination.Host = net.JoinHostPort(alias.Name+".kagent.svc.cluster.local", destination.Port())

	raw, err := codexInteractionMocks.ReadFile("mocks/invoke_codex_builtin_tools.json")
	require.NoError(t, err)
	// Always emit the fixture marker so the mock model finishes after denied
	// requests too. The destination's request count proves whether egress worked.
	command := "wget -T 10 -qO- " + destination.String() + "; printf CODEX_TOOL_FILE"
	raw = bytes.ReplaceAll(raw, []byte("printf CODEX_TOOL_FILE"), []byte(command))
	var cfg mockllm.Config
	require.NoError(t, json.Unmarshal(raw, &cfg))
	modelURL := reachableModelURL(t, startMockLLMConfig(t, cfg))
	name := createInteractionTemplate(t, harness, modelURL)
	testSessionEgressPolicyPinsDestination(t, harness, name, destination.Scheme+"://"+destination.Host, func(fixture *interactionFixture, allowed bool) {
		before := requests.Load()
		streamed := sendStreaming(t, fixture, "Run the requested shell command.")
		require.Equal(t, a2atype.TaskStateCompleted, streamed.state, "egress probe text = %q, failure = %q", streamed.text, streamed.failureText)
		assertToolEvents(t, streamed.toolEvents, "command_execution")
		if allowed {
			require.Greater(t, requests.Load(), before, "the session must reach the policy-granted destination")
		} else {
			require.Equal(t, before, requests.Load(), "shell reached an unconfigured destination")
		}
	})
}

func testSessionEgressPolicyPinsDestination(t *testing.T, harness testHarness, name, origin string, probe func(*interactionFixture, bool)) {
	t.Helper()
	target := interactionTarget(t)
	kube := interactionKubeClient(t)
	fixture := newInteractionFixtureForTemplate(t, harness, target, name)
	probe(fixture, false)
	grant := &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{GenerateName: "additional-access-", Namespace: "kagent"}, Spec: v1alpha3.EgressPolicySpec{
		Destinations: []v1alpha3.EgressDestination{{Origin: origin}},
	}}
	require.NoError(t, kube.Create(t.Context(), grant))
	t.Cleanup(func() { require.NoError(t, ctrlclient.IgnoreNotFound(kube.Delete(context.Background(), grant))) })
	agent := &v1alpha3.Agent{}
	key := ctrlclient.ObjectKey{Namespace: "kagent", Name: name}
	require.NoError(t, kube.Get(t.Context(), key, agent))
	agent.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: grant.Name}}
	require.NoError(t, kube.Update(t.Context(), agent))
	require.NoError(t, wait.PollUntilContextTimeout(t.Context(), time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := kube.Get(ctx, key, agent); err != nil {
			return false, err
		}
		condition := apimeta.FindStatusCondition(agent.Status.Conditions, v1alpha3.AgentConditionReady)
		return condition != nil && condition.ObservedGeneration == agent.Generation && condition.Status == metav1.ConditionTrue, nil
	}))
	allowed := newInteractionFixtureForTemplate(t, harness, target, name)
	probe(allowed, true)
	probe(fixture, false) // Existing sessions retain their earlier grants.

	require.NoError(t, kube.Delete(t.Context(), grant))
	require.NoError(t, wait.PollUntilContextTimeout(t.Context(), time.Second, time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := kube.Get(ctx, key, agent); err != nil {
			return false, err
		}
		condition := apimeta.FindStatusCondition(agent.Status.Conditions, v1alpha3.AgentConditionReady)
		return condition != nil && condition.ObservedGeneration == agent.Generation && condition.Status == metav1.ConditionFalse, nil
	}))
	// Check creation directly, without the fixture's preparation retry loop.
	_, err := allowed.sessions.CreateSession(allowed.ctx, &apiv1.CreateSessionRequest{
		Agent: &apiv1.ResourceReference{Namespace: key.Namespace, Name: key.Name}, RequestId: uuid.NewString(),
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	probe(allowed, true) // Deletion preserves the existing session's pinned grant.
}

func TestSandboxEgressPolicyDeletionBlocksCreation(t *testing.T) {
	t.Parallel()
	f := newSandboxFixture(t)
	kube := interactionKubeClient(t)
	grant := &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{GenerateName: "sandbox-access-", Namespace: f.template.Namespace}, Spec: v1alpha3.EgressPolicySpec{
		Destinations: []v1alpha3.EgressDestination{{Origin: "https://example.com"}},
	}}
	require.NoError(t, kube.Create(f.ctx, grant))
	t.Cleanup(func() { require.NoError(t, ctrlclient.IgnoreNotFound(kube.Delete(context.Background(), grant))) })
	template := &v1alpha3.SandboxTemplate{}
	key := ctrlclient.ObjectKey{Namespace: f.template.Namespace, Name: f.template.Name}
	require.NoError(t, kube.Get(f.ctx, key, template))
	template.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: grant.Name}}
	require.NoError(t, kube.Update(f.ctx, template))
	// Observe the grant's generation before creating the instance to avoid
	// selecting the template's earlier revision during reconciliation.
	waitReady := func(want metav1.ConditionStatus) {
		t.Helper()
		require.NoError(t, wait.PollUntilContextTimeout(f.ctx, time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
			if err := kube.Get(ctx, key, template); err != nil {
				return false, err
			}
			condition := apimeta.FindStatusCondition(template.Status.Conditions, "Ready")
			return condition != nil && condition.ObservedGeneration == template.Generation && condition.Status == want, nil
		}))
	}
	waitReady(metav1.ConditionTrue)
	first := f.create(t, 5*time.Minute)
	require.NoError(t, kube.Delete(f.ctx, grant))
	waitReady(metav1.ConditionFalse)
	_, err := f.client.CreateSandbox(f.ctx, &apiv1.CreateSandboxRequest{SandboxTemplate: f.template, RequestId: uuid.NewString()})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	current, err := f.client.GetSandbox(f.ctx, &apiv1.GetSandboxRequest{SandboxId: first.Id})
	require.NoError(t, err)
	require.Equal(t, first.PreparedRevision, current.Sandbox.PreparedRevision)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, current.Sandbox.State)
}
