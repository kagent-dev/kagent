package e2e_test

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	adka2a "github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	kagenta2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/mockllm"
	"github.com/kagent-dev/mockmcp"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

//go:embed mocks/invoke_agent.json mocks/invoke_golang_hitl_ask_user.json mocks/invoke_mcp_agent.json mocks/invoke_shared_agent.json mocks/invoke_structured_output.json
var interactionMocks embed.FS

const structuredOutputSchema = `{"type":"object","properties":{"answer":{"type":"integer"},"explanation":{"type":"string"}},"required":["answer","explanation"],"additionalProperties":false}`

// TestSessionInteraction verifies the complete public interaction path:
// gateway routing, Substrate Actor transport, harness execution, and the model call.
func TestSessionInteraction(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		fixture := newInteractionFixture(t, harness, interactionTarget(t), startInteractionMock(t))
		_, _, task := fixture.send(t, "What is 2+2?")
		if task.Status.State != a2atype.TaskStateCompleted {
			t.Fatalf("A2A task state = %s, text = %q, want COMPLETED", task.Status.State, taskText(task))
		}
		if text := taskText(task); !strings.Contains(text, "The answer is 4.") {
			t.Fatalf("A2A response text = %q, want mock LLM response", text)
		}
		// Completion is visible before idle suspension finishes. Wait separately
		// to verify that later traffic wakes the same Actor for the next task.
		assertActorSuspended(t, fixture)
		_, _, task = fixture.send(t, "What is 2+2?")
		if task.Status.State != a2atype.TaskStateCompleted {
			t.Fatalf("second A2A task state = %s, text = %q, want COMPLETED", task.Status.State, taskText(task))
		}
	})
}

func TestSessionStructuredOutput(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		switch harness.name {
		case codexE2EHarness, claudeE2EHarness, "byo-adk-e2e":
			t.Skip("the compiler only supports AgentTemplate outputSchema on the kagent harness")
		}
		target := interactionTarget(t)
		kube := interactionKubeClient(t)
		mcpURL, mcpServer := startMCPMock(t)
		template := createStructuredOutputInteractionTemplate(t, harness, kube, startMockLLM(t, "mocks/invoke_structured_output.json"), mcpURL)
		fixture := newInteractionFixtureForHarnessTemplate(t, target, harness.name, template.Name)
		_, _, task := fixture.send(t, "Add 3 and 5 and return the structured result.")
		assertStructuredOutputTask(t, task, 8, "three plus five equals eight")

		for _, request := range mcpServer.Requests() {
			if bytes.Contains(request.Body, []byte(`"method":"tools/call"`)) && bytes.Contains(request.Body, []byte(`"name":"add_numbers"`)) {
				return
			}
		}
		t.Fatal("mock MCP server did not receive the add_numbers call used by the structured response")
	})
}

func assertStructuredOutputTask(t *testing.T, task *a2atype.Task, answer float64, explanation string) {
	t.Helper()
	if task.Status.State != a2atype.TaskStateCompleted {
		t.Fatalf("A2A task state = %s, want COMPLETED", task.Status.State)
	}
	if len(task.Artifacts) == 0 {
		t.Fatal("structured task has no result artifact")
	}
	assertStructuredOutputArtifact(t, task.Artifacts[len(task.Artifacts)-1], answer, explanation)
}

func assertStructuredOutputArtifact(t *testing.T, artifact *a2atype.Artifact, answer float64, explanation string) {
	t.Helper()
	if artifact == nil {
		t.Fatal("structured result artifact is nil")
	}
	if len(artifact.Parts) != 1 {
		t.Fatalf("structured result has %d parts, want 1", len(artifact.Parts))
	}
	part := artifact.Parts[0]
	data, ok := part.Data().(map[string]any)
	if !ok || data["answer"] != answer || data["explanation"] != explanation {
		t.Fatalf("structured result data = %#v", part.Data())
	}
	if part.MediaType != "application/json" {
		t.Fatalf("structured result media type = %q", part.MediaType)
	}
	if got, ok := kagenta2a.StructuredOutputSchemaSHA256(part); !ok || len(got) != 64 {
		t.Fatalf("structured result schema digest = %#v", got)
	}
}

func TestOpaqueBYOAgentInteraction(t *testing.T) {
	fixture := newInteractionFixtureForHarnessTemplate(t, interactionTarget(t), "byo-e2e", "byo-smoke")
	for range 2 {
		_, _, task := fixture.send(t, "hello")
		if task.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(task), "BYO agent response") {
			t.Fatalf("BYO A2A task = %+v", task)
		}
	}
}

func TestSessionAskUserSurvivesSuspension(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		switch harness.name {
		case codexE2EHarness, claudeE2EHarness:
			t.Skip("native ask-user model fixtures are not available yet; this fixture calls the Go ADK ask_user tool")
		}
		fixture := newInteractionFixture(t, harness, interactionTarget(t), startMockLLM(t, "mocks/invoke_golang_hitl_ask_user.json"))
		fixture.ctx = metadata.AppendToOutgoingContext(fixture.ctx, strings.ToLower(a2atype.SvcParamExtensions), adka2a.HITLExtensionURI)
		_, _, waiting := fixture.send(t, "Which database should we use for storage?")
		if waiting.Status.State != a2atype.TaskStateInputRequired {
			t.Fatalf("A2A task state = %s, want INPUT_REQUIRED", waiting.Status.State)
		}
		request := adka2a.GetAskUserRequest(waiting.Status.Message)
		if request == nil {
			t.Fatal("INPUT_REQUIRED task has no ask_user request")
		}
		reply := adka2a.AttachHitlExtension(a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("PostgreSQL")), &kagenta2a.AskUserResponse{
			Type: adka2a.HITLTypeAskUserResponse, ID: request.ID,
			Answers: []kagenta2a.AskUserAnswer{{Answer: []string{"PostgreSQL"}}},
		})
		reply.TaskID, reply.ContextID = waiting.ID, waiting.ContextID
		response, err := a2agrpc.NewGRPCTransportFromClient(fixture.client).SendMessage(fixture.ctx, nil, &a2atype.SendMessageRequest{Tenant: fixture.tenant, Message: reply})
		if err != nil {
			t.Fatalf("resume A2A task: %v", err)
		}
		completed, ok := response.(*a2atype.Task)
		if !ok || completed.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(completed), "Using PostgreSQL") {
			t.Fatalf("resumed A2A task = %#v, want completed PostgreSQL response", response)
		}
	})
}

func sendApprovedToolRequest(t *testing.T, fixture *interactionFixture, prompt, wantTool string) *a2atype.Task {
	t.Helper()
	fixture.ctx = metadata.AppendToOutgoingContext(fixture.ctx, strings.ToLower(a2atype.SvcParamExtensions), kagenta2a.HITLExtensionURI)
	_, _, waiting := fixture.send(t, prompt)
	if waiting.Status.State != a2atype.TaskStateInputRequired {
		t.Fatalf("A2A task state = %s, want INPUT_REQUIRED", waiting.Status.State)
	}
	request, err := kagenta2a.ParseToolApprovalRequest(waiting.Status.Message)
	if err != nil {
		t.Fatalf("parse tool approval request: %v", err)
	}
	if request == nil {
		t.Fatal("INPUT_REQUIRED task has no tool approval request")
	}
	if len(request.Tools) != 1 || request.Tools[0].Name != wantTool {
		t.Fatalf("tool approval request = %+v, want one request for %q", request.Tools, wantTool)
	}

	reply := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("Approved"))
	reply.TaskID, reply.ContextID = waiting.ID, waiting.ContextID
	if err := kagenta2a.AttachHITL(reply, kagenta2a.ToolApprovalResponse{
		Type: kagenta2a.HITLTypeToolApprovalResponse,
		Approvals: []kagenta2a.ToolApproval{{
			ID:       request.Tools[0].ID,
			Approved: true,
		}},
	}); err != nil {
		t.Fatalf("attach tool approval response: %v", err)
	}
	response, err := a2agrpc.NewGRPCTransportFromClient(fixture.client).SendMessage(fixture.ctx, nil, &a2atype.SendMessageRequest{Tenant: fixture.tenant, Message: reply})
	if err != nil {
		t.Fatalf("resume A2A task after tool approval: %v", err)
	}
	completed, ok := response.(*a2atype.Task)
	if !ok {
		t.Fatalf("resumed A2A response = %T, want Task", response)
	}
	if completed.ID != waiting.ID || completed.ContextID != waiting.ContextID {
		t.Fatalf("resumed task = %s/%s, want %s/%s", completed.ContextID, completed.ID, waiting.ContextID, waiting.ID)
	}
	return completed
}

// createCheckpoint waits for independent idle snapshot work using one idempotent
// request. Task completion itself no longer promises checkpoint readiness.
func createCheckpoint(t *testing.T, ctx context.Context, client apiv1alpha1.CheckpointServiceClient, request *apiv1alpha1.CreateCheckpointRequest) *apiv1alpha1.CreateCheckpointResponse {
	t.Helper()
	var result *apiv1alpha1.CreateCheckpointResponse
	err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		var err error
		result, err = client.CreateCheckpoint(ctx, request)
		for _, detail := range status.Convert(err).Details() {
			if info, ok := detail.(*errdetails.ErrorInfo); ok && info.Reason == "KAGENT_CHECKPOINT_SNAPSHOT_PENDING" {
				return false, nil
			}
		}
		return err == nil, err
	})
	if err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}
	return result
}

func TestSessionCheckpoint(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		if harness.name == "byo-adk-e2e" {
			t.Skip("the BYO compiler does not configure a durable session store for the Go ADK fixture; forks cannot restore model history")
		}
		fixture := newInteractionFixture(t, harness, interactionTarget(t), startForkMemoryMock(t))
		_, _, task := fixture.send(t, "What is 2+2?")
		created := createCheckpoint(t, fixture.ctx, fixture.checkpoints, &apiv1alpha1.CreateCheckpointRequest{
			SessionId: fixture.sessionID, RequestId: uuid.NewString(), ExpectedHeadTaskId: string(task.ID),
		})
		checkpoint := created.GetCheckpoint()
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), 2*time.Minute)
			defer cleanupCancel()
			_, cleanupErr := fixture.checkpoints.DeleteCheckpoint(cleanupCtx, &apiv1alpha1.DeleteCheckpointRequest{
				CheckpointId: checkpoint.GetId(),
			})
			if cleanupErr != nil && status.Code(cleanupErr) != codes.NotFound {
				t.Errorf("delete checkpoint: %v", cleanupErr)
			}
		})
		if checkpoint.GetState() != apiv1alpha1.CheckpointState_CHECKPOINT_STATE_READY ||
			checkpoint.GetHeadTaskId() != string(task.ID) || checkpoint.GetHistorySequence() == 0 {
			t.Fatalf("checkpoint = %+v, want ready boundary for task %s", checkpoint, task.ID)
		}

		got, err := fixture.checkpoints.GetCheckpoint(fixture.ctx, &apiv1alpha1.GetCheckpointRequest{
			CheckpointId: checkpoint.GetId(),
		})
		if err != nil || got.GetCheckpoint().GetId() != checkpoint.GetId() {
			t.Fatalf("get checkpoint = %+v, error %v", got.GetCheckpoint(), err)
		}
		listed, err := fixture.checkpoints.ListCheckpoints(fixture.ctx, &apiv1alpha1.ListCheckpointsRequest{
			SessionId: fixture.sessionID,
		})
		if err != nil || len(listed.GetCheckpoints()) != 1 || listed.GetCheckpoints()[0].GetId() != checkpoint.GetId() {
			t.Fatalf("list checkpoints = %+v, error %v", listed.GetCheckpoints(), err)
		}
		// Tags own a copy: later suspends and source deletion must not change
		// either the retained runtime state or the history copied into a fork.
		_, _, later := fixture.send(t, "What is 3+3?")
		if later.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(later), "The answer is 6.") {
			t.Fatalf("source continuation state = %s, text = %q; want a new answer after the checkpoint", later.Status.State, taskText(later))
		}
		if err := deleteIdleSession(fixture.ctx, fixture.sessions, fixture.sessionID); err != nil {
			t.Fatalf("delete checkpoint source: %v", err)
		}
		forked, err := fixture.checkpoints.ForkSession(fixture.ctx, &apiv1alpha1.ForkSessionRequest{
			CheckpointId: checkpoint.GetId(), RequestId: uuid.NewString(),
		})
		if err != nil {
			t.Fatalf("fork Session: %v", err)
		}
		fork := forked.GetSession()
		if fork.GetId() == fixture.sessionID || fork.GetState() != apiv1alpha1.SessionState_SESSION_STATE_READY {
			t.Fatalf("fork = %+v", fork)
		}
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
			defer cleanupCancel()
			if cleanupErr := deleteIdleSession(cleanupCtx, fixture.sessions, fork.GetId()); cleanupErr != nil {
				t.Errorf("delete fork Session: %v", cleanupErr)
			}
		})
		forkCtx, forkCancel := context.WithTimeout(metadata.AppendToOutgoingContext(t.Context(),
			"x-user-id", "e2e",
		), 4*time.Minute)
		t.Cleanup(forkCancel)
		listRequest, err := pbconv.ToProtoListTasksRequest(&a2atype.ListTasksRequest{Tenant: fixture.tenant, ContextID: fork.GetContextId(), PageSize: 10})
		if err != nil {
			t.Fatal(err)
		}
		copiedResponse, err := fixture.client.ListTasks(forkCtx, listRequest)
		if err != nil {
			t.Fatalf("list fork tasks: %v", err)
		}
		copied, err := pbconv.FromProtoListTasksResponse(copiedResponse)
		if err != nil || len(copied.Tasks) != 1 || copied.Tasks[0].ID == task.ID || copied.Tasks[0].ContextID != fork.GetContextId() {
			t.Fatalf("copied fork tasks = %+v, error %v", copied, err)
		}
		// Before its first turn a fork borrows the retained Tag snapshot. Its
		// copied head boundary must refer to that copy, not the deleted source.
		forkCheckpoint := createCheckpoint(t, forkCtx, fixture.checkpoints, &apiv1alpha1.CreateCheckpointRequest{
			SessionId: fork.GetId(), RequestId: uuid.NewString(), ExpectedHeadTaskId: string(copied.Tasks[0].ID),
		})
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
			defer cancel()
			_, err := fixture.checkpoints.DeleteCheckpoint(ctx, &apiv1alpha1.DeleteCheckpointRequest{
				CheckpointId: forkCheckpoint.GetCheckpoint().GetId(),
			})
			if err != nil && status.Code(err) != codes.NotFound {
				t.Errorf("delete fresh fork checkpoint: %v", err)
			}
		})
		// A second fork must find the same private runtime conversation even though
		// neither of its session authorities ever owned the original session ID.
		nested, err := fixture.checkpoints.ForkSession(forkCtx, &apiv1alpha1.ForkSessionRequest{
			CheckpointId: forkCheckpoint.GetCheckpoint().GetId(), RequestId: uuid.NewString(),
		})
		if err != nil {
			t.Fatalf("fork fresh fork: %v", err)
		}
		nestedID := nested.GetSession().GetId()
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
			defer cancel()
			if err := deleteIdleSession(ctx, fixture.sessions, nestedID); err != nil {
				t.Errorf("delete nested fork: %v", err)
			}
		})
		nestedCtx := metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "e2e")
		nestedFixture := &interactionFixture{ctx: nestedCtx, client: fixture.client, tenant: fixture.tenant, sessionID: nestedID, contextID: nestedID}
		_, _, nestedTask := nestedFixture.send(t, "What was the answer before the checkpoint?")
		if nestedTask.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(nestedTask), "The answer is 4.") {
			t.Fatalf("nested fork task state = %s, text = %q; want checkpoint memory", nestedTask.Status.State, taskText(nestedTask))
		}
		if err := deleteIdleSession(nestedCtx, fixture.sessions, nestedID); err != nil {
			t.Fatalf("delete nested fork: %v", err)
		}
		if _, err := fixture.checkpoints.DeleteCheckpoint(forkCtx, &apiv1alpha1.DeleteCheckpointRequest{
			CheckpointId: forkCheckpoint.GetCheckpoint().GetId(),
		}); err != nil {
			t.Fatalf("delete fresh fork checkpoint: %v", err)
		}
		forkFixture := &interactionFixture{ctx: forkCtx, client: fixture.client, tenant: fixture.tenant, sessionID: fork.GetId(), contextID: fork.GetId()}
		_, _, forkTask := forkFixture.send(t, "What was the answer before the checkpoint?")
		if forkTask.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(forkTask), "The answer is 4.") {
			t.Fatalf("fork A2A task state = %s, want COMPLETED", forkTask.Status.State)
		}
		if err := deleteIdleSession(forkCtx, fixture.sessions, fork.GetId()); err != nil {
			t.Fatalf("delete fork Session: %v", err)
		}
		if _, err := fixture.checkpoints.DeleteCheckpoint(fixture.ctx, &apiv1alpha1.DeleteCheckpointRequest{
			CheckpointId: checkpoint.GetId(),
		}); err != nil {
			t.Fatalf("delete checkpoint: %v", err)
		}
	})
}

func TestMCPInteraction(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		target := interactionTarget(t)
		mcpURL, mcpServer := startMCPMock(t)
		template, _ := createMCPInteractionTemplate(t, harness, mcpURL, false)
		fixture := newInteractionFixtureForTemplate(t, harness, target, template)
		_, _, task := fixture.send(t, "Add 3 and 5 using the configured MCP server.")
		if task.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(task), "result is 8") {
			t.Fatalf("A2A task state = %s, text = %q, want completed task with MCP result", task.Status.State, taskText(task))
		}
		for _, request := range mcpServer.Requests() {
			if bytes.Contains(request.Body, []byte(`"method":"tools/call"`)) && bytes.Contains(request.Body, []byte(`"name":"add_numbers"`)) {
				return
			}
		}
		t.Fatal("mock MCP server did not receive an add_numbers tool call")
	})
}

func TestMCPToolApproval(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		target := interactionTarget(t)
		mcpURL, mcpServer := startMCPMock(t)
		template, toolName := createMCPInteractionTemplate(t, harness, mcpURL, true)
		fixture := newInteractionFixtureForTemplate(t, harness, target, template)
		completed := sendApprovedToolRequest(t, fixture, "Add 3 and 5 using the configured MCP server.", toolName)
		if completed.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(completed), "result is 8") {
			t.Fatalf("approved MCP task state = %s, text = %q", completed.Status.State, taskText(completed))
		}
		for _, request := range mcpServer.Requests() {
			if bytes.Contains(request.Body, []byte(`"method":"tools/call"`)) && bytes.Contains(request.Body, []byte(`"name":"add_numbers"`)) {
				return
			}
		}
		t.Fatal("approved MCP tool did not execute")
	})
}

func TestSharedAgentInteraction(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		switch harness.name {
		case codexE2EHarness:
			t.Skip("a deterministic Codex subagent model fixture is not available yet")
		}
		fixture := newSharedInteractionFixture(t, harness, interactionTarget(t))
		_, _, task := fixture.send(t, "Ask the specialist")
		if task.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(task), "Answer from the shared specialist.") {
			t.Fatalf("A2A task state = %s, text = %q, want completed task with shared child response", task.Status.State, taskText(task))
		}
		sessions, err := fixture.sessions.ListSessions(fixture.ctx, &apiv1alpha1.ListSessionsRequest{})
		if err != nil {
			t.Fatalf("list Sessions: %v", err)
		}
		for _, session := range sessions.GetSessions() {
			if session.GetAgent().GetName() == fixture.childTemplate {
				t.Fatalf("Shared child created Session %q", session.GetId())
			}
		}

		listRequest, err := pbconv.ToProtoListTasksRequest(&a2atype.ListTasksRequest{Tenant: fixture.tenant, ContextID: fixture.contextID})
		if err != nil {
			t.Fatalf("build ListTasks request: %v", err)
		}
		listed, err := fixture.client.ListTasks(fixture.ctx, listRequest)
		if err != nil {
			t.Fatalf("list root tasks: %v", err)
		}
		if len(listed.GetTasks()) != 1 || listed.GetTasks()[0].GetId() != string(task.ID) {
			t.Fatalf("public tasks = %#v, want only root task %s", listed.GetTasks(), task.ID)
		}
	})
}

func TestSessionTaskPersistenceAndReconnect(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		fixture := newInteractionFixture(t, harness, interactionTarget(t), startInteractionMock(t))
		_, _, task := fixture.send(t, "What is 2+2?")

		getRequest, err := pbconv.ToProtoGetTaskRequest(&a2atype.GetTaskRequest{Tenant: fixture.tenant, ID: task.ID})
		if err != nil {
			t.Fatalf("build GetTask request: %v", err)
		}
		gotProto, err := fixture.client.GetTask(fixture.ctx, getRequest)
		if err != nil {
			t.Fatalf("get persisted task: %v", err)
		}
		got, err := pbconv.FromProtoTask(gotProto)
		if err != nil {
			t.Fatalf("decode persisted task: %v", err)
		}
		if got.ID != task.ID || got.ContextID != fixture.contextID || got.Status.State != a2atype.TaskStateCompleted {
			t.Fatalf("persisted task = %#v, want completed task %s in context %s", got, task.ID, fixture.sessionID)
		}

		listRequest, err := pbconv.ToProtoListTasksRequest(&a2atype.ListTasksRequest{Tenant: fixture.tenant, ContextID: fixture.contextID})
		if err != nil {
			t.Fatalf("build ListTasks request: %v", err)
		}
		listedProto, err := fixture.client.ListTasks(fixture.ctx, listRequest)
		if err != nil {
			t.Fatalf("list persisted tasks: %v", err)
		}
		listed, err := pbconv.FromProtoListTasksResponse(listedProto)
		if err != nil {
			t.Fatalf("decode listed tasks: %v", err)
		}
		if listed.TotalSize != 1 || len(listed.Tasks) != 1 || listed.Tasks[0].ID != task.ID || listed.Tasks[0].ContextID != fixture.contextID {
			t.Fatalf("listed tasks = %#v, want only task %s in context %s", listed, task.ID, fixture.sessionID)
		}

		stream, err := fixture.client.SubscribeToTask(fixture.ctx, &a2apb.SubscribeToTaskRequest{Tenant: fixture.tenant, Id: string(task.ID)})
		if err != nil {
			t.Fatalf("reconnect to completed task: %v", err)
		}
		event, err := stream.Recv()
		if err != nil {
			t.Fatalf("read completed task: %v", err)
		}
		if event.GetTask().GetId() != string(task.ID) || event.GetTask().GetStatus().GetState() != a2apb.TaskState_TASK_STATE_COMPLETED {
			t.Fatalf("reconnect = %v, want stored completion", event)
		}
	})
}

func TestSessionActiveTask(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		target := interactionTarget(t)
		modelURL, started := startBlockingInteractionMock(t)
		fixture := newInteractionFixture(t, harness, target, modelURL)
		testActiveTaskCancellation(t, fixture, started)
	})
}

func testActiveTaskCancellation(t *testing.T, fixture *interactionFixture, started <-chan struct{}) {
	t.Helper()
	_, request := newMessageRequest(t, "Wait for cancellation")
	request.Tenant, request.Message.ContextId = fixture.tenant, fixture.sessionID
	stream, err := fixture.client.SendStreamingMessage(fixture.ctx, request)
	if err != nil {
		t.Fatalf("start streaming A2A message: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Minute):
		t.Fatal("runtime did not call the blocking model")
	}

	listRequest, err := pbconv.ToProtoListTasksRequest(&a2atype.ListTasksRequest{Tenant: fixture.tenant, ContextID: fixture.contextID})
	if err != nil {
		t.Fatalf("build ListTasks request: %v", err)
	}
	listedProto, err := fixture.client.ListTasks(fixture.ctx, listRequest)
	if err != nil {
		t.Fatalf("list active tasks: %v", err)
	}
	listed, err := pbconv.FromProtoListTasksResponse(listedProto)
	if err != nil {
		t.Fatalf("decode active tasks: %v", err)
	}
	if len(listed.Tasks) != 1 || listed.Tasks[0].Status.State.Terminal() {
		t.Fatalf("active tasks = %#v, want one non-terminal task", listed.Tasks)
	}
	task := listed.Tasks[0]

	_, busyRequest := newMessageRequest(t, "Second concurrent request")
	busyRequest.Tenant, busyRequest.Message.ContextId = fixture.tenant, fixture.sessionID
	if _, err := fixture.client.SendMessage(fixture.ctx, busyRequest); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("concurrent message error = %v, want %s", err, codes.FailedPrecondition)
	}

	subscribeRequest, err := pbconv.ToProtoSubscribeToTaskRequest(&a2atype.SubscribeToTaskRequest{Tenant: fixture.tenant, ID: task.ID})
	if err != nil {
		t.Fatalf("build SubscribeToTask request: %v", err)
	}
	subscription, err := fixture.client.SubscribeToTask(fixture.ctx, subscribeRequest)
	if err != nil {
		t.Fatalf("subscribe to active task: %v", err)
	}
	firstEventProto, err := subscription.Recv()
	if err != nil {
		t.Fatalf("receive initial subscribed task event: %v", err)
	}
	firstEvent, err := pbconv.FromProtoStreamResponse(firstEventProto)
	if err != nil {
		t.Fatalf("decode initial subscribed task event: %v", err)
	}
	if firstEvent.TaskInfo().TaskID != task.ID {
		t.Fatalf("subscribed task = %s, want %s", firstEvent.TaskInfo().TaskID, task.ID)
	}

	cancelRequest, err := pbconv.ToProtoCancelTaskRequest(&a2atype.CancelTaskRequest{Tenant: fixture.tenant, ID: task.ID})
	if err != nil {
		t.Fatalf("build CancelTask request: %v", err)
	}
	canceledProto, err := fixture.client.CancelTask(fixture.ctx, cancelRequest)
	if err != nil {
		t.Fatalf("cancel active task: %v", err)
	}
	canceled, err := pbconv.FromProtoTask(canceledProto)
	if err != nil {
		t.Fatalf("decode canceled task: %v", err)
	}
	if canceled.ID != task.ID || canceled.Status.State != a2atype.TaskStateCanceled {
		t.Fatalf("canceled task = %#v, want task %s in CANCELED", canceled, task.ID)
	}
	waitForTaskState(t, subscription, a2atype.TaskStateCanceled)
	waitForTaskState(t, stream, a2atype.TaskStateCanceled)
	assertTaskStreamClosed(t, subscription)
	assertTaskStreamClosed(t, stream)
	getRequest, err := pbconv.ToProtoGetTaskRequest(&a2atype.GetTaskRequest{Tenant: fixture.tenant, ID: task.ID})
	if err != nil {
		t.Fatalf("build GetTask request: %v", err)
	}
	persistedProto, err := fixture.client.GetTask(fixture.ctx, getRequest)
	if err != nil {
		t.Fatalf("get canceled task: %v", err)
	}
	persisted, err := pbconv.FromProtoTask(persistedProto)
	if err != nil {
		t.Fatalf("decode canceled task: %v", err)
	}
	if persisted.Status.State != a2atype.TaskStateCanceled {
		t.Fatalf("persisted task state = %s, want CANCELED", persisted.Status.State)
	}
}

type interactionFixture struct {
	ctx         context.Context
	client      a2apb.A2AServiceClient
	sessions    apiv1alpha1.SessionServiceClient
	checkpoints apiv1alpha1.CheckpointServiceClient
	system      apiv1alpha1.SystemServiceClient
	sessionID   string
	contextID   string
	tenant      string
}

type sharedInteractionFixture struct {
	*interactionFixture
	childTemplate string
}

func interactionTarget(t *testing.T) string {
	t.Helper()
	rawURL := os.Getenv("KAGENT_E2E_API_URL")
	if rawURL == "" {
		rawURL = os.Getenv("KAGENT_API_URL")
	}
	if rawURL == "" {
		t.Skip("KAGENT_E2E_API_URL is not set")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		t.Fatalf("invalid KAGENT_E2E_API_URL %q: %v", rawURL, err)
	}
	target := parsed.Host
	return target
}

func newInteractionFixture(t *testing.T, harness testHarness, target, modelURL string) *interactionFixture {
	t.Helper()
	return newInteractionFixtureForTemplate(t, harness, target, createInteractionTemplate(t, harness, modelURL))
}

func newInteractionFixtureForTemplate(t *testing.T, harness testHarness, target, templateName string) *interactionFixture {
	t.Helper()
	return newInteractionFixtureForHarnessTemplate(t, target, harness.name, templateName)
}

func newInteractionFixtureForHarnessTemplate(t *testing.T, target, harnessName, templateName string) *interactionFixture {
	t.Helper()
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("connect to kagent gRPC API: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "e2e"), 4*time.Minute)
	t.Cleanup(cancel)
	sessions := apiv1alpha1.NewSessionServiceClient(conn)
	request := &apiv1alpha1.CreateSessionRequest{
		Agent: &apiv1alpha1.ResourceReference{Namespace: "kagent", Name: templateName}, RequestId: uuid.NewString(),
	}
	var created *apiv1alpha1.CreateSessionResponse
	err = wait.PollUntilContextTimeout(ctx, time.Second, time.Minute, true, func(ctx context.Context) (bool, error) {
		created, err = sessions.CreateSession(ctx, request)
		if status.Code(err) == codes.FailedPrecondition {
			return false, nil
		}
		return err == nil, err
	})
	if err != nil {
		t.Fatalf("create Session: %v", err)
	}
	session := created.GetSession()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cleanupCancel()
		// DeleteSession returns only after its Substrate Actor has been
		// suspended and deleted, so this cleanup covers both resources.
		if cleanupErr := deleteIdleSession(cleanupCtx, sessions, session.GetId()); cleanupErr != nil {
			t.Errorf("delete Session: %v", cleanupErr)
		}
	})
	if session.GetState() != apiv1alpha1.SessionState_SESSION_STATE_READY {
		t.Fatalf("created Session state = %s, want READY", session.GetState())
	}
	return &interactionFixture{
		ctx:         ctx,
		tenant:      session.GetAgent().GetNamespace() + "/" + session.GetAgent().GetName(),
		client:      a2apb.NewA2AServiceClient(conn),
		sessions:    sessions,
		checkpoints: apiv1alpha1.NewCheckpointServiceClient(conn),
		system:      apiv1alpha1.NewSystemServiceClient(conn),
		sessionID:   session.GetId(),
		contextID:   session.GetContextId(),
	}
}

// Completion is public before automatic suspension finishes. Cleanup retries
// only the precondition indicating that lifecycle work still owns the session.
func deleteIdleSession(ctx context.Context, sessions apiv1alpha1.SessionServiceClient, id string) error {
	return wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, time.Minute, true, func(ctx context.Context) (bool, error) {
		_, err := sessions.DeleteSession(ctx, &apiv1alpha1.DeleteSessionRequest{SessionId: id})
		switch status.Code(err) {
		case codes.OK, codes.NotFound:
			return true, nil
		case codes.FailedPrecondition:
			return false, nil
		default:
			return false, err
		}
	})
}

func newSharedInteractionFixture(t *testing.T, harness testHarness, target string) *sharedInteractionFixture {
	t.Helper()
	var root, child string
	if harness.name == claudeE2EHarness {
		raw, err := claudeInteractionMocks.ReadFile("mocks/invoke_claude_local_subagent.json")
		if err != nil {
			t.Fatal(err)
		}
		raw = bytes.ReplaceAll(raw, []byte("Delegate this request to the specialist."), []byte("Ask the specialist"))
		raw = bytes.ReplaceAll(raw, []byte("CLAUDE_SUBAGENT_FINAL"), []byte("Answer from the shared specialist."))
		var cfg mockllm.Config
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		kube := interactionKubeClient(t)
		model := harness.createModel(t, kube, reachableModelURL(t, startMockLLMConfig(t, cfg)), nil)
		root, child = createClaudeLocalAgentTemplates(t, kube, model, "CLAUDE_LOCAL_SPECIALIST_INSTRUCTION")
	} else {
		root, child = createSharedInteractionTemplates(t, harness, startSharedInteractionMock(t))
	}
	return &sharedInteractionFixture{
		interactionFixture: newInteractionFixtureForTemplate(t, harness, target, root),
		childTemplate:      child,
	}
}

func (f *interactionFixture) send(t *testing.T, text string) (*a2atype.Message, *a2apb.SendMessageRequest, *a2atype.Task) {
	t.Helper()
	message, request := newMessageRequest(t, text)
	message.ContextID = f.sessionID
	request.Message.ContextId = f.sessionID
	request.Tenant = f.tenant
	response, err := sendMessageWithRetry(f.ctx, f.client, request)
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
	return message, request, task
}

// sendMessageWithRetry follows the gateway's explicit rejection contract. A
// published task can precede the runtime releasing its execution slot. Retry the
// same input only when the gateway proves it was never accepted; a generic
// transport error may hide accepted work and must not cause another execution.
func sendMessageWithRetry(ctx context.Context, client a2apb.A2AServiceClient, request *a2apb.SendMessageRequest) (*a2apb.SendMessageResponse, error) {
	var response *a2apb.SendMessageResponse
	err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		var err error
		response, err = client.SendMessage(ctx, request)
		if status.Code(err) == codes.FailedPrecondition {
			for _, detail := range status.Convert(err).Details() {
				if info, ok := detail.(*errdetails.ErrorInfo); ok && info.Domain == a2atype.ProtocolDomain && info.Metadata["reason"] == "KAGENT_SEND_NOT_ACCEPTED" {
					return false, nil
				}
			}
		}
		return err == nil, err
	})
	return response, err
}

func newMessageRequest(t *testing.T, text string) (*a2atype.Message, *a2apb.SendMessageRequest) {
	t.Helper()
	message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(text))
	request, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: message})
	if err != nil {
		t.Fatalf("build A2A request: %v", err)
	}
	return message, request
}

type streamReceiver interface {
	Recv() (*a2apb.StreamResponse, error)
}

func waitForTaskState(t *testing.T, stream streamReceiver, want a2atype.TaskState) {
	t.Helper()
	for {
		response, err := stream.Recv()
		if err != nil {
			t.Fatalf("receive task stream: %v", err)
		}
		event, err := pbconv.FromProtoStreamResponse(response)
		if err != nil {
			t.Fatalf("decode task stream: %v", err)
		}
		switch event := event.(type) {
		case *a2atype.Task:
			if event.Status.State == want {
				return
			}
		case *a2atype.TaskStatusUpdateEvent:
			if event.Status.State == want {
				return
			}
		}
	}
}

func assertTaskStreamClosed(t *testing.T, stream streamReceiver) {
	t.Helper()
	response, err := stream.Recv()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("task stream emitted after its terminal boundary: response=%#v error=%v", response, err)
	}
}

func startInteractionMock(t *testing.T) string {
	return startMockLLM(t, "mocks/invoke_agent.json")
}

func startMockLLM(t *testing.T, fixture string) string {
	t.Helper()
	return reachableModelURL(t, startMockLLMServer(t, interactionMocks, fixture))
}

func startMockLLMServer(t *testing.T, fixtures fs.ReadFileFS, fixture string) string {
	t.Helper()
	cfg, err := mockllm.LoadConfigFromFile(fixture, fixtures)
	if err != nil {
		t.Fatalf("load mock LLM response: %v", err)
	}
	return startMockLLMConfig(t, cfg)
}

func startMockLLMConfig(t *testing.T, cfg mockllm.Config) string {
	t.Helper()
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
	return baseURL
}

func startBlockingInteractionMock(t *testing.T) (string, <-chan struct{}) {
	t.Helper()
	cfg, err := mockllm.LoadConfigFromFile("mocks/invoke_agent.json", interactionMocks)
	if err != nil {
		t.Fatalf("load mock LLM response: %v", err)
	}
	baseURL, started := startBlockingMockServer(t, cfg.OpenAI[0].Response)
	return reachableModelURL(t, baseURL), started
}

func startBlockingMockServer(t *testing.T, response any) (string, <-chan struct{}) {
	t.Helper()
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce, releaseOnce sync.Once
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedOnce.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
			return
		case <-release:
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("write mock LLM response: %v", err)
		}
	}))
	_ = server.Listener.Close()
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen for blocking mock LLM: %v", err)
	}
	server.Listener = listener
	server.Start()
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		server.Close()
	})
	return server.URL, started
}

func startSharedInteractionMock(t *testing.T) string {
	return startMockLLM(t, "mocks/invoke_shared_agent.json")
}

func startMCPMock(t *testing.T) (string, *mockmcp.Server) {
	t.Helper()
	server, err := mockmcp.NewServer(mockmcp.Options{Addr: "0.0.0.0:0", RecordRequests: true})
	if err != nil {
		t.Fatalf("create mock MCP server: %v", err)
	}
	baseURL, err := server.Start(t.Context())
	if err != nil {
		t.Fatalf("start mock MCP server: %v", err)
	}
	t.Cleanup(func() {
		if err := server.Stop(context.Background()); err != nil {
			t.Errorf("stop mock MCP server: %v", err)
		}
	})
	return reachableServerURL(t, baseURL, mockmcp.MCPPath), server
}

func reachableModelURL(t *testing.T, baseURL string) string {
	return reachableServerURL(t, baseURL, "/v1")
}

func reachableServerURL(t *testing.T, baseURL, path string) string {
	t.Helper()
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
	if net.ParseIP(host) != nil {
		host = mockOriginService(t, host, port)
	}
	parsed.Host = net.JoinHostPort(host, port)
	parsed.Path = path
	return parsed.String()
}

func createInteractionTemplate(t *testing.T, harness testHarness, modelURL string) string {
	t.Helper()
	kube := interactionKubeClient(t)
	model := harness.createModel(t, kube, modelURL, nil)
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "interaction-", Namespace: "kagent",
			Labels: harness.labels(),
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: model.Name},
			Description:  "Agent interaction E2E fixture",
			SystemPrompt: "Reply briefly.",
		},
	}
	createAndWaitInteractionTemplate(t, harness, kube, template)
	return template.Name
}

func createStructuredOutputInteractionTemplate(t *testing.T, harness testHarness, kube ctrlclient.Client, modelURL, mcpURL string) *v1alpha3.AgentTemplate {
	t.Helper()
	model := harness.createModel(t, kube, modelURL, nil)
	server := &v1alpha3.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "structured-output-mcp-", Namespace: "kagent"},
		Spec: v1alpha3.RemoteMCPServerSpec{
			Description: "Structured output interaction E2E fixture",
			Protocol:    v1alpha3.RemoteMCPServerProtocolStreamableHttp,
			URL:         mcpURL,
		},
	}
	if err := kube.Create(t.Context(), server); err != nil {
		t.Fatalf("create structured-output RemoteMCPServer: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), server); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete structured-output RemoteMCPServer: %v", err)
		}
	})
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "structured-output-", Namespace: "kagent",
			Labels: harness.labels(),
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: model.Name},
			SystemPrompt: "Use available tools when needed, then return the arithmetic answer and a short explanation.",
			OutputSchema: &apiextensionsv1.JSON{Raw: []byte(structuredOutputSchema)},
			Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{
				Server: corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: server.Name},
				Tools:  []string{"add_numbers"},
			}}},
		},
	}
	createAndWaitInteractionTemplate(t, harness, kube, template)
	return template
}

func createMCPInteractionTemplate(t *testing.T, harness testHarness, mcpURL string, requireApproval bool) (string, string) {
	t.Helper()
	kube := interactionKubeClient(t)
	server := &v1alpha3.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "interaction-mcp-", Namespace: "kagent"},
		Spec: v1alpha3.RemoteMCPServerSpec{
			Description: "MCP interaction E2E fixture",
			Protocol:    v1alpha3.RemoteMCPServerProtocolStreamableHttp,
			URL:         mcpURL,
		},
	}
	if err := kube.Create(t.Context(), server); err != nil {
		t.Fatalf("create interaction RemoteMCPServer: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), server); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete interaction RemoteMCPServer: %v", err)
		}
	})
	var modelURL string
	toolName := "add_numbers"
	switch harness.name {
	case codexE2EHarness:
		toolName = server.Name + ".add_numbers"
		modelURL = startCodexResourceMockLLM(t, codexMCPToolNamespace(server.Name))
	case claudeE2EHarness:
		toolName = "mcp__" + server.Name + "__add_numbers"
		modelURL = startClaudeResourceMockLLM(t, toolName)
	default:
		cfg, err := mockllm.LoadConfigFromFile("mocks/invoke_mcp_agent.json", interactionMocks)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(`{"role":"user","content":"Add 3 and 5 using the configured MCP server."}`), &cfg.OpenAI[0].Match.Message); err != nil {
			t.Fatal(err)
		}
		modelURL = reachableModelURL(t, startMockLLMConfig(t, cfg))
	}
	model := harness.createModel(t, kube, modelURL, nil)
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "mcp-interaction-", Namespace: "kagent",
			Labels: harness.labels(),
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: model.Name},
			Description:  "MCP interaction E2E fixture",
			SystemPrompt: "Use the configured MCP tool. Do not calculate the answer yourself.",
			Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{
				Server:          corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: server.Name},
				RequireApproval: requireApproval,
			}}},
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, harness.name)
	return template.Name, toolName
}

func createSharedInteractionTemplates(t *testing.T, harness testHarness, modelURL string) (string, string) {
	t.Helper()
	kube := interactionKubeClient(t)
	rootModel := harness.createModel(t, kube, modelURL, map[string]string{"X-Kagent-E2E-Agent": "root"})
	childModel := harness.createModel(t, kube, modelURL, map[string]string{"X-Kagent-E2E-Agent": "child"})
	child := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "shared-child-", Namespace: "kagent",
			Labels: harness.labels(),
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: childModel.Name},
			Description:  "Shared specialist",
			SystemPrompt: "Answer as the shared specialist.",
		},
	}
	createSharedTemplate(t, kube, child)
	root := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "shared-root-", Namespace: "kagent",
			Labels: harness.labels(),
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: rootModel.Name},
			Description:  "Shared agent interaction E2E fixture",
			SystemPrompt: "Delegate every request to the specialist.",
			Tools: []v1alpha3.ToolBinding{{SubAgent: &v1alpha3.SubAgentToolBinding{
				Name: "specialist", Description: "Handles specialist requests",
				TemplateRef: &corev1.LocalObjectReference{Name: child.Name},
			}}},
		},
	}
	createAndWaitInteractionTemplate(t, harness, kube, root)
	return root.Name, child.Name
}

// Shared children are reusable context: no Agent or Harness binding is needed.
func createSharedTemplate(t *testing.T, kube ctrlclient.Client, template *v1alpha3.AgentTemplate) {
	t.Helper()
	template.Labels = nil
	if err := kube.Create(t.Context(), template); err != nil {
		t.Fatalf("create shared template: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), template); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete shared template: %v", err)
		}
	})
}

func interactionKubeClient(t *testing.T) ctrlclient.Client {
	t.Helper()
	cfg, err := config.GetConfig()
	if err != nil {
		t.Fatalf("load Kubernetes config: %v", err)
	}
	clientScheme := k8sruntime.NewScheme()
	if err := corev1.AddToScheme(clientScheme); err != nil {
		t.Fatalf("register Kubernetes core API: %v", err)
	}
	if err := discoveryv1.AddToScheme(clientScheme); err != nil {
		t.Fatalf("register discovery API: %v", err)
	}
	if err := v1alpha3.AddToScheme(clientScheme); err != nil {
		t.Fatalf("register kagent API: %v", err)
	}
	kube, err := ctrlclient.New(cfg, ctrlclient.Options{Scheme: clientScheme})
	if err != nil {
		t.Fatalf("create Kubernetes client: %v", err)
	}
	return kube
}

func createInteractionModel(t *testing.T, kube ctrlclient.Client, modelURL string, headers map[string]string) *v1alpha3.ModelConfig {
	t.Helper()
	model := &v1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "interaction-", Namespace: "kagent"},
		Spec: v1alpha3.ModelConfigSpec{
			Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-4.1-mini",
			APIKeySecret: "kagent-openai", APIKeySecretKey: "OPENAI_API_KEY",
			OpenAI: &v1alpha3.OpenAIConfig{BaseURL: modelURL}, DefaultHeaders: headers,
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
	return model
}

func createAndWaitInteractionTemplate(t *testing.T, harness testHarness, kube ctrlclient.Client, template *v1alpha3.AgentTemplate) {
	t.Helper()
	createAndWaitInteractionTemplateForHarness(t, kube, template, harness.name)
}

func createAndWaitInteractionTemplateForHarness(t *testing.T, kube ctrlclient.Client, template *v1alpha3.AgentTemplate, harnessName string) {
	t.Helper()
	if err := kube.Create(t.Context(), template); err != nil {
		t.Fatalf("create interaction AgentTemplate: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cleanupCancel()
		if err := kube.Delete(cleanupCtx, template); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete interaction AgentTemplate: %v", err)
			return
		}
		if err := wait.PollUntilContextTimeout(cleanupCtx, time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
			current := &v1alpha3.AgentTemplate{}
			if err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(template), current); err == nil {
				return false, nil
			} else if !apierrors.IsNotFound(err) {
				return false, err
			}
			return true, nil
		}); err != nil {
			t.Errorf("wait for interaction AgentTemplate %s/%s to be deleted: %v", template.Namespace, template.Name, err)
			return
		}
	})

	agent := &v1alpha3.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: template.Name, Namespace: template.Namespace},
		Spec: v1alpha3.AgentSpec{
			TemplateRef: &corev1.LocalObjectReference{Name: template.Name},
			HarnessRef:  &corev1.LocalObjectReference{Name: harnessName},
		},
	}
	if err := kube.Create(t.Context(), agent); err != nil {
		t.Fatalf("create Agent: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), agent); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete Agent: %v", err)
		}
	})
	// Independent informers can observe the Agent before its references. False
	// conditions are intermediate observations; wait for the current generation
	// to become ready and retain all conditions for timeout diagnostics.
	err := wait.PollUntilContextTimeout(t.Context(), time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(agent), agent); err != nil {
			return false, err
		}
		for _, condition := range agent.Status.Conditions {
			if condition.Type == v1alpha3.AgentConditionReady && condition.ObservedGeneration == agent.Generation {
				return condition.Status == metav1.ConditionTrue, nil
			}
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("wait for Agent %s/%s: %v; last conditions: %+v", agent.Namespace, agent.Name, err, agent.Status.Conditions)
	}
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

// The model only answers a continuation when its request contains the assistant
// turn captured by the checkpoint and excludes the source's later turn.
func startForkMemoryMock(t *testing.T) string {
	t.Helper()
	fixture, err := interactionMocks.ReadFile("mocks/invoke_agent.json")
	if err != nil {
		t.Fatal(err)
	}
	var config mockllm.Config
	if err := json.Unmarshal(fixture, &config); err != nil {
		t.Fatal(err)
	}
	var continuation mockllm.Config
	if err := json.Unmarshal(bytes.ReplaceAll(fixture, []byte("What is 2+2?"), []byte("What was the answer before the checkpoint?")), &continuation); err != nil {
		t.Fatal(err)
	}
	config.OpenAI = append(config.OpenAI, continuation.OpenAI...)
	config.OpenAIResponse = append(config.OpenAIResponse, continuation.OpenAIResponse...)
	config.Anthropic = append(config.Anthropic, continuation.Anthropic...)
	recorder := startModelRecorder(t, startMockLLMConfig(t, config), func(body []byte) error {
		if !bytes.Contains(body, []byte("What was the answer before the checkpoint?")) {
			return nil
		}
		if !bytes.Contains(body, []byte("The answer is 4.")) || bytes.Contains(body, []byte("The answer is 6.")) {
			return errors.New("fork did not restore the checkpoint conversation")
		}
		return nil
	})
	return reachableModelURL(t, recorder.URL)
}

// modelRecorder proxies model requests to a mock LLM and keeps a copy of each
// one, so a test can assert on what the runtime sent rather than only on what
// the mock answered.
type modelRecorder struct {
	// URL is the proxy's listener on the test host.
	URL string

	mu       sync.Mutex
	requests []recordedModelRequest
}

type recordedModelRequest struct {
	Header http.Header
	Body   []byte
}

// startModelRecorder puts a recording proxy in front of the mock LLM at
// upstreamURL. An inspect function may reject a request: its error is
// answered with 400 instead of being forwarded, which fails the agent's turn
// visibly rather than letting the mock answer a prompt it should not see.
func startModelRecorder(t *testing.T, upstreamURL string, inspect func(body []byte) error) *modelRecorder {
	t.Helper()
	upstream, err := url.Parse(upstreamURL)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &modelRecorder{}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = r.Body.Close()
		recorder.mu.Lock()
		recorder.requests = append(recorder.requests, recordedModelRequest{Header: r.Header.Clone(), Body: body})
		recorder.mu.Unlock()
		if inspect != nil {
			if err := inspect(body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		proxy.ServeHTTP(w, r)
	}))
	_ = server.Listener.Close()
	server.Listener, err = net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	server.Start()
	t.Cleanup(server.Close)
	recorder.URL = server.URL
	return recorder
}

// Requests returns the recorded requests carrying the header value, in
// arrival order.
func (r *modelRecorder) Requests(header, value string) []recordedModelRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	var matched []recordedModelRequest
	for _, request := range r.requests {
		if request.Header.Get(header) == value {
			matched = append(matched, request)
		}
	}
	return matched
}

// Gateway credential rules match DNS names. Give host-based mocks a cluster
// service name without depending on public DNS or changing the gateway config.
func mockOriginService(t *testing.T, address, port string) string {
	t.Helper()
	number, err := strconv.ParseInt(port, 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	kube := interactionKubeClient(t)
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{GenerateName: "mock-origin-", Namespace: "kagent"}, Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http", Port: int32(number)}}}}
	if err := kube.Create(t.Context(), service); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), service); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete mock service: %v", err)
		}
	})
	addressType := discoveryv1.AddressTypeIPv4
	if net.ParseIP(address).To4() == nil {
		addressType = discoveryv1.AddressTypeIPv6
	}
	endpoints := &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{Name: service.Name, Namespace: service.Namespace, Labels: map[string]string{discoveryv1.LabelServiceName: service.Name, discoveryv1.LabelManagedBy: "kagent-e2e"}, OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Service", Name: service.Name, UID: service.UID}}}, AddressType: addressType, Ports: []discoveryv1.EndpointPort{{Name: new("http"), Port: new(int32(number)), Protocol: new(corev1.ProtocolTCP)}}, Endpoints: []discoveryv1.Endpoint{{Addresses: []string{address}, Conditions: discoveryv1.EndpointConditions{Ready: new(true)}}}}
	if err := kube.Create(t.Context(), endpoints); err != nil {
		t.Fatal(err)
	}
	return service.Name + "." + service.Namespace + ".svc.cluster.local"
}

// Exercise first-message creation through the public API, including recovery
// after a response is lost and the client retries without learning its context.
func TestAgentA2ACreatesConversation(t *testing.T) {
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		fixture := newInteractionFixture(t, harness, interactionTarget(t), startInteractionMock(t))
		send := func(messageID string) *a2apb.Task {
			t.Helper()
			_, request := newMessageRequest(t, "What is 2+2?")
			request.Tenant, request.Message.MessageId = fixture.tenant, messageID
			response, err := fixture.client.SendMessage(fixture.ctx, request)
			if err != nil {
				t.Fatalf("send without context: %v", err)
			}
			task := response.GetTask()
			if task == nil || task.ContextId == "" {
				t.Fatalf("expected assigned conversation: %v", response)
			}
			return task
		}
		initialID := uuid.NewString()
		first := send(initialID)
		cleanup := func(id string) {
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
				defer cancel()
				if err := deleteIdleSession(ctx, fixture.sessions, id); err != nil {
					t.Errorf("delete created conversation: %v", err)
				}
			})
		}
		cleanup(first.ContextId)
		retry := send(initialID)
		if retry.ContextId != first.ContextId || retry.Id != first.Id {
			t.Fatalf("retry created another task/conversation: %v vs %v", first, retry)
		}
		second := send(uuid.NewString())
		cleanup(second.ContextId)
		if second.ContextId == first.ContextId {
			t.Fatal("new initial message must create another conversation")
		}
		session, err := fixture.sessions.GetSession(fixture.ctx, &apiv1alpha1.GetSessionRequest{SessionId: first.ContextId})
		if err != nil {
			t.Fatal(err)
		}
		if session.GetSession().GetId() != first.ContextId {
			t.Fatal("public context must identify its Session")
		}
	})
}
