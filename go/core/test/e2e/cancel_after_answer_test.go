package e2e_test

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"google.golang.org/grpc/metadata"

	adka2a "github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	kagenta2a "github.com/kagent-dev/kagent/go/api/a2a"
)

// Issue #3314: once an ask-user answer resumes a task, cancelling it must stop
// the resumed turn, as it does for a task that never paused.
func TestSessionCancelAfterAskUserAnswer(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		model := startAnswerBlockingMock(t, harness)
		fixture := newInteractionFixture(t, harness, interactionTarget(t), model.url)
		fixture.ctx = metadata.AppendToOutgoingContext(fixture.ctx, strings.ToLower(a2atype.SvcParamExtensions), adka2a.HITLExtensionURI)

		// Given a task that asked a question and was suspended while waiting.
		_, _, waiting := fixture.send(t, "Which database should we use for storage?")
		if waiting.Status.State != a2atype.TaskStateInputRequired {
			t.Fatalf("A2A task state = %s, want INPUT_REQUIRED", waiting.Status.State)
		}
		request := adka2a.GetAskUserRequest(waiting.Status.Message)
		if request == nil {
			t.Fatal("INPUT_REQUIRED task has no ask_user request")
		}
		assertActorSuspended(t, fixture)

		// When the answer resumes it and the task is cancelled mid-turn.
		model.holdNextCalls()
		reply := adka2a.AttachHitlExtension(a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("PostgreSQL")), &kagenta2a.AskUserResponse{
			Type: adka2a.HITLTypeAskUserResponse, ID: request.ID,
			Answers: []kagenta2a.AskUserAnswer{{Answer: []string{"PostgreSQL"}}},
		})
		reply.TaskID, reply.ContextID = waiting.ID, waiting.ContextID
		if _, err := a2agrpc.NewGRPCTransportFromClient(fixture.client).SendMessage(fixture.ctx, nil, &a2atype.SendMessageRequest{
			Tenant: fixture.tenant, Message: reply, Config: &a2atype.SendMessageConfig{ReturnImmediately: true},
		}); err != nil {
			t.Fatalf("resume A2A task: %v", err)
		}
		select {
		case <-model.held:
		case <-time.After(time.Minute):
			t.Fatal("resumed turn did not call the model")
		}
		cancelRequest, err := pbconv.ToProtoCancelTaskRequest(&a2atype.CancelTaskRequest{Tenant: fixture.tenant, ID: waiting.ID})
		if err != nil {
			t.Fatalf("build CancelTask request: %v", err)
		}
		canceledProto, err := fixture.client.CancelTask(fixture.ctx, cancelRequest)

		// Assert that the resumed turn is cancelled, in the reply and in storage.
		if err != nil {
			t.Fatalf("cancel resumed task: %v", err)
		}
		canceled, err := pbconv.FromProtoTask(canceledProto)
		if err != nil {
			t.Fatalf("decode canceled task: %v", err)
		}
		if canceled.ID != waiting.ID || canceled.Status.State != a2atype.TaskStateCanceled {
			t.Fatalf("canceled task = %s in %s, want %s in CANCELED", canceled.ID, canceled.Status.State, waiting.ID)
		}
		if persisted := getTask(t, fixture, waiting.ID); persisted.Status.State != a2atype.TaskStateCanceled {
			t.Fatalf("persisted task state = %s, want CANCELED", persisted.Status.State)
		}
	})
}

// answerBlockingModel serves the ask-user mock until holdNextCalls, then holds
// every later model call open, so the resumed turn stays in progress.
type answerBlockingModel struct {
	url  string
	held <-chan struct{}
	hold atomic.Bool
}

func (m *answerBlockingModel) holdNextCalls() { m.hold.Store(true) }

func startAnswerBlockingMock(t *testing.T, harness testHarness) *answerBlockingModel {
	t.Helper()
	fixture := "mocks/invoke_golang_hitl_ask_user.json"
	if harness.name == codexE2EHarness || harness.name == claudeE2EHarness {
		fixture = "mocks/invoke_native_ask_user.json"
	}
	upstream := mockLLMProxy(t, startMockLLMServer(t, interactionMocks, fixture))
	held, release := make(chan struct{}), make(chan struct{})
	var heldOnce, releaseOnce sync.Once
	model := &answerBlockingModel{held: held}
	recorder := startModelRecorder(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		// Hold only the model call that carries the answer, so the replay has
		// settled its question and the turn is doing ordinary work.
		if !model.hold.Load() || !bytes.Contains(body, []byte("PostgreSQL")) {
			upstream.ServeHTTP(w, r)
			return
		}
		heldOnce.Do(func() { close(held) })
		select {
		case <-r.Context().Done():
		case <-release:
			http.Error(w, "test finished", http.StatusServiceUnavailable)
		}
	}), nil)
	// Registered after the recorder, so it runs before the server closes.
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	model.url = reachableModelURL(t, recorder.URL)
	return model
}
