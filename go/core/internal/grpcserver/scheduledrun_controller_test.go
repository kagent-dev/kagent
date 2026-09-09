package grpcserver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/a2agateway"
	"github.com/kagent-dev/kagent/go/core/internal/controller/scheduledrun"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

type scheduledControllerWorkflow struct {
	store       *database.Client
	quiesces    atomic.Int32
	failCleanup bool
}

// Simulate restart after A2A acceptance but before saving the task link. Release
// the lease normally so the test need not wait thirty seconds for its expiry.
type lostTaskLinkStore struct {
	*database.Client
	loseTaskLink bool
}

func (s lostTaskLinkStore) UpdateScheduledRunExecution(ctx context.Context, lease database.ScheduledRunExecutionLease, progress database.ScheduledRunExecutionProgress) error {
	if s.loseTaskLink {
		progress.TaskID = ""
	}
	return s.Client.UpdateScheduledRunExecution(ctx, lease, progress)
}

func (w *scheduledControllerWorkflow) Create(ctx context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	return w.store.MarkAgentInstanceReady(ctx, instance.Id, "scheduled-runtime.test")
}

func (w *scheduledControllerWorkflow) Suspend(ctx context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	_, err := w.Quiesce(ctx, instance)
	return instance, err
}

func (w *scheduledControllerWorkflow) Delete(ctx context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	return instance, w.store.DeleteAgentInstance(ctx, instance.Id)
}

func (w *scheduledControllerWorkflow) Quiesce(context.Context, *apiv1alpha1.AgentInstance) (*database.AgentInstanceTaskSnapshot, error) {
	if w.quiesces.Add(1) == 1 && w.failCleanup {
		return nil, errors.New("temporary Substrate outage")
	}
	return &database.AgentInstanceTaskSnapshot{Atespace: "team", URI: "s3://snapshots/snapshot", ContentScope: "FULL"}, nil
}

type scheduledControllerAuth struct {
	authimpl.UnsecureAuthenticator
}

func (a *scheduledControllerAuth) UpstreamAuth(req *http.Request, session auth.Session, target auth.Principal) error {
	if _, ok := session.(auth.ControlPlaneSession); !ok || session.Principal().User.ID != "" || target.Agent.ID == "" {
		return errors.New("expected control-plane session and target agent")
	}
	req.Header.Set("Authorization", "Bearer controller-test-credential")
	return nil
}

type scheduledControllerAuthorizer struct{}

func (scheduledControllerAuthorizer) Check(ctx context.Context, principal auth.Principal, _ auth.Verb, _ auth.Resource) error {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok {
		return errors.New("missing controller session")
	}
	if _, ok := session.(auth.ControlPlaneSession); !ok || principal.User.ID != "" {
		return errors.New("controller impersonated owner")
	}
	return nil
}

type scheduledControllerRuntime struct {
	a2apb.UnimplementedA2AServiceServer
	mu            sync.Mutex
	tasks         map[string]*a2apb.Task
	sends         int
	prompt        string
	state         a2atype.TaskState
	streamRelease <-chan struct{}
	subscriptions atomic.Int32
}

func (r *scheduledControllerRuntime) SendStreamingMessage(req *a2apb.SendMessageRequest, stream grpc.ServerStreamingServer[a2apb.StreamResponse]) error {
	ctx := stream.Context()
	r.mu.Lock()
	// Release the lock before waiting for the test to allow streaming.
	task, err := r.acceptMessage(ctx, req)
	r.mu.Unlock()
	if err != nil {
		return err
	}
	if r.streamRelease == nil {
		// The runtime accepted the message, but the controller lost the response.
		return status.Error(codes.Unavailable, "response lost")
	}
	select {
	case <-r.streamRelease:
	case <-ctx.Done():
		return ctx.Err()
	}
	info := a2atype.TaskInfo{TaskID: a2atype.TaskID(task.GetId()), ContextID: task.GetContextId()}
	for _, event := range []a2atype.Event{
		a2atype.NewArtifactEvent(info, a2atype.NewTextPart("streamed result")),
		a2atype.NewStatusUpdateEvent(info, a2atype.TaskStateCompleted, nil),
	} {
		response, err := pbconv.ToProtoStreamResponse(event)
		if err != nil {
			return err
		}
		if err := stream.Send(response); err != nil {
			return err
		}
	}
	return nil
}

func (r *scheduledControllerRuntime) acceptMessage(ctx context.Context, req *a2apb.SendMessageRequest) (*a2apb.Task, error) {
	if got := metadata.ValueFromIncomingContext(ctx, "authorization"); len(got) != 1 || got[0] != "Bearer controller-test-credential" {
		return nil, status.Error(codes.Unauthenticated, "controller credential missing")
	}
	if len(metadata.ValueFromIncomingContext(ctx, "x-user-id")) != 0 {
		return nil, status.Error(codes.Unauthenticated, "unexpected human credentials")
	}
	send, err := pbconv.FromProtoSendMessageRequest(req)
	if err != nil {
		return nil, err
	}
	r.sends++
	r.prompt = send.Message.Parts[0].Text()
	now := time.Now()
	task, err := pbconv.ToProtoTask(&a2atype.Task{ID: send.Message.TaskID, ContextID: send.Message.ContextID,
		Status: a2atype.TaskStatus{State: r.state, Timestamp: &now}, History: []*a2atype.Message{send.Message}})
	if err != nil {
		return nil, err
	}
	r.tasks[string(send.Message.TaskID)] = task
	return task, nil
}

func (r *scheduledControllerRuntime) GetTask(_ context.Context, req *a2apb.GetTaskRequest) (*a2apb.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task := r.tasks[req.GetId()]
	if task == nil {
		return nil, status.Error(codes.NotFound, "task not found")
	}
	return proto.CloneOf(task), nil
}

func (r *scheduledControllerRuntime) SubscribeToTask(req *a2apb.SubscribeToTaskRequest, stream grpc.ServerStreamingServer[a2apb.StreamResponse]) error {
	r.subscriptions.Add(1)
	task, err := r.GetTask(stream.Context(), &a2apb.GetTaskRequest{Id: req.GetId()})
	if err != nil {
		return err
	}
	if task.GetStatus().GetState() == a2apb.TaskState_TASK_STATE_COMPLETED {
		return status.Error(codes.NotFound, "task stream already finished")
	}
	return stream.Send(&a2apb.StreamResponse{Payload: &a2apb.StreamResponse_Task{Task: task}})
}

func TestScheduledRunControllerThroughGRPC(t *testing.T) {
	for _, tc := range []struct {
		name    string
		state   a2atype.TaskState
		timeout time.Duration
		want    apiv1alpha1.ScheduledRunExecutionState
		stream  bool
	}{
		{"stream outlives reconciliation", a2atype.TaskStateCompleted, time.Minute, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED, true},
		{"lost dispatch response", a2atype.TaskStateCompleted, time.Minute, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED, false},
		{"timeout retries cleanup", a2atype.TaskStateWorking, 2 * time.Second, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT, false},
		{"auth required retains task", a2atype.TaskStateAuthRequired, 3 * time.Second, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, client, _, owner := scheduledRunTestServer(t)
			runtime := &scheduledControllerRuntime{tasks: map[string]*a2apb.Task{}, state: tc.state}
			release := make(chan struct{})
			if tc.stream {
				runtime.streamRelease = release
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			server := grpc.NewServer()
			a2apb.RegisterA2AServiceServer(server, runtime)
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(server.Stop)
			authenticator := &scheduledControllerAuth{}
			dialer, err := a2agateway.NewRuntimeDialer("http://"+listener.Addr().String(), authenticator)
			require.NoError(t, err)
			workflow := &scheduledControllerWorkflow{store: store, failCleanup: tc.state == a2atype.TaskStateWorking}
			created, err := client.CreateScheduledRun(owner, &apiv1alpha1.CreateScheduledRunRequest{
				Harness: &apiv1alpha1.ResourceReference{Namespace: "team", Name: "runtime"}, AgentTemplate: &apiv1alpha1.ResourceReference{Namespace: "team", Name: "report"}, RequestId: "worker",
				Config: &apiv1alpha1.ScheduledRunConfig{Schedule: "* * * * *", Paused: true, Prompt: "immutable scheduled prompt", ExecutionTimeout: durationpb.New(tc.timeout)},
			})
			require.NoError(t, err)
			trigger := &apiv1alpha1.TriggerScheduledRunRequest{ScheduledRunId: created.ScheduledRun.Id, RequestId: "firing"}
			accepted, err := client.TriggerScheduledRun(owner, trigger)
			require.NoError(t, err)
			// Accepted work survives both editing and deleting its parent schedule.
			_, err = client.DeleteScheduledRun(owner, &apiv1alpha1.DeleteScheduledRunRequest{ScheduledRunId: created.ScheduledRun.Id})
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			controllerStore := lostTaskLinkStore{Client: store}
			if tc.want == apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED && !tc.stream {
				controllerStore.loseTaskLink = true
			}
			controller := scheduledrun.NewController(controllerStore, workflow, a2agateway.New(store, scheduledControllerAuthorizer{}, dialer, workflow, "http://gateway.test"))
			go func() { done <- controller.Start(ctx) }()
			t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
			if tc.want == apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED {
				var running *apiv1alpha1.ScheduledRunExecution
				require.Eventually(t, func() bool {
					response, err := client.GetScheduledRunExecution(owner, &apiv1alpha1.GetScheduledRunExecutionRequest{ExecutionId: accepted.Execution.Id})
					if err != nil {
						return false
					}
					running = response.Execution
					return running.State == apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING && (running.TaskId != "") == tc.stream
				}, 5*time.Second, 20*time.Millisecond)
				cancel()
				require.NoError(t, <-done)
				close(done)
				if tc.stream {
					// The controller has returned and its context is canceled. Only
					// the original stream can persist this result: no subscription runs.
					close(release)
					require.Eventually(t, func() bool {
						task, err := store.GetAgentInstanceTask(t.Context(), running.AgentInstanceId, running.TaskId)
						return err == nil && task.Status.State == a2atype.TaskStateCompleted
					}, 5*time.Second, 20*time.Millisecond)
					require.Zero(t, runtime.subscriptions.Load())
				}
				ctx, cancel = context.WithCancel(t.Context())
				defer cancel()
				done = make(chan error, 1)
				controller = scheduledrun.NewController(store, workflow, a2agateway.New(store, scheduledControllerAuthorizer{}, dialer, workflow, "http://gateway.test"))
				go func() { done <- controller.Start(ctx) }()
			}
			var execution *apiv1alpha1.ScheduledRunExecution
			require.Eventually(t, func() bool {
				response, err := client.GetScheduledRunExecution(owner, &apiv1alpha1.GetScheduledRunExecutionRequest{ExecutionId: accepted.Execution.Id})
				if err != nil {
					return false
				}
				execution = response.Execution
				return execution.State == tc.want
			}, 15*time.Second, 50*time.Millisecond)
			require.NotNil(t, execution.CompletedAt)
			replayed, err := client.TriggerScheduledRun(owner, trigger)
			require.NoError(t, err)
			require.Equal(t, execution.Id, replayed.Execution.Id)
			require.NotEmpty(t, execution.TaskId)
			require.NotEqual(t, execution.Id, execution.TaskId)
			require.NotEmpty(t, execution.AgentInstanceId)
			instance, err := store.GetAgentInstance(t.Context(), execution.AgentInstanceId, "alice")
			require.NoError(t, err)
			require.Equal(t, "alice", instance.Creator)
			task, err := store.GetAgentInstanceTask(t.Context(), instance.Id, execution.TaskId)
			require.NoError(t, err)
			wantTaskState := tc.state
			if tc.state == a2atype.TaskStateWorking {
				wantTaskState = a2atype.TaskStateCanceled
				require.GreaterOrEqual(t, workflow.quiesces.Load(), int32(2))
			}
			require.Equal(t, wantTaskState, task.Status.State)
			if tc.stream {
				require.Zero(t, runtime.subscriptions.Load())
				require.Len(t, task.Artifacts, 1)
				require.Equal(t, "streamed result", task.Artifacts[0].Parts[0].Text())
			}
			runtime.mu.Lock()
			defer runtime.mu.Unlock()
			require.Equal(t, 1, runtime.sends)
			require.Equal(t, "immutable scheduled prompt", runtime.prompt)
		})
	}
}
