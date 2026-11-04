package e2e_test

import (
	"context"
	"strings"
	"testing"
	"time"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/util/wait"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// Sequential: pod replacement must not disrupt unrelated live scenarios. The
// interceptor drops a successful public response; Substrate still performs real
// snapshot copying. Interrupted backend phases are covered by the PostgreSQL
// recovery tests, not claimed as live packet-loss injection here.
func TestCheckpointReplayAfterControllerRestart(t *testing.T) {
	prepared := 0
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		fixture := newInteractionFixture(t, harness, interactionTarget(t), startForkMemoryMock(t))
		_, _, task := fixture.send(t, "What is 2+2?")
		request := &apiv1.CreateCheckpointRequest{SessionId: fixture.sessionID, RequestId: uuid.NewString(), ExpectedHeadTaskId: string(task.ID)}
		var lost *apiv1.Checkpoint
		conn, err := grpc.NewClient(interactionTarget(t), grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, options ...grpc.CallOption) error {
				err := invoke(ctx, method, req, reply, cc, options...)
				if err == nil && method == apiv1.CheckpointService_CreateCheckpoint_FullMethodName {
					lost = proto.CloneOf(reply.(*apiv1.CreateCheckpointResponse).Checkpoint)
					return status.Error(codes.Unavailable, "successful checkpoint reply lost")
				}
				return err
			}))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, conn.Close()) })
		require.NoError(t, waitForControllerAPI(t.Context(), conn))
		client := apiv1.NewCheckpointServiceClient(conn)
		require.NoError(t, wait.PollUntilContextTimeout(fixture.ctx, 100*time.Millisecond, time.Minute, true, func(ctx context.Context) (bool, error) {
			_, err := client.CreateCheckpoint(ctx, request)
			if checkpointSnapshotPending(err) {
				return false, nil
			}
			if lost != nil && status.Code(err) == codes.Unavailable {
				return true, nil
			}
			return false, err
		}))
		require.NotNil(t, lost)
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
			defer cancel()
			_, err := fixture.checkpoints.DeleteCheckpoint(ctx, &apiv1.DeleteCheckpointRequest{CheckpointId: lost.Id})
			if status.Code(err) != codes.NotFound {
				require.NoError(t, err)
			}
		})
		prepared++
		// The parent restarts one controller for all harnesses before these
		// continuations resume in parallel.
		t.Parallel()
		replacement := newControllerConn(t, interactionTarget(t))
		checkpoints := apiv1.NewCheckpointServiceClient(replacement)
		replayed, err := checkpoints.CreateCheckpoint(fixture.ctx, request)
		require.NoError(t, err)
		require.Equal(t, lost.Id, replayed.Checkpoint.Id)
		require.Equal(t, lost.HistorySequence, replayed.Checkpoint.HistorySequence)
		listed, err := checkpoints.ListCheckpoints(fixture.ctx, &apiv1.ListCheckpointsRequest{SessionId: fixture.sessionID})
		require.NoError(t, err)
		require.Len(t, listed.Checkpoints, 1, "retry must retain the original checkpoint")
		forked, err := checkpoints.ForkSession(fixture.ctx, &apiv1.ForkSessionRequest{CheckpointId: lost.Id, RequestId: uuid.NewString()})
		require.NoError(t, err)
		fork := forked.Session
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
			defer cancel()
			require.NoError(t, deleteIdleSession(ctx, apiv1.NewSessionServiceClient(replacement), fork.Id))
		})
		forkFixture := *fixture
		forkFixture.client = a2apb.NewA2AServiceClient(replacement)
		forkFixture.sessionID, forkFixture.contextID = fork.Id, fork.ContextId
		_, _, answer := forkFixture.send(t, "What was the answer before the checkpoint?")
		require.True(t, strings.Contains(taskText(answer), "The answer is 4."), "fork did not restore the retained conversation: %s", taskText(answer))
	})
	if prepared > 0 && !t.Failed() {
		restartTestController(t)
	}
}
