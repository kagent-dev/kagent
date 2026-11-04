package mcp

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiauthorization "github.com/kagent-dev/kagent/go/api/authorization"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func testAgentService(t *testing.T) *kubecrud.Service[*v1alpha3.Agent, *v1alpha3.AgentList] {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha3.AddToScheme(scheme))
	kube := fake.NewClientBuilder().WithScheme(scheme).Build()
	return kubecrud.NewService(kube, auth.NoopAuthorizer{}, &v1alpha3.Agent{}, &v1alpha3.AgentList{}, "Agent")
}

func TestListAgents(t *testing.T) {
	all := apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAll}
	visible := apiauthorization.AuthorizationScope{
		Kind: apiauthorization.ScopeAnyOf,
		AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{
			{Attribute: apiauthorization.AttributeNamespace, Operator: apiauthorization.ScopeIn, Values: []string{"team-a"}},
			{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{"a", "c"}},
		}}},
	}
	a, b, c := agentReference{Namespace: "team-a", Name: "a"}, agentReference{Namespace: "team-a", Name: "b"}, agentReference{Namespace: "team-a", Name: "c"}
	other := agentReference{Namespace: "team-b", Name: "a"}
	afterA := base64.RawURLEncoding.EncodeToString([]byte("team-a/a"))
	for _, test := range []struct {
		name      string
		scope     apiauthorization.AuthorizationScope
		scopeErr  error
		input     listAgentsInput
		want      listAgentsOutput
		wantError bool
	}{
		{name: "all namespaces", scope: all, want: listAgentsOutput{Agents: []agentReference{a, b, c, other}}},
		{name: "namespace filter", scope: all, input: listAgentsInput{Namespace: "team-b"}, want: listAgentsOutput{Agents: []agentReference{other}}},
		{name: "empty namespace", scope: all, input: listAgentsInput{Namespace: "empty"}, want: listAgentsOutput{Agents: []agentReference{}}},
		{name: "scope filters before paging", scope: visible, input: listAgentsInput{PageSize: 1}, want: listAgentsOutput{Agents: []agentReference{a}, NextPageToken: afterA}},
		{name: "continue authorized page", scope: visible, input: listAgentsInput{PageSize: 1, PageToken: afterA}, want: listAgentsOutput{Agents: []agentReference{c}}},
		{name: "namespace cannot widen scope", scope: visible, input: listAgentsInput{Namespace: "team-b"}, want: listAgentsOutput{Agents: []agentReference{}}},
		{name: "no permission", scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeNone}, want: listAgentsOutput{Agents: []agentReference{}}},
		{name: "malformed scope", scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf}, wantError: true},
		{name: "scope error", scopeErr: errors.New("authorization unavailable"), wantError: true},
		{name: "invalid page token", scope: all, input: listAgentsInput{PageToken: "invalid"}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, v1alpha3.AddToScheme(scheme))
			// No Session or readiness status is needed to discover an Agent.
			kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
				&v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: c.Namespace, Name: c.Name}},
				&v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: other.Namespace, Name: other.Name}},
				&v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: b.Namespace, Name: b.Name}},
				&v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: a.Namespace, Name: a.Name}},
			).Build()
			authorizer := &collectionListAuthorizer{scope: test.scope, scopeErr: test.scopeErr}
			agents := kubecrud.NewService(kube, authorizer, &v1alpha3.Agent{}, &v1alpha3.AgentList{}, "Agent")
			handler, err := New(Config{Agents: agents, Sessions: testSessionService(), Checkpoints: testCheckpointService(), Gateway: &fakeGateway{}})
			require.NoError(t, err)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handler.ServeHTTP(w, r.WithContext(auth.AuthSessionTo(r.Context(), testSession{})))
			}))
			t.Cleanup(server.Close)
			response := rawMCPCall(t, server.URL, "tools/call", map[string]any{
				"name": "list_agents", "arguments": test.input,
			}, false)
			result := response["result"].(map[string]any)
			require.Equal(t, auth.VerbList, authorizer.verb)
			require.Equal(t, "Agent", authorizer.resourceType)
			if test.wantError {
				require.Equal(t, true, result["isError"])
				require.Empty(t, result["structuredContent"].(map[string]any)["agents"])
				return
			}
			require.NotEqual(t, true, result["isError"])
			encoded, err := json.Marshal(result["structuredContent"])
			require.NoError(t, err)
			var output listAgentsOutput
			require.NoError(t, json.Unmarshal(encoded, &output))
			require.Equal(t, test.want, output)
			// Text-only clients must receive the same references and page token.
			content := result["content"].([]any)
			require.JSONEq(t, string(encoded), content[0].(map[string]any)["text"].(string))
		})
	}
}

func TestListAgentsValidatesBeforeDispatch(t *testing.T) {
	// Missing dependencies prove malformed filters do not reach Kubernetes or authorization.
	handler, err := New(Config{
		Agents:   &kubecrud.Service[*v1alpha3.Agent, *v1alpha3.AgentList]{},
		Sessions: testSessionService(), Checkpoints: testCheckpointService(), Gateway: &fakeGateway{},
	})
	require.NoError(t, err)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	for _, input := range []listAgentsInput{
		{Namespace: "../team"}, {PageSize: -1}, {PageSize: 101},
	} {
		response := rawMCPCall(t, server.URL, "tools/call", map[string]any{"name": "list_agents", "arguments": input}, false)
		require.Equal(t, true, response["result"].(map[string]any)["isError"])
	}
	// A valid request still needs authentication before accessing Kubernetes.
	response := rawMCPCall(t, server.URL, "tools/call", map[string]any{"name": "list_agents", "arguments": listAgentsInput{}}, false)
	require.Equal(t, true, response["result"].(map[string]any)["isError"])

	listed := rawMCPCall(t, server.URL, "tools/list", map[string]any{}, false)
	for _, value := range listed["result"].(map[string]any)["tools"].([]any) {
		tool := value.(map[string]any)
		if tool["name"] == "list_agents" {
			require.Equal(t, true, tool["annotations"].(map[string]any)["readOnlyHint"])
			return
		}
	}
	t.Fatal("list_agents not registered")
}
