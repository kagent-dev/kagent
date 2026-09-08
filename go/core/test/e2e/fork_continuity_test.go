// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"context"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	adka2a "github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

func TestAgentInstancePausedTaskForkContinuity(t *testing.T) {
	fixture := newInteractionFixture(t, interactionTarget(t), startMockLLM(t, "mocks/invoke_golang_hitl_ask_user.json"))
	fixture.ctx = metadata.AppendToOutgoingContext(fixture.ctx, strings.ToLower(a2atype.SvcParamExtensions), adka2a.HITLExtensionURI)
	_, _, waiting := fixture.send(t, "Which database should we use for storage?")
	require.Equal(t, a2atype.TaskStateInputRequired, waiting.Status.State)
	question := adka2a.GetAskUserRequest(waiting.Status.Message)
	require.NotNil(t, question)
	created, err := fixture.checkpoints.CreateCheckpoint(fixture.ctx, &apiv1alpha1.CreateCheckpointRequest{
		AgentInstanceId: fixture.instanceID, RequestId: uuid.NewString(),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cancel()
		_, err := fixture.checkpoints.DeleteCheckpoint(ctx, &apiv1alpha1.DeleteCheckpointRequest{CheckpointId: created.GetCheckpoint().GetId()})
		require.NoError(t, err)
	})
	resume := func(ctx context.Context) *a2atype.Task {
		t.Helper()
		reply := adka2a.AttachHitlExtension(a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("PostgreSQL")), &adka2a.AskUserResponse{
			Type: adka2a.HITLTypeAskUserResponse, ID: question.ID,
			Answers: []adka2a.AskUserAnswer{{Answer: []string{"PostgreSQL"}}},
		})
		reply.TaskID, reply.ContextID = waiting.ID, waiting.ContextID
		result, err := a2agrpc.NewGRPCTransportFromClient(fixture.client).SendMessage(ctx, nil, &a2atype.SendMessageRequest{Message: reply})
		require.NoError(t, err)
		completed, ok := result.(*a2atype.Task)
		require.True(t, ok)
		require.Equal(t, waiting.ID, completed.ID)
		require.Equal(t, waiting.ContextID, completed.ContextID)
		require.Equal(t, a2atype.TaskStateCompleted, completed.Status.State)
		require.Contains(t, taskText(completed), "Using PostgreSQL")
		return completed
	}
	// Advance the same source task before forking, not just a later task.
	resume(fixture.ctx)
	get, err := pbconv.ToProtoGetTaskRequest(&a2atype.GetTaskRequest{ID: waiting.ID})
	require.NoError(t, err)
	sourceBefore, err := fixture.client.GetTask(fixture.ctx, get)
	require.NoError(t, err)
	forked, err := fixture.checkpoints.ForkAgentInstance(fixture.ctx, &apiv1alpha1.ForkAgentInstanceRequest{
		CheckpointId: created.GetCheckpoint().GetId(), RequestId: uuid.NewString(),
	})
	require.NoError(t, err)
	fork := forked.GetAgentInstance()
	require.NotEqual(t, fixture.instanceID, fork.GetId())
	require.Equal(t, fixture.contextID, fork.GetContextId())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cancel()
		_, err := fixture.instances.DeleteAgentInstance(ctx, &apiv1alpha1.DeleteAgentInstanceRequest{AgentInstanceId: fork.GetId()})
		require.NoError(t, err)
	})
	forkCtx := metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "e2e",
		"x-kagent-agent-instance-id", fork.GetId(), strings.ToLower(a2atype.SvcParamExtensions), adka2a.HITLExtensionURI)
	copied, err := fixture.client.GetTask(forkCtx, get)
	require.NoError(t, err)
	copiedTask, err := pbconv.FromProtoTask(copied)
	require.NoError(t, err)
	require.Equal(t, a2atype.TaskStateInputRequired, copiedTask.Status.State)
	copiedQuestion := adka2a.GetAskUserRequest(copiedTask.Status.Message)
	require.NotNil(t, copiedQuestion)
	require.Equal(t, question.ID, copiedQuestion.ID)
	resume(forkCtx)
	sourceAfter, err := fixture.client.GetTask(fixture.ctx, get)
	require.NoError(t, err)
	require.True(t, proto.Equal(sourceBefore, sourceAfter), "fork continuation changed source history")
}
