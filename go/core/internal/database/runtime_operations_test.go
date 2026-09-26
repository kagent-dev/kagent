package database

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type lifecycleResource interface {
	GetState() apiv1alpha1.RuntimeState
	GetOperation() apiv1alpha1.RuntimeOperation
	GetName() string
	GetFailure() *apiv1alpha1.Failure
	GetCreatedAt() *timestamppb.Timestamp
	GetUpdatedAt() *timestamppb.Timestamp
}

// The same contract is exercised through both owning stores. Resource-specific
// admission remains covered by their task/checkpoint and sandbox lifecycle tests.
func TestRuntimeLifecycleContract(t *testing.T) {
	t.Run("agent", func(t *testing.T) {
		client := NewClient(setupTestDB(t))
		sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
		instance, _, err := client.CreateSession(t.Context(), newSessionRequest(uuid.NewString(), "assistant", "kagent", "conversation"), "create")
		require.NoError(t, err)
		testRuntimeLifecycle(t, func(ctx context.Context, kind apiv1alpha1.RuntimeOperation) (*RuntimeOperation[*apiv1alpha1.Session], error) {
			operation, err := client.BeginSessionOperation(ctx, instance.Id, kind)
			if err != nil {
				return nil, err
			}
			return &operation.RuntimeOperation, nil
		}, func(ctx context.Context, operation, executor uuid.UUID) (bool, error) {
			return client.ClaimSessionOperation(ctx, instance.Id, operation, executor)
		}, func(ctx context.Context, operation, executor uuid.UUID, failure string) (*apiv1alpha1.Session, error) {
			actorUID := ""
			if failure == "" {
				actorUID = "actor-uid"
			}
			return client.FinishSessionOperation(ctx, instance.Id, operation, executor, "runtime.example", actorUID, failure)
		}, func(ctx context.Context, operation, executor uuid.UUID) error {
			return client.ReleaseRuntimeOperation(ctx, instance.Id, operation, executor)
		})
	})
	t.Run("sandbox", func(t *testing.T) {
		client := NewClient(setupTestDB(t))
		input, options := sandboxFixture(t, client, "revision")
		instance, _, err := client.CreateSandbox(t.Context(), input, "create", options)
		require.NoError(t, err)
		testRuntimeLifecycle(t, func(ctx context.Context, kind apiv1alpha1.RuntimeOperation) (*RuntimeOperation[*apiv1alpha1.Sandbox], error) {
			return client.BeginSandboxOperation(ctx, instance.Id, kind)
		}, func(ctx context.Context, operation, executor uuid.UUID) (bool, error) {
			return client.ClaimSandboxOperation(ctx, instance.Id, operation, executor)
		}, func(ctx context.Context, operation, executor uuid.UUID, failure string) (*apiv1alpha1.Sandbox, error) {
			return client.FinishSandboxOperation(ctx, instance.Id, operation, executor, failure)
		}, func(ctx context.Context, operation, executor uuid.UUID) error {
			return client.ReleaseRuntimeOperation(ctx, instance.Id, operation, executor)
		})
	})
}

func testRuntimeLifecycle[T lifecycleResource](t *testing.T,
	begin func(context.Context, apiv1alpha1.RuntimeOperation) (*RuntimeOperation[T], error),
	claim func(context.Context, uuid.UUID, uuid.UUID) (bool, error),
	finish func(context.Context, uuid.UUID, uuid.UUID, string) (T, error),
	release func(context.Context, uuid.UUID, uuid.UUID) error,
) {
	t.Helper()
	ctx := t.Context()
	first, err := begin(ctx, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE)
	require.NoError(t, err)
	require.NotEmpty(t, first.Instance.GetName())
	require.True(t, first.Instance.GetCreatedAt().IsValid())
	_, err = finish(ctx, first.ID, uuid.Nil, "")
	require.ErrorIs(t, err, ErrConflict, "success must require a claim")
	failed, err := finish(ctx, first.ID, uuid.Nil, "backend temporarily unavailable")
	require.NoError(t, err)
	require.Equal(t, "PreparationFailed", failed.GetFailure().GetReason())
	require.Equal(t, "backend temporarily unavailable", failed.GetFailure().GetMessage())
	retry, err := begin(ctx, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE)
	require.NoError(t, err)
	require.True(t, proto.Equal(failed.GetFailure(), retry.Instance.GetFailure()), "failure details must survive a read and retry")
	require.Equal(t, first.Instance.GetName(), retry.Instance.GetName())
	require.NotEqual(t, first.ID, retry.ID)
	claimed, err := claim(ctx, first.ID, uuid.New())
	require.NoError(t, err)
	require.False(t, claimed, "released generations cannot issue work")
	type outcome struct {
		executor uuid.UUID
		claimed  bool
		err      error
	}
	results := make(chan outcome, 8)
	for range cap(results) {
		go func() {
			executor := uuid.New()
			won, err := claim(ctx, retry.ID, executor)
			results <- outcome{executor, won, err}
		}()
	}
	var winner uuid.UUID
	for range cap(results) {
		result := <-results
		require.NoError(t, result.err)
		if result.claimed {
			require.Equal(t, uuid.Nil, winner)
			winner = result.executor
		}
	}
	require.NotEqual(t, uuid.Nil, winner)
	_, err = finish(ctx, retry.ID, winner, "uncertain RPC result")
	require.ErrorIs(t, err, ErrConflict)
	require.NoError(t, release(ctx, retry.ID, winner))
	abandoned := uuid.New()
	attemptCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	claimed, err = claim(attemptCtx, retry.ID, abandoned)
	require.NoError(t, err)
	require.True(t, claimed, "a returned attempt is immediately retryable")
	replacement := uuid.New()
	require.Eventually(t, func() bool {
		won, err := claim(ctx, retry.ID, replacement)
		return err == nil && won
	}, time.Second, 10*time.Millisecond, "a crashed executor must not require background recovery")
	require.NoError(t, release(ctx, retry.ID, abandoned))
	claimed, err = claim(ctx, retry.ID, uuid.New())
	require.NoError(t, err)
	require.False(t, claimed, "stale release cannot release a newer attempt")
	_, err = finish(ctx, retry.ID, abandoned, "")
	require.ErrorIs(t, err, ErrConflict, "stale completion cannot overwrite a newer attempt")
	winner = replacement
	ready, err := finish(ctx, retry.ID, winner, "")
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, ready.GetState())
	require.Nil(t, ready.GetFailure())
	for _, kind := range []apiv1alpha1.RuntimeOperation{apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE} {
		op, err := begin(ctx, kind)
		require.NoError(t, err)
		_, err = finish(ctx, retry.ID, winner, "")
		require.ErrorIs(t, err, ErrConflict, "an older completion cannot overwrite current lifecycle state")
		executor := uuid.New()
		won, err := claim(ctx, op.ID, executor)
		require.NoError(t, err)
		require.True(t, won)
		result, err := finish(ctx, op.ID, executor, "")
		require.NoError(t, err)
		observed, err := begin(ctx, kind)
		require.NoError(t, err)
		require.Equal(t, op.ID, observed.ID, "completion retains the observed generation")
		require.Equal(t, result.GetState(), observed.Instance.GetState())
		require.Equal(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE, observed.Instance.GetOperation())
		require.Equal(t, first.Instance.GetName(), observed.Instance.GetName())
		require.True(t, proto.Equal(first.Instance.GetCreatedAt(), observed.Instance.GetCreatedAt()))
		require.True(t, proto.Equal(result.GetUpdatedAt(), observed.Instance.GetUpdatedAt()), "observing a completed operation must not change its timestamp")
		require.False(t, result.GetUpdatedAt().AsTime().Before(op.Instance.GetUpdatedAt().AsTime()))
		require.Nil(t, observed.Instance.GetFailure())
	}
}
