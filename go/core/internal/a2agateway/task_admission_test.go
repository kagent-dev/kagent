package a2agateway

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type replyBarrierStore struct {
	*database.Client
	readers atomic.Int32
	ready   chan struct{}
}

type initialAdmissionBarrierStore struct {
	*database.Client
	admitted atomic.Int32
	ready    chan struct{}
}

func (s *initialAdmissionBarrierStore) AdmitAgentInstanceTask(ctx context.Context, instanceID string, hash []byte, request *a2atype.SendMessageRequest, task *a2atype.Task) (*database.TaskAdmission, error) {
	admission, err := s.Client.AdmitAgentInstanceTask(ctx, instanceID, hash, request, task)
	if err != nil {
		return nil, err
	}
	if s.admitted.Add(1) == 2 {
		close(s.ready)
	}
	select {
	case <-s.ready:
		return admission, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type countingSendRuntime struct {
	a2aclient.Transport
	sends atomic.Int32
}

func (r *countingSendRuntime) SendMessage(_ context.Context, _ a2aclient.ServiceParams, request *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
	r.sends.Add(1)
	return &a2atype.Task{ID: request.Message.TaskID, ContextID: request.Message.ContextID, Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}}, nil
}

func (r *countingSendRuntime) Destroy() error { return nil }

func TestTwoGatewaysIssueOneRuntimeSend(t *testing.T) {
	client, instance := gatewayPostgresFixture(t)
	store := &initialAdmissionBarrierStore{Client: client, ready: make(chan struct{})}
	runtime := &countingSendRuntime{}
	first := New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, &gatewayTestWorkflow{}, gatewayTestURL)
	second := New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, &gatewayTestWorkflow{}, gatewayTestURL)
	type result struct {
		task *a2atype.Task
		err  error
	}
	results := make(chan result, 2)
	for _, gateway := range []interface {
		SendMessage(context.Context, *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error)
	}{first, second} {
		go func() {
			request := gatewayTestRequest()
			request.Message.ID = "same-message"
			value, err := gateway.SendMessage(gatewayTestContextWithRoute("team-a", instance.Id), request)
			task, _ := value.(*a2atype.Task)
			results <- result{task: task, err: err}
		}()
	}
	firstResult, secondResult := <-results, <-results
	require.NoError(t, firstResult.err)
	require.NoError(t, secondResult.err)
	require.NotNil(t, firstResult.task)
	require.NotNil(t, secondResult.task)
	require.Equal(t, firstResult.task.ID, secondResult.task.ID)
	require.EqualValues(t, 1, runtime.sends.Load())
}

func (s *replyBarrierStore) AdmitAgentInstanceTaskContinuation(ctx context.Context, instanceID string, hash []byte, request *a2atype.SendMessageRequest) (*database.TaskAdmission, error) {
	if s.readers.Add(1) == 2 {
		close(s.ready)
	}
	select {
	case <-s.ready:
		return s.Client.AdmitAgentInstanceTaskContinuation(ctx, instanceID, hash, request)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestPostgresContinuationAdmission(t *testing.T) {
	for _, sameMessage := range []bool{false, true} {
		t.Run(map[bool]string{false: "distinct replies", true: "identical retries"}[sameMessage], func(t *testing.T) {
			client, instance := gatewayPostgresFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("question"))
			task := &a2atype.Task{ID: a2atype.TaskID(message.ID), ContextID: instance.ContextId,
				Status: a2atype.TaskStatus{State: a2atype.TaskStateInputRequired}, History: []*a2atype.Message{message}}
			require.NoError(t, client.StoreAgentInstanceTaskEvent(ctx, instance.Id, task, task, nil))
			store := &replyBarrierStore{Client: client, ready: make(chan struct{})}
			gateway := &Gateway{store: store}
			type outcome struct {
				admitted bool
				err      error
			}
			results := make(chan outcome, 2)
			for i := range 2 {
				go func() {
					req := gatewayTestRequest()
					req.Message.ID = "reply-one"
					if !sameMessage && i == 1 {
						req.Message.ID = "reply-two"
					}
					req.Message.TaskID = task.ID
					prepared, err := gateway.prepareReply(ctx, instance, req)
					results <- outcome{prepared != nil && prepared.claimed, err}
				}()
			}
			admitted := 0
			for range 2 {
				result := <-results
				t.Logf("dispatch=%t err=%v", result.admitted, result.err)
				if sameMessage {
					require.NoError(t, result.err)
				}
				if result.admitted {
					admitted++
				}
			}
			require.Equal(t, 1, admitted, "only one continuation may dispatch from a waiting boundary")
		})
	}
}

func TestTwoGatewaysIssueOneRuntimeContinuation(t *testing.T) {
	client, instance := gatewayPostgresFixture(t)
	question := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("question"))
	question.ID = "question-message"
	task := &a2atype.Task{
		ID: "continuation-task", ContextID: instance.ContextId,
		Status: a2atype.TaskStatus{State: a2atype.TaskStateInputRequired, Message: question},
	}
	question.TaskID, question.ContextID = task.ID, task.ContextID
	require.NoError(t, client.StoreAgentInstanceTaskEvent(t.Context(), instance.Id, task, task, nil))

	store := &replyBarrierStore{Client: client, ready: make(chan struct{})}
	runtime := &countingSendRuntime{}
	first := New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, &gatewayTestWorkflow{}, gatewayTestURL)
	second := New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, &gatewayTestWorkflow{}, gatewayTestURL)
	type result struct {
		task *a2atype.Task
		err  error
	}
	results := make(chan result, 2)
	for _, gateway := range []interface {
		SendMessage(context.Context, *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error)
	}{first, second} {
		go func() {
			reply := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("answer"))
			reply.ID, reply.TaskID = "same-reply", task.ID
			value, err := gateway.SendMessage(gatewayTestContextWithRoute("team-a", instance.Id), &a2atype.SendMessageRequest{Message: reply})
			stored, _ := value.(*a2atype.Task)
			results <- result{task: stored, err: err}
		}()
	}
	firstResult, secondResult := <-results, <-results
	require.NoError(t, firstResult.err)
	require.NoError(t, secondResult.err)
	require.NotNil(t, firstResult.task)
	require.NotNil(t, secondResult.task)
	require.Equal(t, firstResult.task.ID, secondResult.task.ID)
	require.EqualValues(t, 1, runtime.sends.Load())
}

func TestPostgresSuspendRejectsAdmittedTask(t *testing.T) {
	client, instance := gatewayPostgresFixture(t)
	request := gatewayTestRequest()
	request.Message.ID = "admitted-message"
	request.Message.TaskID = "admitted"
	request.Message.ContextID = instance.ContextId
	task := &a2atype.Task{ID: "admitted", ContextID: instance.ContextId, Status: a2atype.TaskStatus{State: a2atype.TaskStateSubmitted}, History: []*a2atype.Message{request.Message}}
	_, err := client.AdmitAgentInstanceTask(t.Context(), instance.Id, []byte("hash"), request, task)
	require.NoError(t, err)
	suspending := proto.CloneOf(instance)
	suspending.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND
	_, err = client.TransitionAgentInstance(t.Context(), suspending, instance.State, instance.Operation)
	require.ErrorIs(t, err, database.ErrConflict, "explicit suspend must reject admitted execution")
}

// gatewayPostgresFixture uses the store API to prepare a runnable instance; the
// caller supplies runtime behavior independently of persistence.
func gatewayPostgresFixture(t *testing.T) (*database.Client, *apiv1alpha1.AgentInstance) {
	t.Helper()
	// testcontainers retains this context for termination, after t.Context ends.
	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	t.Cleanup(cancel)
	connStr, cleanup, err := dbtest.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	require.NoError(t, dbtest.Migrate(connStr, false))
	config, err := pgxpool.ParseConfig(connStr)
	require.NoError(t, err)
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	client := database.NewClient(pool)
	require.NoError(t, client.UpsertAgentTemplateHarnessPair(t.Context(), database.AgentTemplateHarnessPair{
		Namespace: "team-a", AgentTemplateName: "assistant", AgentTemplateUID: "template-uid",
		HarnessName: "kagent", HarnessUID: "harness-uid", DesiredRevision: "revision",
	}))
	require.NoError(t, client.RecordRuntimeRevision(t.Context(), database.RuntimeRevision{
		Revision: "revision", Namespace: "team-a", AgentTemplateName: "assistant", AgentTemplateUID: "template-uid",
		HarnessName: "kagent", HarnessUID: "harness-uid", SourceSnapshot: []byte("{}"),
		AgentCard:          &a2apb.AgentCard{Name: "assistant"},
		EgressDestinations: []string{}, ActorTemplateAtespace: "team-a", ActorTemplateName: "runtime", ActorTemplateUID: "runtime-uid",
	}, true))
	instance, _, err := client.CreateAgentInstance(t.Context(), &apiv1alpha1.AgentInstance{
		Id: uuid.NewString(), Creator: "alice",
		Harness:       &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "kagent"},
		AgentTemplate: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"},
	}, uuid.NewString())
	require.NoError(t, err)
	instance.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY
	instance.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED
	instance.A2AAuthority = "runtime.test"
	instance, err = client.TransitionAgentInstance(t.Context(), instance,
		apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING,
		apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE)
	require.NoError(t, err)
	return client, instance
}
