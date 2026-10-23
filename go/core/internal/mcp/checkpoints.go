package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/checkpoint"
)

const (
	createCheckpointToolName = "create_session_checkpoint"
	listCheckpointsToolName  = "list_session_checkpoints"
	forkSessionToolName      = "fork_session"
	getCheckpointToolName    = "get_session_checkpoint"
	deleteCheckpointToolName = "delete_session_checkpoint"
)

type CheckpointIDInput struct {
	CheckpointID string `json:"checkpoint_id" jsonschema:"Checkpoint UUID"`
}

type DeleteCheckpointOutput struct {
	CheckpointID string `json:"checkpoint_id"`
}

type CreateCheckpointInput struct {
	SessionID          string `json:"session_id" jsonschema:"Session UUID"`
	ExpectedHeadTaskID string `json:"expected_head_task_id" jsonschema:"Terminal task ID to save; fails if the conversation has advanced"`
	RequestID          string `json:"request_id,omitempty" jsonschema:"Optional stable request ID for idempotency"`
}

type CheckpointSummary struct {
	ID              string          `json:"id"`
	SessionID       string          `json:"session_id"`
	Name            string          `json:"name,omitempty"`
	HeadTaskID      string          `json:"head_task_id,omitempty"`
	HistorySequence uint64          `json:"history_sequence"`
	State           string          `json:"state"`
	CreatedAt       string          `json:"created_at,omitempty"`
	Failure         *FailureSummary `json:"failure,omitempty"`
}

type FailureSummary struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

type CreateCheckpointOutput struct {
	Checkpoint CheckpointSummary `json:"checkpoint"`
}

type ListCheckpointsInput struct {
	SessionID string `json:"session_id" jsonschema:"Session UUID"`
	PageSize  int    `json:"page_size,omitempty" jsonschema:"Maximum number of checkpoints to return"`
	PageToken string `json:"page_token,omitempty" jsonschema:"Token returned by a previous call"`
}

type ListCheckpointsOutput struct {
	Checkpoints   []CheckpointSummary `json:"checkpoints"`
	NextPageToken string              `json:"next_page_token,omitempty"`
}

type ForkSessionInput struct {
	CheckpointID string `json:"checkpoint_id" jsonschema:"Checkpoint UUID"`
	RequestID    string `json:"request_id,omitempty" jsonschema:"Optional stable request ID for idempotency"`
}

type ForkSessionOutput struct {
	Session SessionSummary `json:"session"`
}

func (h *Handler) registerCheckpointTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: createCheckpointToolName, Description: "Create a checkpoint at a Session turn boundary", Annotations: &mcp.ToolAnnotations{DestructiveHint: new(false)}}, h.createCheckpoint)
	mcp.AddTool(server, &mcp.Tool{Name: listCheckpointsToolName, Description: "List checkpoints for a Session", Annotations: readOnly()}, h.listCheckpoints)
	mcp.AddTool(server, &mcp.Tool{Name: getCheckpointToolName, Description: "Read one checkpoint's state and failure", Annotations: readOnly()}, h.getCheckpoint)
	mcp.AddTool(server, &mcp.Tool{Name: deleteCheckpointToolName, Description: "Delete a checkpoint", Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), IdempotentHint: true}}, h.deleteCheckpoint)
	mcp.AddTool(server, &mcp.Tool{Name: forkSessionToolName, Description: "Create a Session from a checkpoint", Annotations: &mcp.ToolAnnotations{DestructiveHint: new(false)}}, h.forkSession)
}

func (h *Handler) getCheckpoint(ctx context.Context, _ *mcp.CallToolRequest, input CheckpointIDInput) (*mcp.CallToolResult, CreateCheckpointOutput, error) {
	value, err := h.checkpoints.Get(ctx, input.CheckpointID)
	if err != nil {
		return sessionToolError(err), CreateCheckpointOutput{}, nil
	}
	output := CreateCheckpointOutput{Checkpoint: checkpointSummary(value)}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Checkpoint %s is %s", value.GetId(), value.GetState())}}}, output, nil
}

func (h *Handler) deleteCheckpoint(ctx context.Context, _ *mcp.CallToolRequest, input CheckpointIDInput) (*mcp.CallToolResult, DeleteCheckpointOutput, error) {
	if err := h.checkpoints.Delete(ctx, input.CheckpointID); err != nil {
		return sessionToolError(err), DeleteCheckpointOutput{}, nil
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Deleted checkpoint %s", input.CheckpointID)}}}, DeleteCheckpointOutput(input), nil
}

func (h *Handler) createCheckpoint(ctx context.Context, _ *mcp.CallToolRequest, input CreateCheckpointInput) (*mcp.CallToolResult, CreateCheckpointOutput, error) {
	created, err := h.checkpoints.Create(ctx, input.SessionID, stableRequestID(input.RequestID), input.ExpectedHeadTaskID)
	if err != nil {
		result := toolError(err)
		for _, detail := range status.Convert(err).Details() {
			if info, ok := detail.(*errdetails.ErrorInfo); ok && info.Domain == "kagent.dev" {
				result.Meta = mcp.Meta{"kagent.dev/error-reason": info.Reason}
			}
		}
		return result, CreateCheckpointOutput{}, nil
	}
	output := CreateCheckpointOutput{Checkpoint: checkpointSummary(created)}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Created checkpoint %s", created.GetId())}}}, output, nil
}

func (h *Handler) listCheckpoints(ctx context.Context, _ *mcp.CallToolRequest, input ListCheckpointsInput) (*mcp.CallToolResult, ListCheckpointsOutput, error) {
	listed, err := h.checkpoints.List(ctx, checkpoint.ListRequest{
		SessionID: input.SessionID,
		PageSize:  input.PageSize, PageToken: input.PageToken,
	})
	if err != nil {
		return toolError(err), ListCheckpointsOutput{}, nil
	}
	output := ListCheckpointsOutput{Checkpoints: make([]CheckpointSummary, len(listed.Checkpoints)), NextPageToken: listed.NextPageToken}
	for i, item := range listed.Checkpoints {
		output.Checkpoints[i] = checkpointSummary(item)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Found %d checkpoints", len(output.Checkpoints))}}}, output, nil
}

func (h *Handler) forkSession(ctx context.Context, _ *mcp.CallToolRequest, input ForkSessionInput) (*mcp.CallToolResult, ForkSessionOutput, error) {
	session, err := h.checkpoints.Fork(ctx, input.CheckpointID, stableRequestID(input.RequestID))
	if err != nil {
		return toolError(err), ForkSessionOutput{}, nil
	}
	output := ForkSessionOutput{Session: sessionSummary(session)}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Created Session %s", session.GetId())}}}, output, nil
}

func stableRequestID(id string) string {
	if id != "" {
		return id
	}
	return uuid.NewString()
}

func checkpointSummary(value *apiv1.Checkpoint) CheckpointSummary {
	result := CheckpointSummary{
		ID: value.GetId(), SessionID: value.GetSessionId(), Name: value.GetName(),
		HeadTaskID: value.GetHeadTaskId(), HistorySequence: value.GetHistorySequence(), State: value.GetState().String(),
	}
	if value.GetCreatedAt() != nil {
		result.CreatedAt = value.GetCreatedAt().AsTime().Format(time.RFC3339Nano)
	}
	if value.GetFailure() != nil {
		result.Failure = &FailureSummary{Reason: value.GetFailure().GetReason(), Message: value.GetFailure().GetMessage()}
	}
	return result
}

func sessionSummary(session *apiv1.Session) SessionSummary {
	result := SessionSummary{
		ID: session.GetId(), Agent: sessionTenant(session), Name: session.GetName(),
		State: session.GetState().String(),
	}
	if session.GetOperation() != apiv1.RuntimeOperation_RUNTIME_OPERATION_UNSPECIFIED {
		result.Operation = session.GetOperation().String()
	}
	if session.GetFailure() != nil {
		result.Failure = &FailureSummary{Reason: session.GetFailure().GetReason(), Message: session.GetFailure().GetMessage()}
	}
	if session.GetCreatedAt() != nil {
		result.CreatedAt = session.GetCreatedAt().AsTime().Format(time.RFC3339Nano)
	}
	if session.GetUpdatedAt() != nil {
		result.UpdatedAt = session.GetUpdatedAt().AsTime().Format(time.RFC3339Nano)
	}
	return result
}
