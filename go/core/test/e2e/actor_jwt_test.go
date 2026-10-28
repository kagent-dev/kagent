package e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

func TestModelActorJWTInjection(t *testing.T) {
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		target := interactionTarget(t)
		kube := interactionKubeClient(t)
		recorder := startModelRecorder(t, mockLLMProxy(t, startMockLLMServer(t, interactionMocks, "mocks/invoke_agent.json")), nil)
		model := harness.createModel(t, kube, reachableModelURL(t, recorder.URL), nil)
		base := model.DeepCopy()
		model.Spec.APIKeySecret, model.Spec.APIKeySecretKey = "", ""
		model.Spec.ActorJWT = &v1alpha3.ActorJWT{Audiences: []string{"model-e2e"}, ExpirationSeconds: new(int64(600))}
		require.NoError(t, kube.Patch(t.Context(), model, ctrlclient.MergeFrom(base)))
		template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{GenerateName: "actor-jwt-", Namespace: "kagent", Labels: harness.labels()}, Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig: &corev1.LocalObjectReference{Name: model.Name}, SystemPrompt: "Reply briefly.",
		}}
		createAndWaitInteractionTemplate(t, harness, kube, template)
		fixture := newInteractionFixtureForTemplate(t, harness, target, template.Name)
		_, _, task := fixture.send(t, "What is 2+2?")
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
		actor, err := findSubstrateActor(fixture.ctx, fixture.system, "kagent", substrate.ActorName(fixture.sessionID))
		require.NoError(t, err)
		recorder.mu.Lock()
		requests := append([]recordedModelRequest(nil), recorder.requests...)
		recorder.mu.Unlock()
		require.NotEmpty(t, requests)
		modelCalls := 0
		for _, request := range requests {
			// Claude also probes the endpoint without sending credentials.
			if request.Method != http.MethodPost {
				continue
			}
			modelCalls++
			token := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
			if harness.name == claudeE2EHarness {
				token = request.Header.Get("X-Api-Key")
			}
			assertInjectedActorJWT(t, token, "model-e2e", actor.GetMetadata().GetUid())
		}
		require.Positive(t, modelCalls, "the runtime must call the model")
	})
}

func TestMCPActorJWTInjection(t *testing.T) {
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		target := interactionTarget(t)
		mcpURL, server := startMCPMock(t)
		template, _ := createMCPInteractionTemplate(t, harness, mcpURL, false, &v1alpha3.ActorJWT{
			Audiences: []string{"mcp-e2e"}, ExpirationSeconds: new(int64(600)),
		})
		require.Empty(t, server.Requests(), "Actor-authenticated discovery must wait for the runtime")
		fixture := newInteractionFixtureForTemplate(t, harness, target, template)
		_, _, task := fixture.send(t, "Add 3 and 5 using the configured MCP server.")
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
		require.Contains(t, taskText(task), "result is 8")
		actor, err := findSubstrateActor(fixture.ctx, fixture.system, "kagent", substrate.ActorName(fixture.sessionID))
		require.NoError(t, err)
		toolCalls := 0
		for _, request := range server.Requests() {
			if bytes.Contains(request.Body, []byte(`"method":"tools/call"`)) {
				toolCalls++
				assertInjectedActorJWT(t, strings.TrimPrefix(request.Headers.Get("Authorization"), "Bearer "), "mcp-e2e", actor.GetMetadata().GetUid())
			}
		}
		require.Positive(t, toolCalls, "the runtime must call the MCP tool")
	})
}

func TestEgressPolicyActorJWTInjection(t *testing.T) {
	f := newSandboxFixture(t)
	kube := interactionKubeClient(t)
	recorder := startModelRecorder(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "received") }), nil)
	origin := reachableServerURL(t, recorder.URL, "")
	policy := &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{GenerateName: "actor-jwt-", Namespace: f.template.Namespace}, Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: origin, Authentication: &v1alpha3.EgressAuthentication{
		Header: "X-Actor", Prefix: new("Bearer "), ActorJWT: &v1alpha3.ActorJWT{Audiences: []string{"egress-e2e"}, ExpirationSeconds: new(int64(600))},
	}}}}}
	require.NoError(t, kube.Create(f.ctx, policy))
	t.Cleanup(func() { require.NoError(t, ctrlclient.IgnoreNotFound(kube.Delete(context.Background(), policy))) })
	template := &v1alpha3.SandboxTemplate{}
	require.NoError(t, kube.Get(f.ctx, ctrlclient.ObjectKey{Namespace: f.template.Namespace, Name: f.template.Name}, template))
	base := template.DeepCopy()
	template.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: policy.Name}}
	require.NoError(t, kube.Patch(f.ctx, template, ctrlclient.MergeFrom(base)))
	instance := f.create(t, 5*time.Minute)
	actor, err := findSubstrateActor(f.ctx, f.system, f.template.Namespace, substrate.ActorName(instance.Id))
	require.NoError(t, err)
	process, err := f.processes.StartProcess(f.guestContext(instance.Id), &guestpb.StartProcessRequest{Command: []string{"sh", "-c", fmt.Sprintf("wget -T 5 -qO- --header 'X-Actor: placeholder' '%s'", origin)}})
	require.NoError(t, err)
	outputs, err := f.processes.StreamProcessOutput(f.guestContext(instance.Id), &guestpb.StreamProcessOutputRequest{ProcessId: process.ProcessId, Follow: true})
	require.NoError(t, err)
	var output strings.Builder
	for {
		chunk, err := outputs.Recv()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		output.Write(chunk.GetStdout())
	}
	require.Contains(t, output.String(), "received")
	recorder.mu.Lock()
	requests := append([]recordedModelRequest(nil), recorder.requests...)
	recorder.mu.Unlock()
	require.NotEmpty(t, requests)
	for _, request := range requests {
		assertInjectedActorJWT(t, strings.TrimPrefix(request.Header.Get("X-Actor"), "Bearer "), "egress-e2e", actor.GetMetadata().GetUid())
	}
}

// Inspect gateway output here; signature and issuer verification have their own
// verifier tests. This checks that placeholders become the executing Actor's JWT.
func assertInjectedActorJWT(t *testing.T, token, audience, actorUID string) {
	t.Helper()
	claims := struct {
		jwt.RegisteredClaims
		Actor substrate.ActorIdentity `json:"actor.ate.dev"`
	}{}
	_, _, err := jwt.NewParser().ParseUnverified(token, &claims)
	require.NoError(t, err)
	require.Equal(t, actorUID, claims.Actor.ActorUID)
	require.Equal(t, jwt.ClaimStrings{audience}, claims.Audience)
	require.NotNil(t, claims.IssuedAt)
	require.NotNil(t, claims.ExpiresAt)
	require.Equal(t, 600*time.Second, claims.ExpiresAt.Sub(claims.IssuedAt.Time))
}
