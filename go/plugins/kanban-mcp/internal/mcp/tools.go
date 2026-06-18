// Package mcp wires the kanban TaskService to the Model Context Protocol. It
// registers the kanban tools (10 task + 2 board + 2 attachment + 2 attribute) on an mcp.Server
// using typed input/output structs, so kagent can drive the board over either the
// stdio or the streamable-HTTP transport.
//
// Each handler parses its typed input, calls the corresponding TaskService
// method, and returns the typed output. When the service returns an error the
// handler returns that error too: the go-sdk maps a non-nil handler error onto a
// CallToolResult with IsError=true and the error string in the text content, so
// the model can see the failure and self-correct.
package mcp

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/service"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/ui"
)

// MCP App (SEP-1865) constants for the task-progress widget. The tools link to
// the ui:// resource via _meta.ui.resourceUri; a host that supports MCP Apps
// renders the resource HTML as an interactive view and feeds it the tool result.
const (
	// taskProgressResourceURI is the ui:// resource the progress tools render
	// into. The scheme must be ui:// for the host to treat it as an MCP App.
	taskProgressResourceURI = "ui://kanban/task-progress"
	// mcpAppHTMLMimeType marks a resource as MCP App HTML. The profile= parameter
	// is required; a plain text/html resource will not render as an app.
	mcpAppHTMLMimeType = "text/html;profile=mcp-app"
)

// objectOutputSchema is a permissive output schema used for tools whose result
// contains the self-referential service.Task type (a Feature carries its child
// Tasks). The go-sdk's automatic schema inference panics with "cycle detected"
// on such recursive Go types, so we supply an explicit object schema to skip
// inference. The structured result is still returned to the caller; only the
// advertised output schema is relaxed to a generic object.
func objectOutputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{Type: "object"}
}

// ---------------------------------------------------------------------------
// Tool input/output types
// ---------------------------------------------------------------------------

// ListTasksInput filters the task list. All fields are optional; an empty
// request returns all cards (Features and Tasks) of the default board. Setting
// parent_id returns the child Tasks of that Feature instead (board is implied by
// the parent).
type ListTasksInput struct {
	Board    string `json:"board,omitempty" jsonschema:"Board key to list tasks from; defaults to 'default'"`
	Status   string `json:"status,omitempty" jsonschema:"Filter by column/status (must be a column of the board)"`
	Assignee string `json:"assignee,omitempty" jsonschema:"Filter by assignee"`
	Label    string `json:"label,omitempty" jsonschema:"Return only tasks containing this label"`
	ParentID *int64 `json:"parent_id,omitempty" jsonschema:"If set, list the child Tasks of this Feature instead of all board cards"`
}

// TasksOutput is the result of a list operation.
type TasksOutput struct {
	Tasks []*service.Task `json:"tasks"`
}

// TaskOutput wraps a single task result.
type TaskOutput struct {
	Task *service.Task `json:"task"`
}

// SubtaskOutput wraps a single checklist subtask result.
type SubtaskOutput struct {
	Subtask *service.Subtask `json:"subtask"`
}

// GetTaskInput identifies a task to fetch.
type GetTaskInput struct {
	ID int64 `json:"id" jsonschema:"Task ID"`
}

// CreateTaskInput is the payload for create_task. With parent_id set, the new
// task is created as a child Task under that Feature; otherwise it is a top-level
// card whose kind ("feature" or "task") is selected by Kind.
type CreateTaskInput struct {
	Board       string   `json:"board,omitempty" jsonschema:"Board key to create a top-level card on; defaults to 'default'. Ignored when parent_id is set (the board is inherited from the Feature)"`
	ParentID    *int64   `json:"parent_id,omitempty" jsonschema:"If set, create a child Task under this Feature; if omitted, create a top-level card (see kind)"`
	Kind        string   `json:"kind,omitempty" jsonschema:"For a top-level card: 'feature' (default) or 'task' for a standalone Task. Ignored when parent_id is set"`
	Title       string   `json:"title" jsonschema:"Task title"`
	Description string   `json:"description,omitempty" jsonschema:"Task description"`
	Status      string   `json:"status,omitempty" jsonschema:"Initial column/status; defaults to the board's first column. Must be one of the board's columns"`
	Labels      []string `json:"labels,omitempty" jsonschema:"Labels to attach to the task"`
}

// CreateSubtaskInput is the payload for create_subtask (a checklist item).
type CreateSubtaskInput struct {
	TaskID int64  `json:"task_id" jsonschema:"ID of the Task (a child task, not a Feature) to add the checklist item to"`
	Title  string `json:"title" jsonschema:"Checklist item title"`
}

// ToggleSubtaskInput sets or clears a checklist item's done flag.
type ToggleSubtaskInput struct {
	ID   int64 `json:"id" jsonschema:"Subtask (checklist item) ID"`
	Done bool  `json:"done" jsonschema:"Whether the checklist item is done"`
}

// UpdateSubtaskInput renames a checklist item.
type UpdateSubtaskInput struct {
	ID    int64  `json:"id" jsonschema:"Subtask (checklist item) ID"`
	Title string `json:"title" jsonschema:"New checklist item title"`
}

// DeleteSubtaskInput identifies a checklist item to delete.
type DeleteSubtaskInput struct {
	ID int64 `json:"id" jsonschema:"Subtask (checklist item) ID"`
}

// AssignTaskInput is the payload for assign_task. An empty assignee clears the
// current assignment.
type AssignTaskInput struct {
	ID       int64  `json:"id" jsonschema:"Task ID"`
	Assignee string `json:"assignee" jsonschema:"Assignee name; empty string clears the assignment"`
}

// MoveTaskInput is the payload for move_task.
type MoveTaskInput struct {
	ID     int64  `json:"id" jsonschema:"Task ID"`
	Status string `json:"status" jsonschema:"Target column/status; must be one of the columns of the task's board"`
}

// UpdateTaskInput carries partial updates for update_task. Nil pointer fields are
// left unchanged.
type UpdateTaskInput struct {
	ID              int64     `json:"id" jsonschema:"Task ID"`
	Title           *string   `json:"title,omitempty" jsonschema:"New title"`
	Description     *string   `json:"description,omitempty" jsonschema:"New description"`
	Status          *string   `json:"status,omitempty" jsonschema:"New workflow status"`
	Assignee        *string   `json:"assignee,omitempty" jsonschema:"New assignee; empty string clears it"`
	Labels          *[]string `json:"labels,omitempty" jsonschema:"Replacement label set"`
	UserInputNeeded *bool     `json:"user_input_needed,omitempty" jsonschema:"Whether the task is blocked waiting on human input"`
}

// SetUserInputNeededInput toggles the human-in-the-loop flag.
type SetUserInputNeededInput struct {
	ID              int64 `json:"id" jsonschema:"Task ID"`
	UserInputNeeded bool  `json:"user_input_needed" jsonschema:"Whether the task is blocked waiting on human input"`
}

// DeleteTaskInput identifies a task to delete. Subtasks and attachments cascade.
type DeleteTaskInput struct {
	ID int64 `json:"id" jsonschema:"Task ID; subtasks and attachments are deleted with it"`
}

// SuccessOutput is the result of an operation that has no return value.
type SuccessOutput struct {
	Success bool `json:"success"`
}

// GetBoardInput selects which board to return.
type GetBoardInput struct {
	Board string `json:"board,omitempty" jsonschema:"Board key to fetch; defaults to 'default'"`
}

// BoardOutput wraps the full state of a single board.
type BoardOutput struct {
	Board *service.BoardState `json:"board"`
}

// ListBoardsInput has no fields.
type ListBoardsInput struct{}

// BoardsOutput is the result of list_boards.
type BoardsOutput struct {
	Boards []*service.Board `json:"boards"`
}

// CreateBoardInput is the payload for create_board.
type CreateBoardInput struct {
	Key         string   `json:"key" jsonschema:"Unique board key (slug) used to address the board"`
	Name        string   `json:"name,omitempty" jsonschema:"Display name; defaults to the key"`
	Description string   `json:"description,omitempty" jsonschema:"Board description"`
	Columns     []string `json:"columns" jsonschema:"Ordered list of column names; tasks on this board can only use these columns"`
	Subtasks    []string `json:"subtasks,omitempty" jsonschema:"Optional checklist template; these subtask titles are auto-added to every new Task created on this board"`
	Scope       string   `json:"scope,omitempty" jsonschema:"Board scope: 'general' (shared) or 'agent' (bound to an agent); defaults to 'general'"`
	Owner       string   `json:"owner,omitempty" jsonschema:"Owning agent name when scope is 'agent'"`
}

// BoardMetaOutput wraps a single board's metadata.
type BoardMetaOutput struct {
	Board *service.Board `json:"board"`
}

// AddAttachmentInput is the payload for add_attachment. The required fields
// depend on type: "file" needs filename and base64 content; "link" needs url.
type AddAttachmentInput struct {
	TaskID   int64  `json:"task_id" jsonschema:"ID of the task card (Feature or Task) to attach to"`
	Type     string `json:"type" jsonschema:"Attachment type: file or link"`
	Filename string `json:"filename,omitempty" jsonschema:"File name with extension (required for type=file); allowed: md, markdown, html, htm, txt, yaml, yml, csv, pdf, docx, xlsx"`
	Content  string `json:"content,omitempty" jsonschema:"Base64-encoded file bytes (required for type=file)"`
	URL      string `json:"url,omitempty" jsonschema:"Link URL (required for type=link)"`
	Title    string `json:"title,omitempty" jsonschema:"Link title (optional for type=link)"`
}

// AttachmentOutput wraps a single attachment result.
type AttachmentOutput struct {
	Attachment *service.Attachment `json:"attachment"`
}

// DeleteAttachmentInput identifies an attachment to delete.
type DeleteAttachmentInput struct {
	ID int64 `json:"id" jsonschema:"Attachment ID"`
}

// SetAttributeInput is the payload for set_attribute (upsert a key/value pair).
type SetAttributeInput struct {
	TaskID int64  `json:"task_id" jsonschema:"ID of the card (Feature or Task) to set the attribute on"`
	Key    string `json:"key" jsonschema:"Attribute key"`
	Value  string `json:"value" jsonschema:"Attribute value"`
}

// AttributeOutput wraps a single attribute result.
type AttributeOutput struct {
	Attribute *service.Attribute `json:"attribute"`
}

// DeleteAttributeInput identifies an attribute (by card + key) to delete.
type DeleteAttributeInput struct {
	TaskID int64  `json:"task_id" jsonschema:"ID of the card holding the attribute"`
	Key    string `json:"key" jsonschema:"Attribute key to remove"`
}

// TaskProgressInput identifies the card whose progress widget to render. Shared
// by show_task_progress (model + app) and refresh_task_progress (app-only).
type TaskProgressInput struct {
	ID int64 `json:"id" jsonschema:"ID of the card (Feature or Task) to render a progress widget for"`
}

// TaskProgressOutput wraps the computed progress. It is returned as the tool's
// structuredContent; the MCP App View renders it.
type TaskProgressOutput struct {
	Progress *service.TaskProgress `json:"progress"`
}

// ---------------------------------------------------------------------------
// Server construction
// ---------------------------------------------------------------------------

// NewServer builds an mcp.Server with all kanban tools registered against the
// given TaskService (13 task/subtask + 2 board + 2 attachment + 2 attribute). The returned
// server can be run over any transport (stdio in main.go, streamable HTTP in the
// HTTP server).
func NewServer(svc *service.TaskService) *mcpsdk.Server {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "kanban",
		Version: "v1.0.0",
	}, nil)

	// Task tools. Tools whose result embeds the recursive service.Task type
	// supply an explicit OutputSchema to avoid the go-sdk schema-inference cycle
	// panic (see objectOutputSchema).
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:         "list_tasks",
		Description:  "List cards (Features and Tasks), optionally filtered by status, assignee, or label. Set parent_id to list a Feature's child Tasks instead.",
		OutputSchema: objectOutputSchema(),
	}, handleListTasks(svc))
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:         "get_task",
		Description:  "Get a single card by ID, including its checklist subtasks, attachments, and (for a Feature) its child Tasks.",
		OutputSchema: objectOutputSchema(),
	}, handleGetTask(svc))
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:         "create_task",
		Description:  "Create a Feature (top-level card) or, with parent_id set, a child Task under a Feature. Status defaults to the board's first column. If the board defines a default subtask template, every new Task (standalone or child) is created with those checklist subtasks pre-populated; Features get none.",
		OutputSchema: objectOutputSchema(),
	}, handleCreateTask(svc))
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:         "create_subtask",
		Description:  "Add a checklist subtask (title + done flag) to a Task. Checklist subtasks can only be added to Tasks, not Features.",
		OutputSchema: objectOutputSchema(),
	}, handleCreateSubtask(svc))
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:         "toggle_subtask",
		Description:  "Set or clear the done flag on a checklist subtask.",
		OutputSchema: objectOutputSchema(),
	}, handleToggleSubtask(svc))
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:         "update_subtask",
		Description:  "Rename a checklist subtask.",
		OutputSchema: objectOutputSchema(),
	}, handleUpdateSubtask(svc))
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "delete_subtask",
		Description: "Delete a checklist subtask by ID.",
	}, handleDeleteSubtask(svc))
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:         "assign_task",
		Description:  "Assign a task to someone. An empty assignee clears the assignment.",
		OutputSchema: objectOutputSchema(),
	}, handleAssignTask(svc))
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:         "move_task",
		Description:  "Move a task to a different workflow status.",
		OutputSchema: objectOutputSchema(),
	}, handleMoveTask(svc))
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:         "update_task",
		Description:  "Update one or more fields of a task. Unset fields are left unchanged.",
		OutputSchema: objectOutputSchema(),
	}, handleUpdateTask(svc))
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:         "set_user_input_needed",
		Description:  "Set or clear the human-in-the-loop flag on a task.",
		OutputSchema: objectOutputSchema(),
	}, handleSetUserInputNeeded(svc))
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "delete_task",
		Description: "Delete a card. Its checklist subtasks and attachments are deleted with it; deleting a Feature also deletes its child Tasks.",
	}, handleDeleteTask(svc))
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:         "get_board",
		Description:  "Get the full state of a board (default 'default'): its columns and every card (Feature or Task) grouped by column, with checklist subtasks and attachments.",
		OutputSchema: objectOutputSchema(),
	}, handleGetBoard(svc))

	// Board tools (2).
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:         "list_boards",
		Description:  "List all boards with their key, name, scope/owner, column sets, and default subtask template (if any).",
		OutputSchema: objectOutputSchema(),
	}, handleListBoards(svc))
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:         "create_board",
		Description:  "Create a new board with its own ordered set of columns. Tasks on the board can only use these columns. Optionally provide subtasks: a default checklist template auto-added to every new Task created on the board.",
		OutputSchema: objectOutputSchema(),
	}, handleCreateBoard(svc))

	// Attachment tools (2). File content must be base64-encoded; allowed file
	// extensions: md, markdown, html, htm, txt, yaml, yml, csv, pdf, docx, xlsx.
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "add_attachment",
		Description: "Add a file or link attachment to a card (Feature or Task). For type=file, content must be base64-encoded and the filename extension must be one of: md, markdown, html, htm, txt, yaml, yml, csv, pdf, docx, xlsx.",
	}, handleAddAttachment(svc))
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "delete_attachment",
		Description: "Delete a file or link attachment by ID.",
	}, handleDeleteAttachment(svc))

	// Attribute tools (2): simple key/value pairs on a card, upsert by key.
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "set_attribute",
		Description: "Set (upsert) a key/value attribute on a card (Feature or Task). Setting an existing key replaces its value.",
	}, handleSetAttribute(svc))
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "delete_attribute",
		Description: "Delete a key/value attribute from a card by its key.",
	}, handleDeleteAttribute(svc))

	// MCP App (SEP-1865): the task-progress widget. The ui:// resource carries
	// the single-file HTML View; show_task_progress links to it so MCP-App hosts
	// render an interactive progress card, and refresh_task_progress is the
	// app-only channel the View uses to re-fetch data in place.
	registerTaskProgressApp(server, svc)

	return server
}

// registerTaskProgressApp registers the task-progress MCP App: the ui:// HTML
// resource plus the model+app show tool and the app-only refresh tool. Both
// tools share one handler and one ui:// resourceUri; only their visibility
// differs.
func registerTaskProgressApp(server *mcpsdk.Server, svc *service.TaskService) {
	server.AddResource(&mcpsdk.Resource{
		Name:        "task-progress",
		Title:       "Task progress",
		URI:         taskProgressResourceURI,
		MIMEType:    mcpAppHTMLMimeType,
		Description: "Interactive progress widget for a single kanban card (MCP App).",
	}, func(_ context.Context, _ *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		return &mcpsdk.ReadResourceResult{
			Contents: []*mcpsdk.ResourceContents{{
				URI:      taskProgressResourceURI,
				MIMEType: mcpAppHTMLMimeType,
				Text:     string(ui.TaskProgressHTML()),
			}},
		}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:         "show_task_progress",
		Description:  "Render an interactive progress widget for a card (Feature or Task) inline in the chat. Shows completion percent, per-column child-task counts (Feature) or checklist progress (Task), and refreshes live. Use this when the user asks to see or track a specific task's progress.",
		OutputSchema: objectOutputSchema(),
		Meta:         mcpsdk.Meta{"ui": map[string]any{"resourceUri": taskProgressResourceURI}},
	}, handleTaskProgress(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:         "refresh_task_progress",
		Description:  "Internal MCP App control: re-fetch the progress data for the rendered task-progress widget. Hidden from the model (app-only).",
		OutputSchema: objectOutputSchema(),
		Meta: mcpsdk.Meta{"ui": map[string]any{
			"resourceUri": taskProgressResourceURI,
			"visibility":  []any{"app"},
		}},
	}, handleTaskProgress(svc))
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleListTasks(svc *service.TaskService) mcpsdk.ToolHandlerFor[ListTasksInput, TasksOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in ListTasksInput) (*mcpsdk.CallToolResult, TasksOutput, error) {
		filter := service.TaskFilter{ParentID: in.ParentID}
		if in.Status != "" {
			st := db.TaskStatus(in.Status)
			filter.Status = &st
		}
		if in.Assignee != "" {
			filter.Assignee = &in.Assignee
		}
		if in.Label != "" {
			filter.Label = &in.Label
		}

		tasks, err := svc.ListTasks(ctx, in.Board, filter)
		if err != nil {
			return nil, TasksOutput{}, fmt.Errorf("listing tasks: %w", err)
		}
		return nil, TasksOutput{Tasks: tasks}, nil
	}
}

func handleGetTask(svc *service.TaskService) mcpsdk.ToolHandlerFor[GetTaskInput, TaskOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in GetTaskInput) (*mcpsdk.CallToolResult, TaskOutput, error) {
		task, err := svc.GetTask(ctx, in.ID)
		if err != nil {
			return nil, TaskOutput{}, fmt.Errorf("getting task: %w", err)
		}
		return nil, TaskOutput{Task: task}, nil
	}
}

func handleCreateTask(svc *service.TaskService) mcpsdk.ToolHandlerFor[CreateTaskInput, TaskOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in CreateTaskInput) (*mcpsdk.CallToolResult, TaskOutput, error) {
		task, err := svc.CreateTask(ctx, in.Board, service.CreateTaskRequest{
			Title:       in.Title,
			Description: in.Description,
			Status:      db.TaskStatus(in.Status),
			Labels:      in.Labels,
			ParentID:    in.ParentID,
			Kind:        in.Kind,
		})
		if err != nil {
			return nil, TaskOutput{}, fmt.Errorf("creating task: %w", err)
		}
		return nil, TaskOutput{Task: task}, nil
	}
}

func handleCreateSubtask(svc *service.TaskService) mcpsdk.ToolHandlerFor[CreateSubtaskInput, SubtaskOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in CreateSubtaskInput) (*mcpsdk.CallToolResult, SubtaskOutput, error) {
		sub, err := svc.CreateSubtask(ctx, in.TaskID, in.Title)
		if err != nil {
			return nil, SubtaskOutput{}, fmt.Errorf("creating subtask: %w", err)
		}
		return nil, SubtaskOutput{Subtask: sub}, nil
	}
}

func handleToggleSubtask(svc *service.TaskService) mcpsdk.ToolHandlerFor[ToggleSubtaskInput, SubtaskOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in ToggleSubtaskInput) (*mcpsdk.CallToolResult, SubtaskOutput, error) {
		sub, err := svc.ToggleSubtask(ctx, in.ID, in.Done)
		if err != nil {
			return nil, SubtaskOutput{}, fmt.Errorf("toggling subtask: %w", err)
		}
		return nil, SubtaskOutput{Subtask: sub}, nil
	}
}

func handleUpdateSubtask(svc *service.TaskService) mcpsdk.ToolHandlerFor[UpdateSubtaskInput, SubtaskOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in UpdateSubtaskInput) (*mcpsdk.CallToolResult, SubtaskOutput, error) {
		sub, err := svc.UpdateSubtask(ctx, in.ID, in.Title)
		if err != nil {
			return nil, SubtaskOutput{}, fmt.Errorf("updating subtask: %w", err)
		}
		return nil, SubtaskOutput{Subtask: sub}, nil
	}
}

func handleDeleteSubtask(svc *service.TaskService) mcpsdk.ToolHandlerFor[DeleteSubtaskInput, SuccessOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in DeleteSubtaskInput) (*mcpsdk.CallToolResult, SuccessOutput, error) {
		if err := svc.DeleteSubtask(ctx, in.ID); err != nil {
			return nil, SuccessOutput{}, fmt.Errorf("deleting subtask: %w", err)
		}
		return nil, SuccessOutput{Success: true}, nil
	}
}

func handleAssignTask(svc *service.TaskService) mcpsdk.ToolHandlerFor[AssignTaskInput, TaskOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in AssignTaskInput) (*mcpsdk.CallToolResult, TaskOutput, error) {
		task, err := svc.AssignTask(ctx, in.ID, in.Assignee)
		if err != nil {
			return nil, TaskOutput{}, fmt.Errorf("assigning task: %w", err)
		}
		return nil, TaskOutput{Task: task}, nil
	}
}

func handleMoveTask(svc *service.TaskService) mcpsdk.ToolHandlerFor[MoveTaskInput, TaskOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in MoveTaskInput) (*mcpsdk.CallToolResult, TaskOutput, error) {
		task, err := svc.MoveTask(ctx, in.ID, db.TaskStatus(in.Status))
		if err != nil {
			return nil, TaskOutput{}, fmt.Errorf("moving task: %w", err)
		}
		return nil, TaskOutput{Task: task}, nil
	}
}

func handleUpdateTask(svc *service.TaskService) mcpsdk.ToolHandlerFor[UpdateTaskInput, TaskOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in UpdateTaskInput) (*mcpsdk.CallToolResult, TaskOutput, error) {
		req := service.UpdateTaskRequest{
			Title:           in.Title,
			Description:     in.Description,
			Assignee:        in.Assignee,
			Labels:          in.Labels,
			UserInputNeeded: in.UserInputNeeded,
		}
		if in.Status != nil {
			st := db.TaskStatus(*in.Status)
			req.Status = &st
		}

		task, err := svc.UpdateTask(ctx, in.ID, req)
		if err != nil {
			return nil, TaskOutput{}, fmt.Errorf("updating task: %w", err)
		}
		return nil, TaskOutput{Task: task}, nil
	}
}

func handleSetUserInputNeeded(svc *service.TaskService) mcpsdk.ToolHandlerFor[SetUserInputNeededInput, TaskOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in SetUserInputNeededInput) (*mcpsdk.CallToolResult, TaskOutput, error) {
		uin := in.UserInputNeeded
		task, err := svc.UpdateTask(ctx, in.ID, service.UpdateTaskRequest{UserInputNeeded: &uin})
		if err != nil {
			return nil, TaskOutput{}, fmt.Errorf("setting user_input_needed: %w", err)
		}
		return nil, TaskOutput{Task: task}, nil
	}
}

func handleDeleteTask(svc *service.TaskService) mcpsdk.ToolHandlerFor[DeleteTaskInput, SuccessOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in DeleteTaskInput) (*mcpsdk.CallToolResult, SuccessOutput, error) {
		if err := svc.DeleteTask(ctx, in.ID); err != nil {
			return nil, SuccessOutput{}, fmt.Errorf("deleting task: %w", err)
		}
		return nil, SuccessOutput{Success: true}, nil
	}
}

func handleGetBoard(svc *service.TaskService) mcpsdk.ToolHandlerFor[GetBoardInput, BoardOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in GetBoardInput) (*mcpsdk.CallToolResult, BoardOutput, error) {
		board, err := svc.GetBoard(ctx, in.Board)
		if err != nil {
			return nil, BoardOutput{}, fmt.Errorf("getting board: %w", err)
		}
		return nil, BoardOutput{Board: board}, nil
	}
}

func handleListBoards(svc *service.TaskService) mcpsdk.ToolHandlerFor[ListBoardsInput, BoardsOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ ListBoardsInput) (*mcpsdk.CallToolResult, BoardsOutput, error) {
		boards, err := svc.ListBoards(ctx)
		if err != nil {
			return nil, BoardsOutput{}, fmt.Errorf("listing boards: %w", err)
		}
		return nil, BoardsOutput{Boards: boards}, nil
	}
}

func handleCreateBoard(svc *service.TaskService) mcpsdk.ToolHandlerFor[CreateBoardInput, BoardMetaOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in CreateBoardInput) (*mcpsdk.CallToolResult, BoardMetaOutput, error) {
		board, err := svc.CreateBoard(ctx, service.CreateBoardRequest{
			Key:         in.Key,
			Name:        in.Name,
			Description: in.Description,
			Scope:       in.Scope,
			Owner:       in.Owner,
			Columns:     in.Columns,
			Subtasks:    in.Subtasks,
		})
		if err != nil {
			return nil, BoardMetaOutput{}, fmt.Errorf("creating board: %w", err)
		}
		return nil, BoardMetaOutput{Board: board}, nil
	}
}

func handleAddAttachment(svc *service.TaskService) mcpsdk.ToolHandlerFor[AddAttachmentInput, AttachmentOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in AddAttachmentInput) (*mcpsdk.CallToolResult, AttachmentOutput, error) {
		att, err := svc.AddAttachment(ctx, in.TaskID, service.CreateAttachmentRequest{
			Type:     db.AttachmentType(in.Type),
			Filename: in.Filename,
			Content:  in.Content,
			URL:      in.URL,
			Title:    in.Title,
		})
		if err != nil {
			return nil, AttachmentOutput{}, fmt.Errorf("adding attachment: %w", err)
		}
		return nil, AttachmentOutput{Attachment: att}, nil
	}
}

func handleDeleteAttachment(svc *service.TaskService) mcpsdk.ToolHandlerFor[DeleteAttachmentInput, SuccessOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in DeleteAttachmentInput) (*mcpsdk.CallToolResult, SuccessOutput, error) {
		if err := svc.DeleteAttachment(ctx, in.ID); err != nil {
			return nil, SuccessOutput{}, fmt.Errorf("deleting attachment: %w", err)
		}
		return nil, SuccessOutput{Success: true}, nil
	}
}

func handleSetAttribute(svc *service.TaskService) mcpsdk.ToolHandlerFor[SetAttributeInput, AttributeOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in SetAttributeInput) (*mcpsdk.CallToolResult, AttributeOutput, error) {
		attr, err := svc.SetAttribute(ctx, in.TaskID, in.Key, in.Value)
		if err != nil {
			return nil, AttributeOutput{}, fmt.Errorf("setting attribute: %w", err)
		}
		return nil, AttributeOutput{Attribute: attr}, nil
	}
}

func handleDeleteAttribute(svc *service.TaskService) mcpsdk.ToolHandlerFor[DeleteAttributeInput, SuccessOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in DeleteAttributeInput) (*mcpsdk.CallToolResult, SuccessOutput, error) {
		if err := svc.DeleteAttribute(ctx, in.TaskID, in.Key); err != nil {
			return nil, SuccessOutput{}, fmt.Errorf("deleting attribute: %w", err)
		}
		return nil, SuccessOutput{Success: true}, nil
	}
}

// handleTaskProgress backs both show_task_progress and refresh_task_progress. It
// returns the computed progress as structuredContent (for the MCP App View) plus
// the human summary as the text content block, which is the required fallback
// for non-UI hosts and the text the model sees.
func handleTaskProgress(svc *service.TaskService) mcpsdk.ToolHandlerFor[TaskProgressInput, TaskProgressOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in TaskProgressInput) (*mcpsdk.CallToolResult, TaskProgressOutput, error) {
		progress, err := svc.TaskProgress(ctx, in.ID)
		if err != nil {
			return nil, TaskProgressOutput{}, fmt.Errorf("getting task progress: %w", err)
		}
		result := &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: progress.Summary}},
		}
		return result, TaskProgressOutput{Progress: progress}, nil
	}
}
