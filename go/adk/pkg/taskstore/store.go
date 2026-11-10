// Package taskstore connects the upstream A2A SDK to kagent's private gRPC
// persistence service. It keeps no local copy of public conversation history.
package taskstore

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	sdktaskstore "github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

const (
	// retryBudget bounds one operation's attempts and waits together. A
	// controller restart reaches the runtime as immediate Unavailable from the
	// egress proxy over a connection that stays READY, so WaitForReady never
	// holds the call and an attempt count runs out in under a second. Two
	// minutes keeps the allowance of the former four 30-second attempts.
	retryBudget = 2 * time.Minute
	// The wait before a retry doubles from firstRetryDelay up to maxRetryDelay.
	firstRetryDelay = 100 * time.Millisecond
	maxRetryDelay   = time.Second
)

type Store struct {
	client       *controllerclient.Client
	identityPath string
	budget       time.Duration
}

var _ sdktaskstore.Store = (*Store)(nil)

func New(client *controllerclient.Client, identityPath string) *Store {
	return &Store{client: client, identityPath: identityPath, budget: retryBudget}
}

func (s *Store) sessionID() (string, error) {
	identity, err := os.ReadFile(s.identityPath)
	if err != nil {
		return "", fmt.Errorf("read runtime identity: %w", err)
	}
	id, ok := strings.CutPrefix(strings.TrimSpace(string(identity)), "session-")
	if !ok {
		return "", fmt.Errorf("unexpected runtime actor name")
	}
	if _, err := uuid.Parse(id); err != nil {
		return "", fmt.Errorf("invalid runtime session identity: %w", err)
	}
	return id, nil
}

func (s *Store) callContext(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := s.client.CallContext(ctx, "")
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	md.Delete("authorization")
	md.Delete("x-user-id")
	md.Delete("x-agent-name")
	md.Delete("x-share-token")
	// Substrate replaces this placeholder with the calling Actor's JWT, including
	// after a restore or fork. No credential is kept in the runtime snapshot.
	md.Set("authorization", "Bearer placeholder")
	return metadata.NewOutgoingContext(ctx, md), cancel
}

// Create persists the SDK's initial task and returns its committed version.
func (s *Store) Create(ctx context.Context, task *a2a.Task) (sdktaskstore.TaskVersion, error) {
	wire, err := pbconv.ToProtoTask(task)
	if err != nil {
		return 0, err
	}
	id, err := s.sessionID()
	if err != nil {
		return 0, err
	}
	if executionFailed(ctx) {
		return 0, fmt.Errorf("previous task persistence failed")
	}
	request := &runtimev1.TaskStoreServiceCreateTaskRequest{SessionId: id, Task: wire}
	if state, ok := ctx.Value(executionKey{}).(*execution); ok {
		request.DispatchId = state.dispatchID
		// Finish admitted writes within the retry budget, even if the SDK
		// cancels its writer. Native work still waits for this initial save.
		ctx = context.WithoutCancel(ctx)
	}
	var response *runtimev1.TaskStoreServiceCreateTaskResponse
	err = s.retry(ctx, func(ctx context.Context) error {
		var err error
		response, err = s.client.TaskStoreService().CreateTask(ctx, request, grpc.WaitForReady(true))
		return err
	})
	if err != nil {
		recordSaveFailure(ctx)
		return 0, sdkError(err)
	}
	recordSave(ctx, task, response.Version)
	return sdktaskstore.TaskVersion(response.Version), nil
}

func (s *Store) Update(ctx context.Context, update *sdktaskstore.UpdateRequest) (sdktaskstore.TaskVersion, error) {
	if executionFailed(ctx) {
		return 0, fmt.Errorf("previous task persistence failed")
	}
	task, err := pbconv.ToProtoTask(update.Task)
	if err != nil {
		return 0, err
	}
	event, err := pbconv.ToProtoStreamResponse(update.Event)
	if err != nil {
		return 0, err
	}
	id, err := s.sessionID()
	if err != nil {
		return 0, err
	}
	request := &runtimev1.TaskStoreServiceUpdateTaskRequest{SessionId: id, Task: task, Event: event, ExpectedVersion: int64(update.PrevVersion)}
	if state, ok := ctx.Value(executionKey{}).(*execution); ok {
		request.DispatchId = state.dispatchID
		// a2a-go cancels its writer when an executor fails. Finish any in-flight
		// save and the failure update within our retry budget, preserving a known
		// version for finalization. Cleanup still gates publication and suspension.
		ctx = context.WithoutCancel(ctx)
	}
	var response *runtimev1.TaskStoreServiceUpdateTaskResponse
	err = s.retry(ctx, func(ctx context.Context) error {
		var err error
		response, err = s.client.TaskStoreService().UpdateTask(ctx, request, grpc.WaitForReady(true))
		return err
	})
	if err != nil {
		recordSaveFailure(ctx)
		return 0, sdkError(err)
	}
	recordSave(ctx, update.Task, response.Version)
	return sdktaskstore.TaskVersion(response.Version), nil
}

func (s *Store) Get(ctx context.Context, taskID a2a.TaskID) (*sdktaskstore.StoredTask, error) {
	id, err := s.sessionID()
	if err != nil {
		return nil, err
	}
	var response *runtimev1.TaskStoreServiceGetTaskResponse
	err = s.retry(ctx, func(ctx context.Context) error {
		var err error
		response, err = s.client.TaskStoreService().GetTask(ctx, &runtimev1.TaskStoreServiceGetTaskRequest{SessionId: id, TaskId: string(taskID)}, grpc.WaitForReady(true))
		return err
	})
	if err != nil {
		return nil, sdkError(err)
	}
	current, err := fromStored(response.Stored)
	if err != nil {
		return nil, err
	}
	return current, nil
}

func (s *Store) List(ctx context.Context, request *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	wire, err := pbconv.ToProtoListTasksRequest(request)
	if err != nil {
		return nil, err
	}
	id, err := s.sessionID()
	if err != nil {
		return nil, err
	}
	var response *runtimev1.TaskStoreServiceListTasksResponse
	err = s.retry(ctx, func(ctx context.Context) error {
		var err error
		response, err = s.client.TaskStoreService().ListTasks(ctx, &runtimev1.TaskStoreServiceListTasksRequest{SessionId: id, Request: wire}, grpc.WaitForReady(true))
		return err
	})
	if err != nil {
		return nil, sdkError(err)
	}
	return pbconv.FromProtoListTasksResponse(response.Result)
}

// retry repeats a call that fails with Unavailable or DeadlineExceeded until it
// succeeds, fails otherwise, or the store's retry budget ends. Each attempt has
// the client's call timeout within what remains of the budget. It retains the
// same immutable mutation after a lost response, so a replay returns the
// committed version. It never retries conflicts, advances a version locally,
// or invokes the agent again.
func (s *Store) retry(ctx context.Context, call func(context.Context) error) error {
	operation, cancel := context.WithTimeout(ctx, s.budget)
	defer cancel()
	deadline, _ := operation.Deadline()
	// A context reports its deadline only once its timer has fired, which can
	// lag under load, so the clock decides when the budget has passed.
	open := func() bool { return operation.Err() == nil && time.Now().Before(deadline) }
	delay := firstRetryDelay
	var last error
	for attempt := 1; ; attempt++ {
		rpcCtx, cancelRPC := s.callContext(operation)
		err := call(rpcCtx)
		cancelRPC()
		if code := status.Code(err); code != codes.Unavailable && code != codes.DeadlineExceeded {
			return err
		}
		// An attempt cut short by the budget only says that time ran out. Keep
		// the error before it, which names the cause.
		if last == nil || open() {
			last = err
		}
		timer := time.NewTimer(jitter(delay))
		select {
		case <-operation.Done():
			timer.Stop()
		case <-timer.C:
		}
		if open() {
			delay = min(2*delay, maxRetryDelay)
			continue
		}
		// A caller that cancelled or ran out of time has not exhausted retries.
		if callerErr := ctx.Err(); callerErr != nil {
			return callerErr
		}
		if callerDeadline, ok := ctx.Deadline(); ok && !callerDeadline.After(deadline) {
			return context.DeadlineExceeded
		}
		logging.FromContext(ctx).ErrorContext(ctx, "taskstore retries exhausted", "attempts", attempt, "error", last)
		return last
	}
}

// jitter waits between half and all of delay, so runtimes that lost the
// controller together do not retry in step when it returns.
func jitter(delay time.Duration) time.Duration {
	return delay/2 + rand.N(delay/2+1)
}

func fromStored(stored *runtimev1.StoredTask) (*sdktaskstore.StoredTask, error) {
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
	case codes.AlreadyExists:
		return fmt.Errorf("create runtime task: %w", sdktaskstore.ErrTaskAlreadyExists)
	case codes.Aborted:
		return fmt.Errorf("save runtime task: %w", sdktaskstore.ErrConcurrentModification)
	case codes.PermissionDenied, codes.Unauthenticated:
		return a2a.ErrUnauthorized
	case codes.InvalidArgument, codes.FailedPrecondition:
		return a2a.NewError(a2a.ErrInvalidParams, status.Convert(err).Message())
	default:
		return err
	}
}
