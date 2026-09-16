// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
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

func TestAgentInstancePausedTaskCheckpointRejected(t *testing.T) {
	fixture := newInteractionFixture(t, interactionTarget(t), startMockLLM(t, "mocks/invoke_golang_hitl_ask_user.json"))
	fixture.ctx = metadata.AppendToOutgoingContext(fixture.ctx, strings.ToLower(a2atype.SvcParamExtensions), adka2a.HITLExtensionURI)
	_, _, waiting := fixture.send(t, "Which database should we use for storage?")
	require.Equal(t, a2atype.TaskStateInputRequired, waiting.Status.State)
	require.NotNil(t, adka2a.GetAskUserRequest(waiting.Status.Message))

	_, err := fixture.checkpoints.CreateCheckpoint(fixture.ctx, &apiv1alpha1.CreateCheckpointRequest{
		AgentInstanceId: fixture.instanceID, RequestId: uuid.NewString(),
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.ErrorContains(t, err, "no quiescent turn boundary")
}
