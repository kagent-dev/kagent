package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/temporal"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer creates an MCP server with the 4 Temporal workflow tools registered.
func NewServer(tc temporal.WorkflowClient) *mcpsdk.Server {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "temporal-workflows",
		Version: "v1.0.0",
	}, nil)

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "list_workflows",
		Description: "List Temporal workflow executions, optionally filtered by status or agent name.",
	}, handleListWorkflows(tc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "get_workflow",
		Description: "Get detailed information about a specific workflow execution including activity history.",
	}, handleGetWorkflow(tc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "cancel_workflow",
		Description: "Cancel a running workflow execution.",
	}, handleCancelWorkflow(tc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "signal_workflow",
		Description: "Send a signal to a running workflow execution.",
	}, handleSignalWorkflow(tc))

	return server
}

// textResult wraps a value as a JSON text content result.
func textResult(v interface{}) (*mcpsdk.CallToolResult, interface{}, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to marshal result: %v", err)), nil, nil
	}
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: string(data)},
		},
	}, nil, nil
}

// errorResult returns an MCP error result with isError=true.
func errorResult(msg string) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		IsError: true,
		Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: msg},
		},
	}
}

// --- Tool input types ---

type listWorkflowsInput struct {
	Status    string `json:"status,omitempty"`
	AgentName string `json:"agent_name,omitempty"`
	PageSize  int    `json:"page_size,omitempty"`
}

type getWorkflowInput struct {
	WorkflowID string `json:"workflow_id"`
}

type cancelWorkflowInput struct {
	WorkflowID string `json:"workflow_id"`
}

type signalWorkflowInput struct {
	WorkflowID string `json:"workflow_id"`
	SignalName string `json:"signal_name"`
	Data       string `json:"data,omitempty"`
}

// --- Tool handlers ---

func handleListWorkflows(tc temporal.WorkflowClient) func(context.Context, *mcpsdk.CallToolRequest, listWorkflowsInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input listWorkflowsInput) (*mcpsdk.CallToolResult, interface{}, error) {
		pageSize := input.PageSize
		if pageSize <= 0 {
			pageSize = 50
		}
		filter := temporal.WorkflowFilter{
			Status:    input.Status,
			AgentName: input.AgentName,
			PageSize:  pageSize,
		}
		workflows, err := tc.ListWorkflows(ctx, filter)
		if err != nil {
			return errorResult(fmt.Sprintf("list_workflows failed: %v", err)), nil, nil
		}
		return textResult(workflows)
	}
}

func handleGetWorkflow(tc temporal.WorkflowClient) func(context.Context, *mcpsdk.CallToolRequest, getWorkflowInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input getWorkflowInput) (*mcpsdk.CallToolResult, interface{}, error) {
		if input.WorkflowID == "" {
			return errorResult("workflow_id is required"), nil, nil
		}
		detail, err := tc.GetWorkflow(ctx, input.WorkflowID)
		if err != nil {
			return errorResult(fmt.Sprintf("get_workflow failed: %v", err)), nil, nil
		}
		return textResult(detail)
	}
}

func handleCancelWorkflow(tc temporal.WorkflowClient) func(context.Context, *mcpsdk.CallToolRequest, cancelWorkflowInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input cancelWorkflowInput) (*mcpsdk.CallToolResult, interface{}, error) {
		if input.WorkflowID == "" {
			return errorResult("workflow_id is required"), nil, nil
		}
		if err := tc.CancelWorkflow(ctx, input.WorkflowID); err != nil {
			return errorResult(fmt.Sprintf("cancel_workflow failed: %v", err)), nil, nil
		}
		return textResult(map[string]interface{}{"canceled": true, "workflow_id": input.WorkflowID})
	}
}

func handleSignalWorkflow(tc temporal.WorkflowClient) func(context.Context, *mcpsdk.CallToolRequest, signalWorkflowInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input signalWorkflowInput) (*mcpsdk.CallToolResult, interface{}, error) {
		if input.WorkflowID == "" {
			return errorResult("workflow_id is required"), nil, nil
		}
		if input.SignalName == "" {
			return errorResult("signal_name is required"), nil, nil
		}

		var data interface{}
		if input.Data != "" {
			if err := json.Unmarshal([]byte(input.Data), &data); err != nil {
				data = input.Data // treat as plain string if not valid JSON
			}
		}

		if err := tc.SignalWorkflow(ctx, input.WorkflowID, input.SignalName, data); err != nil {
			return errorResult(fmt.Sprintf("signal_workflow failed: %v", err)), nil, nil
		}
		return textResult(map[string]interface{}{"signaled": true, "workflow_id": input.WorkflowID, "signal_name": input.SignalName})
	}
}
