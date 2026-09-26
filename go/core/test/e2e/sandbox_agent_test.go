package e2e_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/mockllm"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestSandboxAgentMCP(t *testing.T) {
	image := os.Getenv("KAGENT_E2E_RUNTIME_IMAGE")
	if image == "" {
		t.Skip("KAGENT_E2E_RUNTIME_IMAGE is not set")
	}
	f := newSandboxFixture(t)
	prepared := f.create(t, 5*time.Minute)
	_, err := f.client.DeleteSandbox(f.ctx, &apiv1alpha1.DeleteSandboxRequest{SandboxId: prepared.Id})
	require.NoError(t, err)
	kube := interactionKubeClient(t)
	source := &v1alpha3.SandboxTemplate{}
	require.NoError(t, kube.Get(f.ctx, ctrlclient.ObjectKey{Namespace: f.template.Namespace, Name: f.template.Name}, source))
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "scratch-agent-", Namespace: f.template.Namespace},
		Spec: v1alpha3.HarnessSpec{Kagent: &v1alpha3.KagentHarness{}, Workload: v1alpha3.HarnessWorkload{Image: image}, Substrate: source.Spec.Substrate,
			Env: []v1alpha3.HarnessEnvVar{{Name: "KAGENT_PROPAGATE_TOKEN", Value: new("true")}}},
	}
	require.NoError(t, kube.Create(f.ctx, harness))
	t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), harness)) })
	cfg, err := mockllm.LoadConfigFromFile("mocks/invoke_mcp_agent.json", interactionMocks)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(`{"role":"user","content":"Create a scratch sandbox."}`), &cfg.OpenAI[0].Match.Message))
	arguments, err := json.Marshal(struct {
		Namespace string `json:"namespace"`
		Template  string `json:"template"`
		RequestID string `json:"request_id"`
		TTL       int    `json:"ttl_seconds"`
	}{f.template.Namespace, f.template.Name, uuid.NewString(), 120})
	require.NoError(t, err)
	call := &cfg.OpenAI[0].Response.Choices[0].Message.ToolCalls[0]
	call.Function.Name, call.Function.Arguments = "create_sandbox", string(arguments)
	require.NoError(t, json.Unmarshal([]byte(`{"role":"tool","content":"sandbox_template","tool_call_id":"call_1"}`), &cfg.OpenAI[1].Match.Message))
	cfg.OpenAI[1].Response.Choices[0].Message.Content = "Created the scratch sandbox."
	model := &v1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "scratch-model-", Namespace: f.template.Namespace},
		Spec:       v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-4.1-mini", OpenAI: &v1alpha3.OpenAIConfig{BaseURL: reachableModelURL(t, startMockLLMConfig(t, cfg))}},
	}
	require.NoError(t, kube.Create(f.ctx, model))
	t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), model)) })
	endpoint := os.Getenv("KAGENT_E2E_SANDBOX_MCP_URL")
	if endpoint == "" {
		endpoint = "http://kagent-controller." + f.template.Namespace + ".svc:8083/mcp"
	}
	server := &v1alpha3.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "scratch-tools-", Namespace: f.template.Namespace},
		Spec:       v1alpha3.RemoteMCPServerSpec{Description: "Sandbox tools", Protocol: v1alpha3.RemoteMCPServerProtocolStreamableHttp, URL: endpoint},
	}
	require.NoError(t, kube.Create(f.ctx, server))
	t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), server)) })
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "scratch-caller-", Namespace: f.template.Namespace},
		Spec: v1alpha3.AgentTemplateSpec{
			Description: "Scratch sandbox MCP caller", SystemPrompt: "Use create_sandbox to create the requested workspace.", ModelConfig: &corev1.LocalObjectReference{Name: model.Name},
			Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{Server: corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: server.Name}, Tools: []string{"create_sandbox"}}}},
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, harness.Name)
	conn, err := grpc.NewClient(interactionTarget(t), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	instances := apiv1alpha1.NewSessionServiceClient(conn)
	created, err := instances.CreateSession(f.ctx, &apiv1alpha1.CreateSessionRequest{
		Agent: &apiv1alpha1.ResourceReference{Namespace: template.Namespace, Name: template.Name}, RequestId: uuid.NewString(),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(f.ctx), time.Minute)
		defer cancel()
		err := deleteIdleSession(ctx, instances, created.Session.Id)
		require.NoError(t, err)
	})
	agent := &interactionFixture{ctx: f.ctx, client: a2apb.NewA2AServiceClient(conn), tenant: template.Namespace + "/" + template.Name, sessionID: created.Session.Id, contextID: created.Session.ContextId}
	_, _, task := agent.send(t, "Create a scratch sandbox.")
	require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, "%s", taskText(task))
	require.Contains(t, taskText(task), "Created the scratch sandbox")
	owned, err := f.client.ListSandboxes(f.ctx, &apiv1alpha1.ListSandboxesRequest{SandboxTemplate: f.template})
	require.NoError(t, err)
	require.Len(t, owned.Sandboxes, 1, "MCP must create a sandbox for the invoking user")
	for _, instance := range owned.Sandboxes {
		require.Equal(t, "e2e", instance.Creator)
		_, err := f.client.DeleteSandbox(f.ctx, &apiv1alpha1.DeleteSandboxRequest{SandboxId: instance.Id})
		require.NoError(t, err)
	}
}
