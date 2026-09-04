package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/checkpoint"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	createCheckpointToolName  = "create_agent_instance_checkpoint"
	listCheckpointsToolName   = "list_agent_instance_checkpoints"
	forkAgentInstanceToolName = "fork_agent_instance"
)

type CreateCheckpointInput struct {
	Namespace       string `json:"namespace" jsonschema:"Kubernetes namespace containing the AgentInstance"`
	AgentInstanceID string `json:"agent_instance_id" jsonschema:"AgentInstance UUID"`
	RequestID       string `json:"request_id,omitempty" jsonschema:"Optional stable request ID for idempotency"`
}

type CheckpointSummary struct {
	ID              string          `json:"id"`
	Namespace       string          `json:"namespace"`
	AgentInstanceID string          `json:"agent_instance_id"`
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
	Namespace       string `json:"namespace" jsonschema:"Kubernetes namespace containing the AgentInstance"`
	AgentInstanceID string `json:"agent_instance_id" jsonschema:"AgentInstance UUID"`
	PageSize        int    `json:"page_size,omitempty" jsonschema:"Maximum number of checkpoints to return"`
	PageToken       string `json:"page_token,omitempty" jsonschema:"Token returned by a previous call"`
}

type ListCheckpointsOutput struct {
	Checkpoints   []CheckpointSummary `json:"checkpoints"`
	NextPageToken string              `json:"next_page_token,omitempty"`
}

type ForkAgentInstanceInput struct {
	Namespace    string `json:"namespace" jsonschema:"Kubernetes namespace containing the checkpoint"`
	CheckpointID string `json:"checkpoint_id" jsonschema:"Checkpoint UUID"`
	RequestID    string `json:"request_id,omitempty" jsonschema:"Optional stable request ID for idempotency"`
}

type ForkAgentInstanceOutput struct {
	AgentInstance AgentInstanceSummary `json:"agent_instance"`
}

func (h *Handler) registerCheckpointTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: createCheckpointToolName, Description: "Create a checkpoint at an AgentInstance turn boundary"}, h.createCheckpoint)
	mcp.AddTool(server, &mcp.Tool{Name: listCheckpointsToolName, Description: "List checkpoints for an AgentInstance"}, h.listCheckpoints)
	mcp.AddTool(server, &mcp.Tool{Name: forkAgentInstanceToolName, Description: "Create an AgentInstance from a checkpoint"}, h.forkAgentInstance)
}

func (h *Handler) createCheckpoint(ctx context.Context, _ *mcp.CallToolRequest, input CreateCheckpointInput) (*mcp.CallToolResult, CreateCheckpointOutput, error) {
	created, err := h.checkpoints.Create(ctx, input.Namespace, input.AgentInstanceID, stableRequestID(input.RequestID))
	if err != nil {
		return toolError(err), CreateCheckpointOutput{}, nil
	}
	output := CreateCheckpointOutput{Checkpoint: checkpointSummary(created)}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Created checkpoint %s", created.GetId())}}}, output, nil
}

func (h *Handler) listCheckpoints(ctx context.Context, _ *mcp.CallToolRequest, input ListCheckpointsInput) (*mcp.CallToolResult, ListCheckpointsOutput, error) {
	listed, err := h.checkpoints.List(ctx, checkpoint.ListRequest{
		Namespace: input.Namespace, InstanceID: input.AgentInstanceID,
		PageSize: input.PageSize, PageToken: input.PageToken,
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

func (h *Handler) forkAgentInstance(ctx context.Context, _ *mcp.CallToolRequest, input ForkAgentInstanceInput) (*mcp.CallToolResult, ForkAgentInstanceOutput, error) {
	instance, err := h.checkpoints.Fork(ctx, input.Namespace, input.CheckpointID, stableRequestID(input.RequestID))
	if err != nil {
		return toolError(err), ForkAgentInstanceOutput{}, nil
	}
	output := ForkAgentInstanceOutput{AgentInstance: agentInstanceSummary(instance)}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Created AgentInstance %s", instance.GetId())}}}, output, nil
}

func stableRequestID(id string) string {
	if id != "" {
		return id
	}
	return uuid.NewString()
}

func checkpointSummary(value *apiv1alpha1.Checkpoint) CheckpointSummary {
	result := CheckpointSummary{
		ID: value.GetId(), Namespace: value.GetNamespace(), AgentInstanceID: value.GetAgentInstanceId(),
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

func agentInstanceSummary(instance *apiv1alpha1.AgentInstance) AgentInstanceSummary {
	return AgentInstanceSummary{
		Namespace: instance.GetNamespace(), ID: instance.GetId(),
		AgentTemplate: instance.GetAgentTemplate().GetName(), Harness: instance.GetHarness().GetName(),
		State: instance.GetState().String(),
	}
}
