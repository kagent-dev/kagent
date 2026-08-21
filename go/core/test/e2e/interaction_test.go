package e2e_test

import (
	"context"
	"embed"
	"net"
	"net/url"
	"os"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/mockllm"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

//go:embed mocks/invoke_golang_adk_agent.json
var interactionMocks embed.FS

// TestAgentInstanceInteraction verifies the complete public interaction path:
// gateway routing, Substrate Actor transport, Go ADK execution, and the model call.
func TestAgentInstanceInteraction(t *testing.T) {
	target := os.Getenv("KAGENT_E2E_GRPC_TARGET")
	if target == "" {
		target = os.Getenv("KAGENT_GRPC_URL")
	}
	if target == "" {
		t.Skip("KAGENT_E2E_GRPC_TARGET is not set")
	}

	modelURL := startInteractionMock(t)
	templateName := createInteractionTemplate(t, modelURL)

	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("connect to kagent gRPC API: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "e2e"), 4*time.Minute)
	defer cancel()
	instances := apiv1alpha1.NewAgentInstanceServiceClient(conn)
	created, err := instances.CreateAgentInstance(ctx, &apiv1alpha1.CreateAgentInstanceRequest{
		Namespace: "kagent", AgentTemplate: templateName, Harness: "kagent", RequestId: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create AgentInstance: %v", err)
	}
	instance := created.GetAgentInstance()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cleanupCancel()
		_, cleanupErr := instances.DeleteAgentInstance(cleanupCtx, &apiv1alpha1.DeleteAgentInstanceRequest{
			Namespace: "kagent", AgentInstanceId: instance.GetId(),
		})
		if cleanupErr != nil && status.Code(cleanupErr) != codes.NotFound {
			t.Errorf("delete AgentInstance: %v", cleanupErr)
		}
	})
	if instance.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY {
		t.Fatalf("created AgentInstance state = %s, want READY", instance.GetState())
	}

	message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("What is 2+2?"))
	request, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{
		Message: message,
	})
	if err != nil {
		t.Fatalf("build A2A request: %v", err)
	}
	interactionCtx := metadata.AppendToOutgoingContext(ctx,
		"x-kagent-agent-instance-namespace", "kagent",
		"x-kagent-agent-instance-id", instance.GetId(),
	)
	a2aClient := a2apb.NewA2AServiceClient(conn)
	response, err := a2aClient.SendMessage(interactionCtx, request)
	if err != nil {
		t.Fatalf("send A2A message: %v", err)
	}
	result, err := pbconv.FromProtoSendMessageResponse(response)
	if err != nil {
		t.Fatalf("decode A2A response: %v", err)
	}
	task, ok := result.(*a2atype.Task)
	if !ok {
		t.Fatalf("A2A response = %T, want Task", result)
	}
	if task.Status.State != a2atype.TaskStateCompleted {
		t.Fatalf("A2A task state = %s, want COMPLETED", task.Status.State)
	}
	if text := taskText(task); !strings.Contains(text, "The answer is 4.") {
		t.Fatalf("A2A response text = %q, want mock LLM response", text)
	}

	getRequest, err := pbconv.ToProtoGetTaskRequest(&a2atype.GetTaskRequest{ID: task.ID})
	if err != nil {
		t.Fatalf("build GetTask request: %v", err)
	}
	gotProto, err := a2aClient.GetTask(interactionCtx, getRequest)
	if err != nil {
		t.Fatalf("get persisted task: %v", err)
	}
	got, err := pbconv.FromProtoTask(gotProto)
	if err != nil {
		t.Fatalf("decode persisted task: %v", err)
	}
	if got.ID != task.ID || got.ContextID != instance.GetId() || got.Status.State != a2atype.TaskStateCompleted {
		t.Fatalf("persisted task = %#v, want completed task %s in context %s", got, task.ID, instance.GetId())
	}

	listRequest, err := pbconv.ToProtoListTasksRequest(&a2atype.ListTasksRequest{ContextID: instance.GetId()})
	if err != nil {
		t.Fatalf("build ListTasks request: %v", err)
	}
	listedProto, err := a2aClient.ListTasks(interactionCtx, listRequest)
	if err != nil {
		t.Fatalf("list persisted tasks: %v", err)
	}
	listed, err := pbconv.FromProtoListTasksResponse(listedProto)
	if err != nil {
		t.Fatalf("decode listed tasks: %v", err)
	}
	if listed.TotalSize != 1 || len(listed.Tasks) != 1 || listed.Tasks[0].ID != task.ID || listed.Tasks[0].ContextID != instance.GetId() {
		t.Fatalf("listed tasks = %#v, want only task %s in context %s", listed, task.ID, instance.GetId())
	}

	replayedProto, err := a2aClient.SendMessage(interactionCtx, request)
	if err != nil {
		t.Fatalf("replay A2A message: %v", err)
	}
	replayed, err := pbconv.FromProtoSendMessageResponse(replayedProto)
	if err != nil {
		t.Fatalf("decode replayed response: %v", err)
	}
	replayedTask, ok := replayed.(*a2atype.Task)
	if !ok || replayedTask.ID != task.ID {
		t.Fatalf("replayed response = %#v, want task %s", replayed, task.ID)
	}

	conflictingMessage := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("What is 3+3?"))
	conflictingMessage.ID = message.ID
	conflictingRequest, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: conflictingMessage})
	if err != nil {
		t.Fatalf("build conflicting A2A request: %v", err)
	}
	if _, err := a2aClient.SendMessage(interactionCtx, conflictingRequest); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("conflicting message error = %v, want %s", err, codes.InvalidArgument)
	}
}

func startInteractionMock(t *testing.T) string {
	t.Helper()
	cfg, err := mockllm.LoadConfigFromFile("mocks/invoke_golang_adk_agent.json", interactionMocks)
	if err != nil {
		t.Fatalf("load mock LLM response: %v", err)
	}
	server := mockllm.NewServer(cfg)
	baseURL, err := server.Start(t.Context())
	if err != nil {
		t.Fatalf("start mock LLM: %v", err)
	}
	t.Cleanup(func() {
		if err := server.Stop(context.Background()); err != nil {
			t.Errorf("stop mock LLM: %v", err)
		}
	})

	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse mock LLM URL: %v", err)
	}
	_, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("parse mock LLM address: %v", err)
	}
	host := os.Getenv("KAGENT_LOCAL_HOST")
	if host == "" {
		switch goruntime.GOOS {
		case "darwin":
			host = "host.docker.internal"
		case "linux":
			host = "172.17.0.1"
		default:
			t.Fatalf("KAGENT_LOCAL_HOST is required on %s", goruntime.GOOS)
		}
	}
	parsed.Host = net.JoinHostPort(host, port)
	return strings.TrimSuffix(parsed.String(), "/") + "/v1"
}

func createInteractionTemplate(t *testing.T, modelURL string) string {
	t.Helper()
	cfg, err := config.GetConfig()
	if err != nil {
		t.Fatalf("load Kubernetes config: %v", err)
	}
	clientScheme := k8sruntime.NewScheme()
	if err := v1alpha3.AddToScheme(clientScheme); err != nil {
		t.Fatalf("register kagent API: %v", err)
	}
	kube, err := ctrlclient.New(cfg, ctrlclient.Options{Scheme: clientScheme})
	if err != nil {
		t.Fatalf("create Kubernetes client: %v", err)
	}

	model := &v1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "interaction-", Namespace: "kagent"},
		Spec: v1alpha3.ModelConfigSpec{
			Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-4.1-mini",
			APIKeySecret: "kagent-openai", APIKeySecretKey: "OPENAI_API_KEY",
			OpenAI: &v1alpha3.OpenAIConfig{BaseURL: modelURL},
		},
	}
	if err := kube.Create(t.Context(), model); err != nil {
		t.Fatalf("create interaction ModelConfig: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), model); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete interaction ModelConfig: %v", err)
		}
	})

	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "interaction-", Namespace: "kagent",
			Labels: map[string]string{"kagent.dev/e2e-runtime": "kagent"},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  v1alpha3.AgentTemplateLocalReference{Name: model.Name},
			Description:  "Agent interaction E2E fixture",
			SystemPrompt: "Reply briefly.",
		},
	}
	if err := kube.Create(t.Context(), template); err != nil {
		t.Fatalf("create interaction AgentTemplate: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), template); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete interaction AgentTemplate: %v", err)
		}
	})

	err = wait.PollUntilContextTimeout(t.Context(), time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(template), template); err != nil {
			return false, err
		}
		for _, harness := range template.Status.Harnesses {
			if harness.Harness != "kagent" {
				continue
			}
			for _, condition := range harness.Conditions {
				if condition.Type == v1alpha3.AgentTemplateConditionReady && condition.Status == metav1.ConditionTrue {
					return true, nil
				}
			}
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("wait for interaction AgentTemplate: %v", err)
	}
	return template.Name
}

func taskText(task *a2atype.Task) string {
	var parts []string
	if task.Status.Message != nil {
		for _, part := range task.Status.Message.Parts {
			parts = append(parts, part.Text())
		}
	}
	for _, artifact := range task.Artifacts {
		for _, part := range artifact.Parts {
			parts = append(parts, part.Text())
		}
	}
	return strings.Join(parts, "\n")
}
