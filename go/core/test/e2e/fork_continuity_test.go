// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"context"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	adka2a "github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// checkpointForksSupported is false while Substrate 0.5 snapshots carry process
// memory: kagent rejects forking them because the restored processes would keep
// the source session's IDs. Remove it, and the guards that read it, once
// lifecycle v2 lets templates take DATA snapshots on suspend again.
const checkpointForksSupported = false

// requireForkRejected asserts the rejection checkpointForksSupported describes.
func requireForkRejected(ctx context.Context, t *testing.T, checkpoints apiv1alpha1.CheckpointServiceClient, checkpointID string) {
	t.Helper()
	_, err := checkpoints.ForkSession(ctx, &apiv1alpha1.ForkSessionRequest{CheckpointId: checkpointID, RequestId: uuid.NewString()})
	require.Equal(t, codes.FailedPrecondition, status.Code(err), "fork a process-memory checkpoint: %v", err)
}

func TestSessionPausedTaskCheckpointRejected(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		switch harness.name {
		case codexE2EHarness, claudeE2EHarness:
			t.Skip("native ask-user model fixtures are not available yet; this fixture calls the Go ADK ask_user tool")
		}
		fixture := newInteractionFixture(t, harness, interactionTarget(t), startMockLLM(t, "mocks/invoke_golang_hitl_ask_user.json"))
		fixture.ctx = metadata.AppendToOutgoingContext(fixture.ctx, strings.ToLower(a2atype.SvcParamExtensions), adka2a.HITLExtensionURI)
		_, _, waiting := fixture.send(t, "Which database should we use for storage?")
		require.Equal(t, a2atype.TaskStateInputRequired, waiting.Status.State)
		require.NotNil(t, adka2a.GetAskUserRequest(waiting.Status.Message))

		_, err := fixture.checkpoints.CreateCheckpoint(fixture.ctx, &apiv1alpha1.CreateCheckpointRequest{
			SessionId: fixture.sessionID, RequestId: uuid.NewString(), ExpectedHeadTaskId: string(waiting.ID),
		})
		require.Equal(t, codes.FailedPrecondition, status.Code(err))
	})
}
