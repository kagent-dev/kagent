// Package taskstore connects the upstream A2A SDK to kagent's private gRPC
// persistence service. It keeps no local copy of public conversation history.
package taskstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	sdktaskstore "github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type Store struct {
	a2asrv.PassthroughCallInterceptor
	client       *controllerclient.Client
	identityPath string
	reservation  interface{ ReservedTaskID() a2a.TaskID }
}

var _ sdktaskstore.Store = (*Store)(nil)
var _ a2asrv.CallInterceptor = (*Store)(nil)

func New(client *controllerclient.Client, identityPath string, executor a2asrv.AgentExecutor) *Store {
	reservation, _ := executor.(interface{ ReservedTaskID() a2a.TaskID })
	return &Store{client: client, identityPath: identityPath, reservation: reservation}
}

// Before resolves the projected actor identity and durably admits
// an input before the SDK can invoke the executor. A public retry returns the
// stored task directly. It never reaches the native runner a second time.
func (s *Store) Before(ctx context.Context, call *a2asrv.CallContext, request *a2asrv.Request) (context.Context, any, error) {
	if _, ok := ctx.Value(seedKey{}).(*seed); !ok {
		ctx = context.WithValue(ctx, seedKey{}, &seed{})
	}
	instanceID, err := s.instanceID()
	if err != nil {
		return ctx, nil, err
	}
	send, ok := request.Payload.(*a2a.SendMessageRequest)
	if !ok {
		return ctx, nil, nil
	}
	wire, err := pbconv.ToProtoSendMessageRequest(send)
	if err != nil {
		return ctx, nil, err
	}
	input := &apiv1alpha1.TaskStoreServiceAdmitMessageRequest{AgentInstanceId: instanceID, AdmissionId: uuid.NewString(), Request: wire}
	if s.reservation != nil {
		input.ReservedTaskId = string(s.reservation.ReservedTaskID())
	}
	var response *apiv1alpha1.TaskStoreServiceAdmitMessageResponse
	err = s.retry(ctx, func(ctx context.Context) error {
		var err error
		response, err = s.client.TaskStoreService().AdmitMessage(ctx, input)
		return err
	})
	if err != nil {
		if status.Code(err) == codes.Aborted {
			return ctx, nil, a2a.NewError(a2a.ErrUnsupportedOperation, "instance already has active work or a pending lifecycle operation")
		}
		return ctx, nil, sdkError(err)
	}
	current, err := fromStored(response.Current)
	if err != nil {
		return ctx, nil, err
	}
	if !response.Admitted || current.Task.Status.State.Terminal() {
		return ctx, current.Task, nil
	}
	// Use the canonical admitted message (including its assigned task/context
	// IDs) and skip an SDK history write for this already committed input.
	for _, message := range current.Task.History {
		if message.ID == send.Message.ID {
			send.Message = message
			break
		}
	}
	if send.Message.TaskID != current.Task.ID {
		return ctx, nil, fmt.Errorf("admitted task does not contain its input message")
	}
	if response.Previous != nil {
		previous, err := pbconv.FromProtoTask(response.Previous)
		if err != nil {
			return ctx, nil, err
		}
		previous.History = current.Task.History
		current.Task = previous
	}
	return context.WithValue(ctx, seedKey{}, &seed{task: current}), nil, nil
}

// Read the projection for every operation. A data-only fork starts a new actor
// with its own identity; no copied state or caller header selects its history.
func (s *Store) instanceID() (string, error) {
	identity, err := os.ReadFile(s.identityPath)
	if err != nil {
		return "", fmt.Errorf("read runtime identity: %w", err)
	}
	id, ok := strings.CutPrefix(strings.TrimSpace(string(identity)), "ai-")
	if !ok {
		return "", fmt.Errorf("unexpected runtime actor name")
	}
	if _, err := uuid.Parse(id); err != nil {
		return "", fmt.Errorf("invalid runtime instance identity: %w", err)
	}
	return id, nil
}

func (s *Store) callContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	ctx, cancel := s.client.CallContext(ctx, "")
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	// Substrate replaces this placeholder with the actor JWT on egress. The
	// actor never receives or refreshes credentials, including on reconnect.
	md.Set("authorization", "Bearer substrate-actor")
	md.Delete("x-user-id")
	md.Delete("x-agent-name")
	md.Delete("x-share-token")
	md.Delete(apia2a.InsecureRuntimeIdentityHeader)
	if os.Getenv(apia2a.InsecureTaskStoreAuthEnv) == "true" {
		var identity []string
		for _, field := range []string{"atespace", "name", "uid"} {
			value, err := os.ReadFile(filepath.Join(filepath.Dir(s.identityPath), field))
			if err != nil {
				cancel()
				return nil, nil, fmt.Errorf("read insecure runtime identity %s: %w", field, err)
			}
			identity = append(identity, strings.TrimSpace(string(value)))
		}
		md.Delete("authorization")
		md.Set(apia2a.InsecureRuntimeIdentityHeader, strings.Join(identity, "/"))
	}
	return metadata.NewOutgoingContext(ctx, md), cancel, nil
}

// Create is unreachable for properly admitted inputs: Before assigns their
// task ID and seeds the SDK's first Get. Refuse unadmitted SDK task creation.
func (*Store) Create(context.Context, *a2a.Task) (sdktaskstore.TaskVersion, error) {
	return 0, fmt.Errorf("runtime tasks must be admitted before execution: %w", sdktaskstore.ErrTaskAlreadyExists)
}

func (s *Store) Update(ctx context.Context, update *sdktaskstore.UpdateRequest) (sdktaskstore.TaskVersion, error) {
	task, err := pbconv.ToProtoTask(update.Task)
	if err != nil {
		return 0, err
	}
	event, err := pbconv.ToProtoStreamResponse(update.Event)
	if err != nil {
		return 0, err
	}
	id, err := s.instanceID()
	if err != nil {
		return 0, err
	}
	request := &apiv1alpha1.TaskStoreServiceUpdateTaskRequest{AgentInstanceId: id, Task: task, Event: event, ExpectedVersion: int64(update.PrevVersion)}
	var response *apiv1alpha1.TaskStoreServiceUpdateTaskResponse
	err = s.retry(ctx, func(ctx context.Context) error {
		var err error
		response, err = s.client.TaskStoreService().UpdateTask(ctx, request)
		return err
	})
	if err != nil {
		return 0, sdkError(err)
	}
	if update.Task.Status.State.Terminal() || update.Task.Status.State == a2a.TaskStateInputRequired || update.Task.Status.State == a2a.TaskStateAuthRequired {
		if state, ok := ctx.Value(seedKey{}).(*seed); ok {
			state.boundary.Store(response.Version)
		}
	}
	return sdktaskstore.TaskVersion(response.Version), nil
}

type seedKey struct{}
type seed struct {
	task     *sdktaskstore.StoredTask
	used     atomic.Bool
	boundary atomic.Int64
	cleaned  bool // guarded by settledExecutor.mu
}

func (s *Store) Get(ctx context.Context, taskID a2a.TaskID) (*sdktaskstore.StoredTask, error) {
	id, err := s.instanceID()
	if err != nil {
		return nil, err
	}
	var response *apiv1alpha1.TaskStoreServiceGetTaskResponse
	err = s.retry(ctx, func(ctx context.Context) error {
		var err error
		response, err = s.client.TaskStoreService().GetTask(ctx, &apiv1alpha1.TaskStoreServiceGetTaskRequest{AgentInstanceId: id, TaskId: string(taskID)})
		return err
	})
	if err != nil {
		return nil, sdkError(err)
	}
	current, err := fromStored(response.Stored)
	if err != nil {
		return nil, err
	}
	if initial, ok := ctx.Value(seedKey{}).(*seed); ok && initial.task != nil && initial.task.Task.ID == taskID && initial.used.CompareAndSwap(false, true) {
		// A cancel can commit while the admission response is in flight,
		// before the SDK registers its execution. Never dispatch a stale seed.
		if initial.task.Version != current.Version {
			return nil, sdktaskstore.ErrConcurrentModification
		}
		return initial.task, nil
	}
	return current, nil
}

func (s *Store) List(ctx context.Context, request *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	wire, err := pbconv.ToProtoListTasksRequest(request)
	if err != nil {
		return nil, err
	}
	id, err := s.instanceID()
	if err != nil {
		return nil, err
	}
	var response *apiv1alpha1.TaskStoreServiceListTasksResponse
	err = s.retry(ctx, func(ctx context.Context) error {
		var err error
		response, err = s.client.TaskStoreService().ListTasks(ctx, &apiv1alpha1.TaskStoreServiceListTasksRequest{AgentInstanceId: id, Request: wire})
		return err
	})
	if err != nil {
		return nil, sdkError(err)
	}
	return pbconv.FromProtoListTasksResponse(response.Result)
}

// retry retains the same immutable mutation after a lost response. It never
// retries conflicts, advances a version locally, or invokes the agent again.
func (s *Store) retry(ctx context.Context, call func(context.Context) error) error {
	for attempt := range 4 {
		rpcCtx, cancel, err := s.callContext(ctx)
		if err != nil {
			return err
		}
		err = call(rpcCtx)
		cancel()
		if (status.Code(err) != codes.Unavailable && status.Code(err) != codes.DeadlineExceeded) || attempt == 3 {
			return err
		}
		timer := time.NewTimer(time.Duration(1<<attempt) * 100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	panic("unreachable")
}

func fromStored(stored *apiv1alpha1.StoredTask) (*sdktaskstore.StoredTask, error) {
	if stored == nil || stored.Task == nil || stored.Version <= 0 {
		return nil, fmt.Errorf("TaskStore returned an invalid stored task")
	}
	task, err := pbconv.FromProtoTask(stored.Task)
	if err != nil {
		return nil, err
	}
	return &sdktaskstore.StoredTask{Task: task, Version: sdktaskstore.TaskVersion(stored.Version)}, nil
}

func sdkError(err error) error {
	switch status.Code(err) {
	case codes.NotFound:
		return fmt.Errorf("load runtime task: %w", a2a.ErrTaskNotFound)
	case codes.Aborted:
		return fmt.Errorf("save runtime task: %w", sdktaskstore.ErrConcurrentModification)
	case codes.PermissionDenied, codes.Unauthenticated:
		return a2a.ErrUnauthorized
	case codes.InvalidArgument, codes.AlreadyExists, codes.FailedPrecondition:
		return a2a.NewError(a2a.ErrInvalidParams, status.Convert(err).Message())
	default:
		return err
	}
}
