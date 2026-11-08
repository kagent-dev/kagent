package checkpoint

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestCreateObservesReplacementCompletion(t *testing.T) {
	persistence := reviewStore(t)
	for _, outcome := range []string{"pending", "ready", "failed", "deleted"} {
		t.Run("Given replacement ownership with outcome "+outcome+", when the original Create finishes, assert that it observes the current result", func(t *testing.T) {
			owner := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
			source, head := recoverySource(t, persistence)
			tags := &testTags{snapshotURI: "s3://snapshots/source"}
			var replacement uuid.UUID
			var checkpointID string
			gate := &checkpointCompletionGate{Client: persistence}
			gate.beforeCreateFinish = func(ctx context.Context, checkpoint *apiv1.Checkpoint, executor uuid.UUID, uid, uri string) {
				checkpointID = checkpoint.Id
				require.NoError(t, persistence.ReleaseCheckpointOperation(ctx, checkpoint.Id, executor))
				replacement = uuid.New()
				op, err := persistence.ClaimCheckpointOperation(ctx, checkpoint.Id, "alice", replacement)
				require.NoError(t, err)
				if outcome == "pending" {
					return
				}
				failure := ""
				if outcome == "failed" {
					failure = "replacement rejected the copy"
				}
				_, err = persistence.FinalizeClaimedSessionCheckpoint(ctx, op.Checkpoint, replacement, uid, uri, failure)
				require.NoError(t, err)
				if outcome == "deleted" {
					deletion := uuid.New()
					_, err := persistence.BeginDeleteSessionCheckpoint(ctx, checkpoint.Id, "alice", deletion)
					require.NoError(t, err)
					require.NoError(t, tags.DeleteTag(ctx, "team-a", tagName(checkpoint.Id), uid))
					require.NoError(t, persistence.DeleteClaimedSessionCheckpoint(ctx, checkpoint.Id, "alice", deletion))
				}
			}
			service := NewService(gate, testAuthorizer{}, tags, nil)
			request := uuid.NewString()
			checkpoint, err := service.Create(owner, source.Id, request, head)
			require.Equal(t, 1, gate.conflicts, "the real store must reject the original executor")
			require.Equal(t, 1, tags.createCalls, "observing completion must not repeat backend work")
			switch outcome {
			case "ready":
				require.NoError(t, err)
				require.Equal(t, checkpointID, checkpoint.Id)
				require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_READY, checkpoint.State)
			case "pending":
				require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeAborted), "%v", err)
				require.Nil(t, checkpoint)
				ctx, cancel := context.WithTimeout(owner, database.RuntimeOperationTimeout)
				defer cancel()
				_, err := persistence.ClaimCheckpointOperation(ctx, checkpointID, "alice", uuid.New())
				require.ErrorIs(t, err, database.ErrConflict, "the stale release must preserve replacement ownership")
				require.NoError(t, persistence.ReleaseCheckpointOperation(ctx, checkpointID, replacement))
				_, err = NewService(persistence, testAuthorizer{}, tags, nil).Create(owner, source.Id, request, head)
				require.NoError(t, err)
			case "failed":
				require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnavailable), "%v", err)
				op, readErr := persistence.GetCheckpointOperation(owner, checkpointID, "alice")
				require.NoError(t, readErr)
				require.Equal(t, "replacement rejected the copy", op.Checkpoint.Failure.Message)
			case "deleted":
				require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound), "%v", err)
				require.Nil(t, checkpoint, "Create must not return an empty successful response")
			}
			if outcome != "deleted" {
				require.NoError(t, NewService(persistence, testAuthorizer{}, tags, nil).Delete(owner, checkpointID))
			}
		})
	}
}

func TestDeleteObservesReplacementCompletion(t *testing.T) {
	persistence := reviewStore(t)
	for _, outcome := range []string{"pending", "deleted"} {
		t.Run("Given replacement ownership with outcome "+outcome+", when the original Delete finishes, assert that it observes the current result", func(t *testing.T) {
			owner := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
			source, head := recoverySource(t, persistence)
			tags := &testTags{snapshotURI: "s3://snapshots/source"}
			service := NewService(persistence, testAuthorizer{}, tags, nil)
			checkpoint, err := service.Create(owner, source.Id, uuid.NewString(), head)
			require.NoError(t, err)
			var replacement uuid.UUID
			gate := &checkpointCompletionGate{Client: persistence}
			gate.beforeDeleteFinish = func(ctx context.Context, id, owner string, executor uuid.UUID) {
				require.NoError(t, persistence.ReleaseCheckpointOperation(ctx, id, executor))
				replacement = uuid.New()
				_, err := persistence.ClaimCheckpointOperation(ctx, id, owner, replacement)
				require.NoError(t, err)
				if outcome == "deleted" {
					require.NoError(t, persistence.DeleteClaimedSessionCheckpoint(ctx, id, owner, replacement))
				}
			}
			err = NewService(gate, testAuthorizer{}, tags, nil).Delete(owner, checkpoint.Id)
			require.Equal(t, 1, gate.conflicts)
			require.Equal(t, 1, tags.deleteCalls, "observing completion must not repeat backend cleanup")
			if outcome == "deleted" {
				require.NoError(t, err, "a replacement already completed deletion")
				_, err = persistence.GetCheckpointOperation(owner, checkpoint.Id, "alice")
				require.ErrorIs(t, err, database.ErrNotFound)
			} else {
				require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeAborted), "%v", err)
				require.NoError(t, persistence.ReleaseCheckpointOperation(owner, checkpoint.Id, replacement))
				require.NoError(t, service.Delete(owner, checkpoint.Id))
			}
		})
	}
}

// Replace ownership after backend work, just before the original conditional
// completion. Store methods perform every transition and reject the stale write.
type checkpointCompletionGate struct {
	*database.Client
	beforeCreateFinish func(context.Context, *apiv1.Checkpoint, uuid.UUID, string, string)
	beforeDeleteFinish func(context.Context, string, string, uuid.UUID)
	conflicts          int
}

func (s *checkpointCompletionGate) FinalizeClaimedSessionCheckpoint(ctx context.Context, checkpoint *apiv1.Checkpoint, executor uuid.UUID, uid, uri, failure string) (*apiv1.Checkpoint, error) {
	s.beforeCreateFinish(ctx, checkpoint, executor, uid, uri)
	result, err := s.Client.FinalizeClaimedSessionCheckpoint(ctx, checkpoint, executor, uid, uri, failure)
	if errors.Is(err, database.ErrConflict) {
		s.conflicts++
	}
	return result, err
}

func (s *checkpointCompletionGate) DeleteClaimedSessionCheckpoint(ctx context.Context, id, owner string, executor uuid.UUID) error {
	s.beforeDeleteFinish(ctx, id, owner, executor)
	err := s.Client.DeleteClaimedSessionCheckpoint(ctx, id, owner, executor)
	if errors.Is(err, database.ErrConflict) {
		s.conflicts++
	}
	return err
}
