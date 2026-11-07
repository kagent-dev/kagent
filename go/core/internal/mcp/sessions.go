package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"buf.build/go/protovalidate"
	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/errgroup"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

const (
	createSessionToolName = "create_session"
	getSessionToolName    = "get_session"
	listTasksToolName     = "list_tasks"
	getTaskToolName       = "get_task"
	deleteSessionToolName = "delete_session"
	renameSessionToolName = "rename_session"

	// maxFirstMessageBytes bounds create_session's first message.
	maxFirstMessageBytes = 128 << 10
	// maxSummaryTurns bounds how many turns one summary or transcript reads.
	maxSummaryTurns = 200
	// fetchBatch turns are read whole at a time, newest first.
	fetchBatch = 8
	// defaultTaskPageSize is A2A tasks/list's default page size.
	defaultTaskPageSize = 50
	// minTaskShare is the least of max_bytes one listed task's summary may
	// take: room for its header and clipped texts.
	minTaskShare = 256
	// envelopeSlack is what an answer's fields set after its budget was taken
	// may add.
	envelopeSlack = 128
)

type CreateSessionInput struct {
	Agent     agentReference `json:"agent" jsonschema:"The Agent the Session talks to"`
	RequestID string         `json:"request_id,omitempty" jsonschema:"Stable idempotency key; reuse it with identical inputs on a retry to get the same Session and first turn"`
	Name      string         `json:"name,omitempty" jsonschema:"Optional display name"`
	Message   string         `json:"message,omitempty" jsonschema:"Optional first message, sent as invoke_agent sends one"`
}

type CreateSessionOutput struct {
	Session SessionSummary `json:"session"`
	Turn    *Turn          `json:"turn,omitempty"`
}

type SessionIDInput struct {
	SessionID string `json:"session_id" jsonschema:"Session UUID"`
}

type SessionOutput struct {
	Session SessionSummary `json:"session"`
}

type RenameSessionInput struct {
	SessionID string `json:"session_id" jsonschema:"Session UUID"`
	Name      string `json:"name" jsonschema:"New display name; empty clears it"`
}

type GetSessionInput struct {
	SessionID string `json:"session_id" jsonschema:"Session UUID"`
	LastTurns int    `json:"last_turns,omitempty" jsonschema:"Read only the last n turns; poll with 1 until the last turn is final"`
	MaxBytes  int    `json:"max_bytes,omitempty" jsonschema:"Budget in bytes for the JSON answer; the oldest turns are dropped to fit"`
}

type GetSessionOutput struct {
	Session      SessionSummary `json:"session"`
	TotalTurns   int            `json:"total_turns"`
	OmittedTurns int            `json:"omitted_turns,omitempty"`
	Truncated    bool           `json:"truncated,omitempty"`
	Hint         string         `json:"hint,omitempty"`
	Turns        []Turn         `json:"turns"`
}

type ListTasksInput struct {
	SessionID string `json:"session_id" jsonschema:"Session UUID"`
	PageSize  int    `json:"page_size,omitempty" jsonschema:"Maximum number of tasks to return, 1 to 100; default 50"`
	PageToken string `json:"page_token,omitempty" jsonschema:"Token returned by a previous call"`
	MaxBytes  int    `json:"max_bytes,omitempty" jsonschema:"Budget in bytes for the JSON answer; a task's summary that exceeds its share is clipped"`
}

type ListTasksOutput struct {
	SessionID     string `json:"session_id"`
	TotalSize     int    `json:"total_size"`
	NextPageToken string `json:"next_page_token,omitempty"`
	Truncated     bool   `json:"truncated,omitempty"`
	Hint          string `json:"hint,omitempty"`
	Tasks         []Turn `json:"tasks"`
}

type GetTaskInput struct {
	SessionID string    `json:"session_id" jsonschema:"Session UUID"`
	TaskID    string    `json:"task_id" jsonschema:"Task ID, as list_tasks and get_session answer it"`
	ToolCall  indexOrID `json:"tool_call,omitempty" jsonschema:"Answer only the A2A parts of one tool call, by its index or ID"`
	FromItem  int       `json:"from_item,omitempty" jsonschema:"First message or artifact index to answer; continue with next_item"`
	MaxBytes  int       `json:"max_bytes,omitempty" jsonschema:"Budget in bytes for the JSON answer"`
}

// indexOrID names a tool call by ID or index. A JSON integer is read as
// the index it spells.
type indexOrID string

func (r *indexOrID) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*r = indexOrID(text)
		return nil
	}
	var index int
	if err := json.Unmarshal(data, &index); err != nil {
		return errors.New("must be a string or an integer")
	}
	*r = indexOrID(strconv.Itoa(index))
	return nil
}

func getTaskSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[GetTaskInput](&jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[indexOrID](): {Types: []string{"string", "integer"}},
	}})
	if err != nil {
		panic(fmt.Sprintf("get_task input schema: %v", err))
	}
	return schema
}

// GetTaskOutput is one task's messages and artifacts in timeline order, or
// with tool_call one tool call's parts. Truncated is set whenever the answer
// is incomplete: more items remain, or an item or part was left out.
type GetTaskOutput struct {
	SessionID  string         `json:"session_id"`
	TaskID     string         `json:"task_id"`
	State      string         `json:"state"`
	Timestamp  string         `json:"timestamp,omitempty"`
	TotalItems int            `json:"total_items"`
	FromItem   int            `json:"from_item"`
	NextItem   int            `json:"next_item,omitempty"`
	Truncated  bool           `json:"truncated,omitempty"`
	Hint       string         `json:"hint,omitempty"`
	Items      []TaskItem     `json:"items,omitempty"`
	ToolCall   *ToolCallParts `json:"tool_call,omitempty"`
}

var (
	omittedTurnsHint  = fmt.Sprintf("older turns were left out to fit max_bytes: raise max_bytes (at most %d), pick turns with last_turns, or page through them with %s", maxSessionMaxBytes, listTasksToolName)
	turnBoundHint     = fmt.Sprintf("older turns were left out: one call reads at most %d turns; page through them with %s", maxSummaryTurns, listTasksToolName)
	clippedTurnHint   = "the newest turn alone exceeds max_bytes and was clipped: read it with " + getTaskToolName
	clippedTasksHint  = "summaries marked clipped exceeded their share of max_bytes: read one with " + getTaskToolName + ", or lower page_size"
	omittedItemHint   = fmt.Sprintf("items with omitted_bytes exceed max_bytes: raise max_bytes (at most %d), or read their tool calls with tool_call", maxSessionMaxBytes)
	omittedPartHint   = fmt.Sprintf("parts counted in omitted_bytes exceed max_bytes: raise max_bytes (at most %d)", maxSessionMaxBytes)
	continueItemsHint = "continue with from_item: "
)

func (h *Handler) registerSessionTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        createSessionToolName,
		Description: "Start a Session with an Agent, optionally sending its first message. With message it returns once the turn is accepted; read the reply with get_session. A retry with the same request_id returns the same Session and turn.",
	}, h.createSession)
	mcp.AddTool(server, &mcp.Tool{
		Name:        getSessionToolName,
		Description: "Read a Session's state, failure and turns, oldest first, within a byte budget.",
		Annotations: readOnly(),
	}, h.getSession)
	mcp.AddTool(server, &mcp.Tool{
		Name:        listTasksToolName,
		Description: "Page through a Session's tasks, oldest first, each as a summary: user message, reply and tool call stubs.",
		Annotations: readOnly(),
	}, h.listTasks)
	mcp.AddTool(server, &mcp.Tool{
		Name:        getTaskToolName,
		Description: "Read one task of a Session as its A2A messages and artifacts in timeline order, paged by item, or the A2A parts of one of its tool calls.",
		InputSchema: getTaskSchema(),
		Annotations: readOnly(),
	}, h.getSessionTask)
	mcp.AddTool(server, &mcp.Tool{
		Name:        deleteSessionToolName,
		Description: "Delete a Session and its runtime state. Fails while the Session has active turns, or with a conflict while another operation on the Session is in flight; retry it.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input SessionIDInput) (*mcp.CallToolResult, SessionOutput, error) {
		session, err := h.sessions.Delete(ctx, input.SessionID, false)
		if err != nil {
			return sessionToolError(err), SessionOutput{}, nil
		}
		return sessionResult(session)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        renameSessionToolName,
		Description: "Set or clear a Session's display name",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(false), IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input RenameSessionInput) (*mcp.CallToolResult, SessionOutput, error) {
		request := &apiv1.UpdateSessionNameRequest{SessionId: input.SessionID, Name: input.Name}
		if err := protovalidate.Validate(request); err != nil {
			return toolError(err), SessionOutput{}, nil
		}
		session, err := h.sessions.Rename(ctx, request.GetSessionId(), request.GetName())
		if err != nil {
			return sessionToolError(err), SessionOutput{}, nil
		}
		return sessionResult(session)
	})
}

// createSession creates the Session as the caller and sends the first message
// once per request ID.
func (h *Handler) createSession(ctx context.Context, _ *mcp.CallToolRequest, input CreateSessionInput) (*mcp.CallToolResult, CreateSessionOutput, error) {
	if input.Message != "" {
		if strings.TrimSpace(input.Message) == "" {
			return toolError(errors.New("message is blank: omit it to create the Session without a first message")), CreateSessionOutput{}, nil
		}
		if len(input.Message) > maxFirstMessageBytes || !utf8.ValidString(input.Message) {
			return toolError(fmt.Errorf("message must be valid UTF-8 of at most %d bytes", maxFirstMessageBytes)), CreateSessionOutput{}, nil
		}
	}
	create := &apiv1.CreateSessionRequest{
		Agent:     &apiv1.ResourceReference{Namespace: input.Agent.Namespace, Name: input.Agent.Name},
		RequestId: stableRequestID(input.RequestID),
		Name:      input.Name,
	}
	if err := protovalidate.Validate(create); err != nil {
		return toolError(err), CreateSessionOutput{}, nil
	}
	session, err := h.sessions.Create(ctx, create.GetAgent(), create.GetRequestId(), create.GetName())
	if err != nil {
		return sessionToolError(err), CreateSessionOutput{}, nil
	}
	output := CreateSessionOutput{Session: sessionSummary(session)}
	if input.Message != "" {
		turn, err := h.firstTurn(ctx, session, create.GetRequestId(), input.Message)
		if err != nil {
			return toolError(fmt.Errorf("session %s was created but its first message was not accepted (retry with request_id %q to reuse it): %s", session.GetId(), create.GetRequestId(), serviceerrors.MessageOf(err))), CreateSessionOutput{}, nil
		}
		first := readTurn(turn).summary()
		output.Turn = &first
	}
	return nil, output, nil
}

// firstTurn sends a Session's first message under an ID derived from the
// Session and request ID. The gateway accepts a task-opening message once per
// conversation, so a retry answers the turn the first send started.
func (h *Handler) firstTurn(ctx context.Context, session *apiv1.Session, requestID, message string) (*a2atype.Task, error) {
	messageID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("mcp/first-message/"+session.GetId()+"/"+requestID)).String()
	return h.invoke(ctx, InvokeAgentInput{SessionID: session.GetId(), Message: message, MessageID: messageID}, true)
}

func (h *Handler) getSession(ctx context.Context, _ *mcp.CallToolRequest, input GetSessionInput) (*mcp.CallToolResult, GetSessionOutput, error) {
	maxBytes, err := sessionMaxBytes(input.MaxBytes)
	if err != nil {
		return toolError(err), GetSessionOutput{}, nil
	}
	if input.LastTurns < 0 {
		return toolError(errors.New("last_turns must not be negative")), GetSessionOutput{}, nil
	}
	session, err := h.sessions.Get(ctx, input.SessionID)
	if err != nil {
		return sessionToolError(err), GetSessionOutput{}, nil
	}
	tasks, err := h.listTurns(ctx, session)
	if err != nil {
		return sessionToolError(err), GetSessionOutput{}, nil
	}
	output := GetSessionOutput{Session: sessionSummary(session), TotalTurns: len(tasks), Turns: []Turn{}}
	budget := maxBytes - jsonSize(output) - max(len(omittedTurnsHint), len(turnBoundHint)) - envelopeSlack
	first := 0
	if input.LastTurns > 0 {
		first = max(len(tasks)-input.LastTurns, 0)
	}
	turns, omitted, clipped, err := h.packNewestTurns(ctx, session, tasks, first, budget)
	if err != nil {
		return sessionToolError(err), GetSessionOutput{}, nil
	}
	if turns != nil {
		output.Turns = turns
	}
	output.OmittedTurns = omitted
	switch {
	case omitted > 0 && len(turns) == maxSummaryTurns:
		output.Truncated, output.Hint = true, turnBoundHint
	case omitted > 0:
		output.Truncated, output.Hint = true, omittedTurnsHint
	case clipped:
		output.Truncated, output.Hint = true, clippedTurnHint
	}
	return nil, output, nil
}

// packNewestTurns reads tasks[first:] whole, newest first, a batch at a time
// and at most maxSummaryTurns, and keeps those that fit budget. It answers
// them oldest first and how many older turns it left out. When even the
// newest does not fit, its summary is clipped to fit.
func (h *Handler) packNewestTurns(ctx context.Context, session *apiv1.Session, tasks []a2atype.TaskID, first, budget int) (kept []Turn, omitted int, clipped bool, err error) {
	used := 0
	next := len(tasks) - 1
	fetched := 0
pack:
	for next >= first && fetched < maxSummaryTurns {
		n := min(fetchBatch, next-first+1, maxSummaryTurns-fetched)
		batch, err := h.fetchTasks(ctx, session, tasks[next-n+1:next+1])
		if err != nil {
			return nil, 0, false, err
		}
		fetched += n
		for j := n - 1; j >= 0; j-- {
			item := readTurn(batch[j]).summary()
			if size := jsonSize(item) + 1; used+size <= budget {
				kept, used, next = append(kept, item), used+size, next-1
				continue
			}
			if len(kept) == 0 {
				if cut, ok := fitTurn(item, budget-1); ok {
					kept, clipped, next = append(kept, cut), true, next-1
				}
			}
			break pack
		}
	}
	slices.Reverse(kept)
	return kept, next - first + 1, clipped, nil
}

func (h *Handler) listTasks(ctx context.Context, _ *mcp.CallToolRequest, input ListTasksInput) (*mcp.CallToolResult, ListTasksOutput, error) {
	maxBytes, err := sessionMaxBytes(input.MaxBytes)
	if err != nil {
		return toolError(err), ListTasksOutput{}, nil
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = defaultTaskPageSize
	}
	if pageSize < 1 || pageSize > 100 {
		return toolError(errors.New("page_size must be between 1 and 100")), ListTasksOutput{}, nil
	}
	budget := maxBytes - jsonSize(ListTasksOutput{NextPageToken: input.PageToken}) - len(clippedTasksHint) - envelopeSlack
	if budget/pageSize < minTaskShare {
		return toolError(fmt.Errorf("max_bytes %d leaves under %d bytes per task at page_size %d: raise max_bytes or lower page_size", maxBytes, minTaskShare, pageSize)), ListTasksOutput{}, nil
	}
	session, err := h.sessions.Get(ctx, input.SessionID)
	if err != nil {
		return sessionToolError(err), ListTasksOutput{}, nil
	}
	listed, err := h.gateway.ListTasks(interactionContext(ctx), &a2atype.ListTasksRequest{
		Tenant: sessionTenant(session), ContextID: session.GetId(),
		PageSize: pageSize, PageToken: input.PageToken, IncludeArtifacts: true,
	})
	if err != nil {
		return sessionToolError(err), ListTasksOutput{}, nil
	}
	output := ListTasksOutput{SessionID: session.GetId(), TotalSize: listed.TotalSize, NextPageToken: listed.NextPageToken, Tasks: make([]Turn, 0, len(listed.Tasks))}
	share := (maxBytes - jsonSize(output) - len(clippedTasksHint) - envelopeSlack) / max(len(listed.Tasks), 1)
	for _, task := range listed.Tasks {
		if task.ContextID != "" && task.ContextID != session.GetId() {
			return toolError(fmt.Errorf("task %s does not belong to session %s", task.ID, session.GetId())), ListTasksOutput{}, nil
		}
		item := readTurn(task).summary()
		if jsonSize(item)+1 > share {
			item, _ = fitTurn(item, share-1)
			output.Truncated, output.Hint = true, clippedTasksHint
		}
		output.Tasks = append(output.Tasks, item)
	}
	return nil, output, nil
}

func (h *Handler) getSessionTask(ctx context.Context, _ *mcp.CallToolRequest, input GetTaskInput) (*mcp.CallToolResult, GetTaskOutput, error) {
	maxBytes, err := sessionMaxBytes(input.MaxBytes)
	if err != nil {
		return toolError(err), GetTaskOutput{}, nil
	}
	if input.FromItem < 0 {
		return toolError(errors.New("from_item must not be negative")), GetTaskOutput{}, nil
	}
	taskID := strings.TrimSpace(input.TaskID)
	if taskID == "" {
		return toolError(errors.New("task_id is required")), GetTaskOutput{}, nil
	}
	session, err := h.sessions.Get(ctx, input.SessionID)
	if err != nil {
		return sessionToolError(err), GetTaskOutput{}, nil
	}
	whole, err := h.fetchTasks(ctx, session, []a2atype.TaskID{a2atype.TaskID(taskID)})
	if err != nil {
		return sessionToolError(err), GetTaskOutput{}, nil
	}
	t := readTurn(whole[0])
	output := GetTaskOutput{
		SessionID: session.GetId(), TaskID: string(t.task.ID),
		State: t.state(), Timestamp: t.timestamp(), TotalItems: t.items(), FromItem: input.FromItem,
	}
	if input.ToolCall != "" {
		call := t.call(strings.TrimSpace(string(input.ToolCall)))
		if call == nil {
			return toolError(fmt.Errorf("task %s of session %s has no tool call %s (it has %d, indexed from 0)", t.task.ID, session.GetId(), input.ToolCall, len(t.calls))), GetTaskOutput{}, nil
		}
		parts, omitted := call.parts(maxBytes - jsonSize(output) - len(omittedPartHint) - envelopeSlack)
		output.ToolCall = &parts
		if omitted {
			output.Truncated, output.Hint = true, omittedPartHint
		}
		return nil, output, nil
	}
	if input.FromItem > t.items() {
		return toolError(fmt.Errorf("from_item %d is past the task's %d items", input.FromItem, t.items())), GetTaskOutput{}, nil
	}
	budget := maxBytes - jsonSize(output) - len(omittedItemHint) - len(continueItemsHint) - envelopeSlack
	items, next, omitted := packItems(t, input.FromItem, budget)
	output.Items = items
	var hints []string
	if omitted {
		hints = append(hints, omittedItemHint)
	}
	if next < t.items() {
		output.NextItem = next
		hints = append(hints, continueItemsHint+strconv.Itoa(next))
	}
	if len(hints) > 0 {
		output.Truncated, output.Hint = true, strings.Join(hints, "; ")
	}
	return nil, output, nil
}

// packItems answers the turn's items from on while they fit budget, the index
// of the first left for the next page, and whether an item was left out. The
// first item is always answered: when it alone exceeds the budget, as its
// index and size.
func packItems(t *turn, from, budget int) (items []TaskItem, next int, omitted bool) {
	used := 0
	next = from
	for ; next < t.items(); next++ {
		item := t.item(next)
		size := jsonSize(item) + 1
		if used+size > budget {
			if len(items) > 0 {
				break
			}
			item = TaskItem{Index: next, OmittedBytes: size - 1}
			size, omitted = jsonSize(item)+1, true
		}
		items, used = append(items, item), used+size
	}
	return items, next, omitted
}

// listTurns is the IDs of the Session's tasks, oldest first.
func (h *Handler) listTurns(ctx context.Context, session *apiv1.Session) ([]a2atype.TaskID, error) {
	none := 0
	routed := interactionContext(ctx)
	request := &a2atype.ListTasksRequest{Tenant: sessionTenant(session), ContextID: session.GetId(), PageSize: 100, HistoryLength: &none}
	var ids []a2atype.TaskID
	for {
		listed, err := h.gateway.ListTasks(routed, request)
		if err != nil {
			return nil, err
		}
		for _, task := range listed.Tasks {
			ids = append(ids, task.ID)
		}
		if listed.NextPageToken == "" {
			return ids, nil
		}
		request.PageToken = listed.NextPageToken
	}
}

// fetchTasks reads tasks whole, fetchBatch at a time, in their order.
func (h *Handler) fetchTasks(ctx context.Context, session *apiv1.Session, ids []a2atype.TaskID) ([]*a2atype.Task, error) {
	out := make([]*a2atype.Task, len(ids))
	routed := interactionContext(ctx)
	tenant := sessionTenant(session)
	group, groupCtx := errgroup.WithContext(routed)
	group.SetLimit(fetchBatch)
	for i, id := range ids {
		group.Go(func() error {
			whole, err := h.gateway.GetTask(groupCtx, &a2atype.GetTaskRequest{Tenant: tenant, ID: id})
			if err != nil {
				return fmt.Errorf("turn %s: %w", id, err)
			}
			if whole.ContextID != "" && whole.ContextID != session.GetId() {
				return fmt.Errorf("turn %s does not belong to session %s", id, session.GetId())
			}
			out[i] = whole
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

func sessionMaxBytes(n int) (int, error) {
	if n == 0 {
		return defaultSessionMaxBytes, nil
	}
	if n < minSessionMaxBytes || n > maxSessionMaxBytes {
		return 0, fmt.Errorf("max_bytes must be between %d and %d", minSessionMaxBytes, maxSessionMaxBytes)
	}
	return n, nil
}

func sessionTenant(session *apiv1.Session) string {
	agent := session.GetAgent().GetRef()
	return agent.GetNamespace() + "/" + agent.GetName()
}

func sessionResult(session *apiv1.Session) (*mcp.CallToolResult, SessionOutput, error) {
	return nil, SessionOutput{Session: sessionSummary(session)}, nil
}

// sessionToolError is a service error as a tool result: its public message,
// and its code under kagent.dev/error-code so a client can tell a retryable
// conflict from a refusal.
func sessionToolError(err error) *mcp.CallToolResult {
	result := toolError(errors.New(serviceerrors.MessageOf(err)))
	if code := serviceerrors.CodeOf(err); code != "" {
		result.Meta = mcp.Meta{"kagent.dev/error-code": string(code)}
	}
	return result
}

func readOnly() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}
}
