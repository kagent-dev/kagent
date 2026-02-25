package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kagent-dev/kagent/go/cmd/kanban-mcp/internal/db"
	"github.com/kagent-dev/kagent/go/cmd/kanban-mcp/internal/service"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Board is the response for get_board, grouping tasks by status column.
type Board struct {
	Columns []Column `json:"columns"`
}

// Column holds tasks for a single status in the workflow.
type Column struct {
	Status string     `json:"status"`
	Tasks  []*db.Task `json:"tasks"`
}

// NewServer creates and returns an MCP server with all 12 Kanban tools registered.
func NewServer(svc *service.TaskService) *mcpsdk.Server {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "kanban",
		Version: "v1.0.0",
	}, nil)

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "list_tasks",
		Description: "List tasks, optionally filtered by status, assignee, or label. Returns top-level tasks only by default.",
	}, handleListTasks(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "get_task",
		Description: "Get a task by ID including its subtasks and attachments.",
	}, handleGetTask(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "create_task",
		Description: "Create a new top-level task. Status defaults to Inbox if not specified.",
	}, handleCreateTask(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "create_subtask",
		Description: "Create a subtask under an existing top-level task (one level only).",
	}, handleCreateSubtask(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "assign_task",
		Description: "Assign a task to a person. Pass empty string to clear assignment.",
	}, handleAssignTask(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "move_task",
		Description: "Move a task to a new status column. Valid statuses: Inbox, Design, Develop, Testing, SecurityScan, CodeReview, Documentation, Done.",
	}, handleMoveTask(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "update_task",
		Description: "Update task fields (title, description, status, assignee, labels, user_input_needed).",
	}, handleUpdateTask(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "set_user_input_needed",
		Description: "Set or clear the user_input_needed flag on a task.",
	}, handleSetUserInputNeeded(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "delete_task",
		Description: "Delete a task and all its subtasks and attachments.",
	}, handleDeleteTask(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "get_board",
		Description: "Get the full Kanban board grouped by status columns in workflow order, with subtasks and attachments inline.",
	}, handleGetBoard(svc))

	// Attachment tools
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "add_attachment",
		Description: "Add a file or link attachment to a top-level task. For type=file: provide filename and content. For type=link: provide url and optional title.",
	}, handleAddAttachment(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "delete_attachment",
		Description: "Delete an attachment by ID.",
	}, handleDeleteAttachment(svc))

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

type listTasksInput struct {
	Status   string `json:"status,omitempty"`
	Assignee string `json:"assignee,omitempty"`
	Label    string `json:"label,omitempty"`
}

type getTaskInput struct {
	ID uint `json:"id"`
}

type createTaskInput struct {
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status,omitempty"`
	Labels      []string `json:"labels,omitempty"`
}

type createSubtaskInput struct {
	ParentID    uint     `json:"parent_id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status,omitempty"`
	Labels      []string `json:"labels,omitempty"`
}

type assignTaskInput struct {
	ID       uint   `json:"id"`
	Assignee string `json:"assignee"`
}

type moveTaskInput struct {
	ID     uint   `json:"id"`
	Status string `json:"status"`
}

type updateTaskInput struct {
	ID              uint      `json:"id"`
	Title           *string   `json:"title,omitempty"`
	Description     *string   `json:"description,omitempty"`
	Status          *string   `json:"status,omitempty"`
	Assignee        *string   `json:"assignee,omitempty"`
	Labels          *[]string `json:"labels,omitempty"`
	UserInputNeeded *bool     `json:"user_input_needed,omitempty"`
}

type setUserInputNeededInput struct {
	ID     uint `json:"id"`
	Needed bool `json:"needed"`
}

type deleteTaskInput struct {
	ID uint `json:"id"`
}

type addAttachmentInput struct {
	TaskID   uint   `json:"task_id"`
	Type     string `json:"type"`
	Filename string `json:"filename,omitempty"`
	Content  string `json:"content,omitempty"`
	URL      string `json:"url,omitempty"`
	Title    string `json:"title,omitempty"`
}

type deleteAttachmentInput struct {
	ID uint `json:"id"`
}

// --- Tool handlers ---

func handleListTasks(svc *service.TaskService) func(context.Context, *mcpsdk.CallToolRequest, listTasksInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input listTasksInput) (*mcpsdk.CallToolResult, interface{}, error) {
		filter := service.TaskFilter{}
		if input.Status != "" {
			s := db.TaskStatus(input.Status)
			filter.Status = &s
		}
		if input.Assignee != "" {
			filter.Assignee = &input.Assignee
		}
		if input.Label != "" {
			filter.Label = &input.Label
		}

		tasks, err := svc.ListTasks(ctx, filter)
		if err != nil {
			return errorResult(fmt.Sprintf("list_tasks failed: %v", err)), nil, nil
		}
		return textResult(tasks)
	}
}

func handleGetTask(svc *service.TaskService) func(context.Context, *mcpsdk.CallToolRequest, getTaskInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input getTaskInput) (*mcpsdk.CallToolResult, interface{}, error) {
		task, err := svc.GetTask(ctx, input.ID)
		if err != nil {
			return errorResult(fmt.Sprintf("get_task failed: %v", err)), nil, nil
		}
		return textResult(task)
	}
}

func handleCreateTask(svc *service.TaskService) func(context.Context, *mcpsdk.CallToolRequest, createTaskInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input createTaskInput) (*mcpsdk.CallToolResult, interface{}, error) {
		req := service.CreateTaskRequest{
			Title:       input.Title,
			Description: input.Description,
			Status:      db.TaskStatus(input.Status),
			Labels:      input.Labels,
		}
		task, err := svc.CreateTask(ctx, req)
		if err != nil {
			return errorResult(fmt.Sprintf("create_task failed: %v", err)), nil, nil
		}
		return textResult(task)
	}
}

func handleCreateSubtask(svc *service.TaskService) func(context.Context, *mcpsdk.CallToolRequest, createSubtaskInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input createSubtaskInput) (*mcpsdk.CallToolResult, interface{}, error) {
		req := service.CreateTaskRequest{
			Title:       input.Title,
			Description: input.Description,
			Status:      db.TaskStatus(input.Status),
			Labels:      input.Labels,
		}
		task, err := svc.CreateSubtask(ctx, input.ParentID, req)
		if err != nil {
			return errorResult(fmt.Sprintf("create_subtask failed: %v", err)), nil, nil
		}
		return textResult(task)
	}
}

func handleAssignTask(svc *service.TaskService) func(context.Context, *mcpsdk.CallToolRequest, assignTaskInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input assignTaskInput) (*mcpsdk.CallToolResult, interface{}, error) {
		task, err := svc.AssignTask(ctx, input.ID, input.Assignee)
		if err != nil {
			return errorResult(fmt.Sprintf("assign_task failed: %v", err)), nil, nil
		}
		return textResult(task)
	}
}

func handleMoveTask(svc *service.TaskService) func(context.Context, *mcpsdk.CallToolRequest, moveTaskInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input moveTaskInput) (*mcpsdk.CallToolResult, interface{}, error) {
		task, err := svc.MoveTask(ctx, input.ID, db.TaskStatus(input.Status))
		if err != nil {
			return errorResult(fmt.Sprintf("move_task failed: %v", err)), nil, nil
		}
		return textResult(task)
	}
}

func handleUpdateTask(svc *service.TaskService) func(context.Context, *mcpsdk.CallToolRequest, updateTaskInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input updateTaskInput) (*mcpsdk.CallToolResult, interface{}, error) {
		req := service.UpdateTaskRequest{
			Title:           input.Title,
			Description:     input.Description,
			Assignee:        input.Assignee,
			Labels:          input.Labels,
			UserInputNeeded: input.UserInputNeeded,
		}
		if input.Status != nil {
			s := db.TaskStatus(*input.Status)
			req.Status = &s
		}
		task, err := svc.UpdateTask(ctx, input.ID, req)
		if err != nil {
			return errorResult(fmt.Sprintf("update_task failed: %v", err)), nil, nil
		}
		return textResult(task)
	}
}

func handleSetUserInputNeeded(svc *service.TaskService) func(context.Context, *mcpsdk.CallToolRequest, setUserInputNeededInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input setUserInputNeededInput) (*mcpsdk.CallToolResult, interface{}, error) {
		req := service.UpdateTaskRequest{
			UserInputNeeded: &input.Needed,
		}
		task, err := svc.UpdateTask(ctx, input.ID, req)
		if err != nil {
			return errorResult(fmt.Sprintf("set_user_input_needed failed: %v", err)), nil, nil
		}
		return textResult(task)
	}
}

func handleDeleteTask(svc *service.TaskService) func(context.Context, *mcpsdk.CallToolRequest, deleteTaskInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input deleteTaskInput) (*mcpsdk.CallToolResult, interface{}, error) {
		if err := svc.DeleteTask(ctx, input.ID); err != nil {
			return errorResult(fmt.Sprintf("delete_task failed: %v", err)), nil, nil
		}
		return textResult(map[string]interface{}{"deleted": true, "id": input.ID})
	}
}

func handleGetBoard(svc *service.TaskService) func(context.Context, *mcpsdk.CallToolRequest, interface{}) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ interface{}) (*mcpsdk.CallToolResult, interface{}, error) {
		board, err := buildBoard(ctx, svc)
		if err != nil {
			return errorResult(fmt.Sprintf("get_board failed: %v", err)), nil, nil
		}
		return textResult(board)
	}
}

func handleAddAttachment(svc *service.TaskService) func(context.Context, *mcpsdk.CallToolRequest, addAttachmentInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input addAttachmentInput) (*mcpsdk.CallToolResult, interface{}, error) {
		req := service.CreateAttachmentRequest{
			Type:     db.AttachmentType(input.Type),
			Filename: input.Filename,
			Content:  input.Content,
			URL:      input.URL,
			Title:    input.Title,
		}
		attachment, err := svc.AddAttachment(ctx, input.TaskID, req)
		if err != nil {
			return errorResult(fmt.Sprintf("add_attachment failed: %v", err)), nil, nil
		}
		return textResult(attachment)
	}
}

func handleDeleteAttachment(svc *service.TaskService) func(context.Context, *mcpsdk.CallToolRequest, deleteAttachmentInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input deleteAttachmentInput) (*mcpsdk.CallToolResult, interface{}, error) {
		if err := svc.DeleteAttachment(ctx, input.ID); err != nil {
			return errorResult(fmt.Sprintf("delete_attachment failed: %v", err)), nil, nil
		}
		return textResult(map[string]interface{}{"deleted": true, "id": input.ID})
	}
}

// buildBoard fetches all top-level tasks and groups them by status column.
func buildBoard(ctx context.Context, svc *service.TaskService) (*Board, error) {
	tasks, err := svc.ListTasks(ctx, service.TaskFilter{})
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}

	// Index tasks by status
	byStatus := make(map[db.TaskStatus][]*db.Task)
	for _, t := range tasks {
		byStatus[t.Status] = append(byStatus[t.Status], t)
	}

	columns := make([]Column, 0, len(db.StatusWorkflow))
	for _, status := range db.StatusWorkflow {
		col := Column{
			Status: string(status),
			Tasks:  byStatus[status],
		}
		if col.Tasks == nil {
			col.Tasks = []*db.Task{}
		}
		columns = append(columns, col)
	}

	return &Board{Columns: columns}, nil
}
