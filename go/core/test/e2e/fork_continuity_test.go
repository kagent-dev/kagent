// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	adka2a "github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

func TestSessionPausedTaskCheckpointRejected(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		fixture := newInteractionFixture(t, harness, interactionTarget(t), startAskUserMock(t, harness))
		fixture.ctx = metadata.AppendToOutgoingContext(fixture.ctx, strings.ToLower(a2atype.SvcParamExtensions), adka2a.HITLExtensionURI)
		_, _, waiting := fixture.send(t, "Which database should we use for storage?")
		require.Equal(t, a2atype.TaskStateInputRequired, waiting.Status.State)
		require.NotNil(t, adka2a.GetAskUserRequest(waiting.Status.Message))

		assertActorSuspended(t, fixture)
		_, err := fixture.checkpoints.CreateCheckpoint(fixture.ctx, &apiv1.CreateCheckpointRequest{
			SessionId: fixture.sessionID, RequestId: uuid.NewString(), ExpectedHeadTaskId: string(waiting.ID),
		})
		require.Equal(t, codes.FailedPrecondition, status.Code(err))
	})
}
