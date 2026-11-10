package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/checkpoint"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestCheckpointToolsHideStorageFailures(t *testing.T) {
	for _, phase := range []string{"reserve failure", "claim failure", "list failure", "fork failure", "delete claim failure"} {
		t.Run("Given "+phase+", when the checkpoint tool is called, assert that internal details stay private", func(t *testing.T) {
			store := &checkpointErrorStore{phase: phase}
			tool := createCheckpointToolName
			args := map[string]any{"session_id": testSessionID, "expected_head_task_id": testTaskID, "request_id": "checkpoint-request"}
			if phase == "list failure" {
				tool = listCheckpointsToolName
				args = map[string]any{"session_id": testSessionID}
			}
			if phase == "fork failure" {
				tool = forkSessionToolName
				args = map[string]any{"checkpoint_id": "33333333-3333-4333-8333-333333333333", "request_id": "checkpoint-fork"}
			}
			if phase == "delete claim failure" {
				tool = deleteCheckpointToolName
				args = map[string]any{"checkpoint_id": "33333333-3333-4333-8333-333333333333"}
			}
			result := checkpointToolResponse(t, checkpoint.NewService(store, &auth.NoopAuthorizer{}, nil, nil), tool, args)
			text := result["content"].([]any)[0].(map[string]any)["text"].(string)
			require.Equal(t, true, result["isError"])
			require.Equal(t, 1, store.failures, "the intended failure must be reached")
			require.NotContains(t, text, "private-db.internal", "MCP must not expose the storage cause")
		})
	}
}

func TestCheckpointToolsRetainTerminalIdentity(t *testing.T) {
	store := &checkpointErrorStore{phase: "failed"}
	h := &Handler{checkpoints: checkpoint.NewService(store, &auth.NoopAuthorizer{}, nil, nil)}
	result := checkpointToolResponse(t, h.checkpoints, createCheckpointToolName, map[string]any{
		"session_id": testSessionID, "expected_head_task_id": testTaskID, "request_id": "failed-request"})
	require.Equal(t, true, result["isError"])
	meta := result["_meta"].(map[string]any)
	require.Equal(t, "FailedPrecondition", meta["kagent.dev/grpc-code"])
	require.Equal(t, "KAGENT_CHECKPOINT_FAILED", meta["kagent.dev/error-reason"])
	id := meta["kagent.dev/checkpoint-id"].(string)
	require.Equal(t, "33333333-3333-4333-8333-333333333333", id)
	read, output, err := h.getCheckpoint(authContext(), nil, CheckpointIDInput{CheckpointID: id})
	require.NoError(t, err)
	require.False(t, read.IsError)
	require.Equal(t, id, output.Checkpoint.ID)
	require.NotContains(t, output.Checkpoint.Failure.Message, "private")
}

func TestCheckpointToolsRetainCurrentBackendStatus(t *testing.T) {
	for _, code := range []codes.Code{codes.PermissionDenied, codes.FailedPrecondition, codes.InvalidArgument, codes.Unauthenticated, codes.Unavailable, codes.Aborted} {
		t.Run("Given backend "+code.String()+", when Delete is called, assert that the current code survives without private details", func(t *testing.T) {
			backend := &checkpointErrorBackend{err: status.Error(code, "private backend endpoint")}
			h := &Handler{checkpoints: checkpoint.NewService(&checkpointErrorStore{phase: "backend"}, &auth.NoopAuthorizer{}, backend, nil)}
			result, _, err := h.deleteCheckpoint(authContext(), nil, CheckpointIDInput{CheckpointID: "33333333-3333-4333-8333-333333333333"})
			require.NoError(t, err)
			require.True(t, result.IsError)
			require.Equal(t, 1, backend.calls)
			require.Equal(t, code.String(), result.Meta["kagent.dev/grpc-code"])
			require.NotContains(t, fmt.Sprint(result.Content), "private")
		})
	}
}

func TestCheckpointToolsSanitizeWrappedCancellation(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		store := &checkpointErrorStore{phase: "claim failure", claimErr: fmt.Errorf("private database endpoint: %w", cause)}
		h := &Handler{checkpoints: checkpoint.NewService(store, &auth.NoopAuthorizer{}, nil, nil)}
		result, _, err := h.createCheckpoint(authContext(), nil, CreateCheckpointInput{
			SessionID: testSessionID, ExpectedHeadTaskID: testTaskID, RequestID: "canceled-request"})
		require.NoError(t, err)
		require.True(t, result.IsError)
		require.Equal(t, status.FromContextError(cause).Code().String(), result.Meta["kagent.dev/grpc-code"])
		require.NotContains(t, fmt.Sprint(result.Content), "private")
	}
}

func checkpointToolResponse(t *testing.T, service *checkpoint.Service, tool string, args map[string]any) map[string]any {
	t.Helper()
	h, err := New(Config{Agents: testAgentService(t), Sessions: testSessionService(), Checkpoints: service, Gateway: &a2asrv.InterceptedHandler{Handler: &fakeGateway{}}})
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r.WithContext(authContext())) }))
	t.Cleanup(server.Close)
	response := rawMCPCall(t, server.URL, "tools/call", map[string]any{"name": tool, "arguments": args}, false)
	return response["result"].(map[string]any)
}

type checkpointErrorStore struct {
	*database.Client
	phase    string
	failures int
	claimErr error
}

func (s *checkpointErrorStore) ReserveSessionCheckpoint(_ context.Context, cp *apiv1.Checkpoint, _ string, _ string, _ uuid.UUID) (*apiv1.Checkpoint, *database.SessionTaskSnapshot, error) {
	if s.phase == "reserve failure" {
		s.failures++
		return nil, nil, errors.New("connect to private-db.internal user=controller database=kagent: connection refused")
	}
	cp.Id = "33333333-3333-4333-8333-333333333333"
	cp.State = apiv1.CheckpointState_CHECKPOINT_STATE_READY
	if s.phase == "claim failure" {
		cp.State = apiv1.CheckpointState_CHECKPOINT_STATE_CREATING
	}
	if s.phase == "failed" {
		cp.State = apiv1.CheckpointState_CHECKPOINT_STATE_FAILED
		cp.Failure = &apiv1.Failure{Message: "private legacy failure"}
	}
	return cp, &database.SessionTaskSnapshot{}, nil
}
func (s *checkpointErrorStore) ClaimCheckpointOperation(context.Context, string, string, uuid.UUID) (*database.CheckpointOperation, error) {
	s.failures++
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	return nil, errors.New("claim checkpoint operation: private-db.internal user=controller database=kagent: connection refused")
}
func (s *checkpointErrorStore) ReleaseCheckpointOperation(context.Context, string, uuid.UUID) error {
	return nil
}
func (s *checkpointErrorStore) ReleaseCheckpointReservation(context.Context, string, string, uuid.UUID) error {
	return nil
}
func (s *checkpointErrorStore) ListSessionCheckpoints(context.Context, string, string, string, int) ([]*apiv1.Checkpoint, error) {
	s.failures++
	return nil, errors.New("query private-db.internal user=controller database=kagent: connection refused")
}
func (s *checkpointErrorStore) GetSessionCheckpointForkSource(context.Context, string, string) (*database.SessionCheckpointForkSource, error) {
	s.failures++
	return nil, errors.New("read fork source from private-db.internal user=controller database=kagent: connection refused")
}
func (s *checkpointErrorStore) BeginDeleteSessionCheckpoint(context.Context, string, string, uuid.UUID) (*database.CheckpointOperation, error) {
	if s.phase == "backend" {
		return &database.CheckpointOperation{Checkpoint: &apiv1.Checkpoint{Id: "33333333-3333-4333-8333-333333333333", State: apiv1.CheckpointState_CHECKPOINT_STATE_DELETING}, Creator: "alice", Snapshot: &database.SessionTaskSnapshot{Atespace: "team-a"}}, nil
	}
	return nil, database.ErrConflict
}

func (s *checkpointErrorStore) GetSessionCheckpoint(context.Context, string, string) (*apiv1.Checkpoint, error) {
	return &apiv1.Checkpoint{Id: "33333333-3333-4333-8333-333333333333", State: apiv1.CheckpointState_CHECKPOINT_STATE_FAILED, Failure: &apiv1.Failure{Message: "private legacy failure"}}, nil
}

type checkpointErrorBackend struct {
	*substrate.Client
	err   error
	calls int
}

func (b *checkpointErrorBackend) GetTag(context.Context, string, string) (*ateapipb.Tag, error) {
	b.calls++
	return nil, b.err
}
