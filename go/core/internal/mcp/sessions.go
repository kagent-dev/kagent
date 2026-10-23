package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"google.golang.org/protobuf/encoding/protojson"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

const (
	createSessionToolName  = "create_session"
	getSessionToolName     = "get_session"
	getSessionTurnToolName = "get_session_turn"
	deleteSessionToolName  = "delete_session"
	renameSessionToolName  = "rename_session"

	// maxFirstMessageBytes bounds create_session's first message.
	maxFirstMessageBytes = 128 << 10
	// maxListedTurns bounds how many turns of one Session are addressed.
	maxListedTurns = 1000
	// maxSummaryTurns bounds how many turns one summary or transcript reads.
	maxSummaryTurns = 200
	// fetchBatch turns are read whole at a time, newest first.
	fetchBatch = 8
	// envelopeSlack is what an answer's fields set after its budget was taken
	// may add.
	envelopeSlack = 128
)

var sessionDescriptions = map[string]string{
	"agent":           "The Agent the Session talks to",
	"agent.namespace": "Agent namespace",
	"agent.name":      "Agent name",
	"request_id":      "Stable idempotency key; reuse it with identical inputs on a retry to get the same Session and first turn",
	"name":            "Optional display name",
}

type CreateSessionOutput struct {
	Session SessionSummary       `json:"session"`
	Turn    *InvokeSessionOutput `json:"turn,omitempty"`
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
	Detail    string `json:"detail,omitempty" jsonschema:"summary (default): per turn the user message, the reply and tool call stubs; transcript: every message, tool arguments clipped and tool results as stubs"`
	LastTurns int    `json:"last_turns,omitempty" jsonschema:"Read only the last n turns; poll with 1 until the last turn is final"`
	MaxBytes  int    `json:"max_bytes,omitempty" jsonschema:"Answer budget in bytes; the oldest turns are dropped to fit"`
}

type GetSessionOutput struct {
	Session      SessionSummary `json:"session"`
	Detail       string         `json:"detail"`
	TotalTurns   int            `json:"total_turns"`
	OmittedTurns int            `json:"omitted_turns,omitempty"`
	Truncated    bool           `json:"truncated,omitempty"`
	Hint         string         `json:"hint,omitempty"`
	Turns        []Turn         `json:"turns"`
}

type GetSessionTurnInput struct {
	SessionID   string `json:"session_id" jsonschema:"Session UUID"`
	Turn        string `json:"turn" jsonschema:"Task ID, turn index from 0, or last"`
	Detail      string `json:"detail,omitempty" jsonschema:"transcript (default) or full (A2A messages and artifacts as JSON)"`
	ToolCall    string `json:"tool_call,omitempty" jsonschema:"Answer one tool call whole, by its index or ID"`
	FromMessage int    `json:"from_message,omitempty" jsonschema:"First message index to answer; continue with next_message"`
	MaxBytes    int    `json:"max_bytes,omitempty" jsonschema:"Answer budget in bytes"`
}

type GetSessionTurnOutput struct {
	SessionID     string          `json:"session_id"`
	Detail        string          `json:"detail"`
	Index         int             `json:"index"`
	TotalTurns    int             `json:"total_turns"`
	TaskID        string          `json:"task_id"`
	State         string          `json:"state"`
	Timestamp     string          `json:"timestamp,omitempty"`
	TotalMessages int             `json:"total_messages"`
	FromMessage   int             `json:"from_message"`
	NextMessage   int             `json:"next_message,omitempty"`
	Truncated     bool            `json:"truncated,omitempty"`
	Hint          string          `json:"hint,omitempty"`
	Messages      []TurnMessage   `json:"messages,omitempty"`
	History       []any           `json:"history,omitempty"`
	ToolCall      *ToolCallDetail `json:"tool_call,omitempty"`
}

var (
	omittedTurnsHint = fmt.Sprintf("older turns were left out to fit max_bytes: raise max_bytes (at most %d), pick turns with last_turns, or read one turn with %s", maxSessionMaxBytes, getSessionTurnToolName)
	turnBoundHint    = fmt.Sprintf("older turns were left out: one call reads at most %d turns; pick turns with last_turns or read one with %s", maxSummaryTurns, getSessionTurnToolName)
	clippedTurnHint  = "the newest turn alone exceeds max_bytes and was clipped: read it with " + getSessionTurnToolName
)

func (h *Handler) registerSessionTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        createSessionToolName,
		Description: "Start a Session with an Agent, optionally sending its first message. With message it returns once the turn is accepted; read the reply with get_session. A retry with the same request_id returns the same Session and turn.",
		InputSchema: createSessionSchema(),
	}, h.createSession)
	mcp.AddTool(server, &mcp.Tool{
		Name:        getSessionToolName,
		Description: "Read a Session's state, failure and turns, oldest first, within a byte budget.",
		Annotations: readOnly(),
	}, h.getSession)
	mcp.AddTool(server, &mcp.Tool{
		Name:        getSessionTurnToolName,
		Description: "Read one turn of a Session in full, or one of its tool calls with arguments and result, paged by message.",
		Annotations: readOnly(),
	}, h.getSessionTurn)
	mcp.AddTool(server, &mcp.Tool{
		Name:        deleteSessionToolName,
		Description: "Delete a Session and its runtime state. Fails with a conflict while another operation on the Session is in flight; retry it.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input SessionIDInput) (*mcp.CallToolResult, SessionOutput, error) {
		session, err := h.sessions.Delete(ctx, input.SessionID)
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

// createSessionSchema is CreateSessionRequest's schema, so every field the
// request carries is accepted, plus the first message.
func createSessionSchema() *jsonschema.Schema {
	schema := protoMessageSchema((&apiv1.CreateSessionRequest{}).ProtoReflect().Descriptor(), sessionDescriptions)
	schema.Required = slices.DeleteFunc(schema.Required, func(name string) bool { return name == "request_id" })
	schema.Properties["message"] = &jsonschema.Schema{Type: "string", Description: "Optional first message, sent as invoke_session sends one"}
	schema.PropertyOrder = append(schema.PropertyOrder, "message")
	return schema
}

// createSession decodes everything but message into a CreateSessionRequest,
// creates the Session as the caller and sends the first message once per
// request ID.
func (h *Handler) createSession(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var arguments map[string]json.RawMessage
	if err := json.Unmarshal(request.Params.Arguments, &arguments); err != nil {
		return toolError(fmt.Errorf("arguments must be an object: %w", err)), nil
	}
	var message string
	if raw, ok := arguments["message"]; ok {
		if err := json.Unmarshal(raw, &message); err != nil {
			return toolError(fmt.Errorf("message must be a string: %w", err)), nil
		}
		delete(arguments, "message")
		if strings.TrimSpace(message) == "" {
			return toolError(errors.New("message is blank: omit it to create the Session without a first message")), nil
		}
		if len(message) > maxFirstMessageBytes || !utf8.ValidString(message) {
			return toolError(fmt.Errorf("message must be valid UTF-8 of at most %d bytes", maxFirstMessageBytes)), nil
		}
	}
	data, err := json.Marshal(arguments)
	if err != nil {
		return toolError(err), nil
	}
	create := &apiv1.CreateSessionRequest{}
	if err := protojson.Unmarshal(data, create); err != nil {
		return toolError(fmt.Errorf("invalid arguments: %w", err)), nil
	}
	create.RequestId = stableRequestID(create.GetRequestId())
	if err := protovalidate.Validate(create); err != nil {
		return toolError(err), nil
	}
	session, err := h.sessions.CreateFromRequest(ctx, create)
	if err != nil {
		return sessionToolError(err), nil
	}
	output := CreateSessionOutput{Session: sessionSummary(session)}
	text := fmt.Sprintf("Created Session %s", session.GetId())
	if message != "" {
		turn, err := h.firstTurn(ctx, session, create.GetRequestId(), message)
		if err != nil {
			return toolError(fmt.Errorf("session %s was created but its first message was not accepted (retry with request_id %q to reuse it): %w", session.GetId(), create.GetRequestId(), err)), nil
		}
		_, invoked := invocationResult(InvokeSessionInput{SessionID: session.GetId()}, turn)
		output.Turn = &invoked
		text += fmt.Sprintf("; turn %s is %s", invoked.TaskID, invoked.State)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}, StructuredContent: output}, nil
}

// firstTurn sends a Session's first message once per request ID: the message
// ID is derived from the Session and request ID, and when the first turn
// already carries it, that turn is answered and nothing is sent.
func (h *Handler) firstTurn(ctx context.Context, session *apiv1.Session, requestID, message string) (*a2atype.Task, error) {
	messageID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("mcp/first-message/"+session.GetId()+"/"+requestID)).String()
	tenant := sessionTenant(session)
	routed := interactionContext(ctx)
	listed, err := h.gateway.ListTasks(routed, &a2atype.ListTasksRequest{Tenant: tenant, ContextID: session.GetId(), PageSize: 1})
	if err != nil {
		return nil, err
	}
	if len(listed.Tasks) > 0 {
		first := listed.Tasks[0]
		for _, sent := range first.History {
			if sent.ID == messageID {
				return first, nil
			}
		}
	}
	return h.invoke(ctx, InvokeSessionInput{SessionID: session.GetId(), Message: message, MessageID: messageID}, true)
}

func (h *Handler) getSession(ctx context.Context, _ *mcp.CallToolRequest, input GetSessionInput) (*mcp.CallToolResult, GetSessionOutput, error) {
	detail, err := detailLevel(input.Detail, detailSummary, detailSummary, detailTranscript)
	if err != nil {
		return toolError(err), GetSessionOutput{}, nil
	}
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
		return toolError(err), GetSessionOutput{}, nil
	}
	output := GetSessionOutput{Session: sessionSummary(session), Detail: detail, TotalTurns: len(tasks), Turns: []Turn{}}
	budget := maxBytes - jsonSize(output) - max(len(omittedTurnsHint), len(turnBoundHint)) - envelopeSlack
	first := 0
	if input.LastTurns > 0 {
		first = max(len(tasks)-input.LastTurns, 0)
	}
	render := func(t *turn) Turn { return t.summary() }
	if detail == detailTranscript {
		render = func(t *turn) Turn { return t.transcript() }
	}
	turns, omitted, clipped, err := h.packNewestTurns(ctx, session, tasks, first, budget, render)
	if err != nil {
		return toolError(err), GetSessionOutput{}, nil
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
	text := fmt.Sprintf("Session %s is %s with %d turns", session.GetId(), session.GetState(), len(tasks))
	if n := len(output.Turns); n > 0 {
		last := output.Turns[n-1]
		text += fmt.Sprintf("; turn %d is %s", last.Index, last.State)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, output, nil
}

// packNewestTurns reads tasks[first:] whole, newest first, a batch at a time
// and at most maxSummaryTurns, and keeps those that fit budget. It answers
// them oldest first and how many older turns it left out. When even the
// newest does not fit, its summary is clipped to fit.
func (h *Handler) packNewestTurns(ctx context.Context, session *apiv1.Session, tasks []*a2atype.Task, first, budget int, render func(*turn) Turn) (kept []Turn, omitted int, clipped bool, err error) {
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
			t := readTurn(next, batch[j])
			item := render(t)
			if size := jsonSize(item) + 1; used+size <= budget {
				kept, used, next = append(kept, item), used+size, next-1
				continue
			}
			if len(kept) == 0 {
				if cut, ok := fitTurn(t.summary(), budget-1); ok {
					kept, clipped, next = append(kept, cut), true, next-1
				}
			}
			break pack
		}
	}
	slices.Reverse(kept)
	return kept, next - first + 1, clipped, nil
}

func (h *Handler) getSessionTurn(ctx context.Context, _ *mcp.CallToolRequest, input GetSessionTurnInput) (*mcp.CallToolResult, GetSessionTurnOutput, error) {
	detail, err := detailLevel(input.Detail, detailTranscript, detailTranscript, detailFull)
	if err != nil {
		return toolError(err), GetSessionTurnOutput{}, nil
	}
	maxBytes, err := sessionMaxBytes(input.MaxBytes)
	if err != nil {
		return toolError(err), GetSessionTurnOutput{}, nil
	}
	if input.FromMessage < 0 {
		return toolError(errors.New("from_message must not be negative")), GetSessionTurnOutput{}, nil
	}
	ref := strings.TrimSpace(input.Turn)
	if ref == "" {
		return toolError(errors.New("turn is required: a task ID, an index or last")), GetSessionTurnOutput{}, nil
	}
	session, err := h.sessions.Get(ctx, input.SessionID)
	if err != nil {
		return sessionToolError(err), GetSessionTurnOutput{}, nil
	}
	tasks, err := h.listTurns(ctx, session)
	if err != nil {
		return toolError(err), GetSessionTurnOutput{}, nil
	}
	index := turnIndex(tasks, ref)
	if index < 0 {
		return toolError(fmt.Errorf("session %s has no turn %s (it has %d, indexed from 0)", session.GetId(), ref, len(tasks))), GetSessionTurnOutput{}, nil
	}
	whole, err := h.fetchTasks(ctx, session, tasks[index:index+1])
	if err != nil {
		return toolError(err), GetSessionTurnOutput{}, nil
	}
	t := readTurn(index, whole[0])
	output := GetSessionTurnOutput{
		SessionID: session.GetId(), Detail: detail, Index: index, TotalTurns: len(tasks), TaskID: string(t.task.ID),
		State: t.state(), Timestamp: t.timestamp(), TotalMessages: t.items(), FromMessage: input.FromMessage,
	}
	if input.ToolCall != "" {
		call := t.call(strings.TrimSpace(input.ToolCall))
		if call == nil {
			return toolError(fmt.Errorf("turn %d of session %s has no tool call %s (it has %d, indexed from 0)", index, session.GetId(), input.ToolCall, len(t.calls))), GetSessionTurnOutput{}, nil
		}
		answer, clipped := t.toolCallDetail(call, maxBytes-jsonSize(output)-envelopeSlack)
		output.ToolCall, output.Truncated = &answer, clipped
		return turnResult(output), output, nil
	}
	if input.FromMessage > t.items() {
		return toolError(fmt.Errorf("from_message %d is past the turn's %d messages", input.FromMessage, t.items())), GetSessionTurnOutput{}, nil
	}
	budget := maxBytes - jsonSize(output) - envelopeSlack
	var next int
	if detail == detailFull {
		output.History, next, err = packForward(t.items(), input.FromMessage, budget, func(i int) (any, error) {
			return t.rawItem(i)
		}, func(i int, raw any, _ int) any {
			return map[string]any{
				"index":   i,
				"omitted": fmt.Sprintf("%d bytes exceed max_bytes: read it clipped with detail transcript, or its tool calls with tool_call", jsonSize(raw)),
			}
		})
		if err != nil {
			return toolError(err), GetSessionTurnOutput{}, nil
		}
	} else {
		output.Messages, next, _ = packForward(t.items(), input.FromMessage, budget, func(i int) (TurnMessage, error) {
			return t.messageView(i), nil
		}, func(_ int, m TurnMessage, budget int) TurnMessage { return fitMessage(m, budget) })
	}
	if next < t.items() {
		output.NextMessage, output.Truncated, output.Hint = next, true, "continue with from_message: "+strconv.Itoa(next)
	}
	return turnResult(output), output, nil
}

func turnResult(output GetSessionTurnOutput) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{
		Text: fmt.Sprintf("Turn %d of Session %s (%s)", output.Index, output.SessionID, output.State),
	}}}
}

// packForward renders items from on while they fit budget and answers them
// and the index of the first left out. The first item is always kept: fit
// cuts it down when it alone exceeds the budget.
func packForward[T any](n, from, budget int, render func(int) (T, error), fit func(int, T, int) T) ([]T, int, error) {
	var out []T
	used := 0
	i := from
	for ; i < n; i++ {
		item, err := render(i)
		if err != nil {
			return nil, 0, err
		}
		size := jsonSize(item) + 1
		if used+size > budget {
			if len(out) > 0 {
				break
			}
			item = fit(i, item, budget-1)
			size = jsonSize(item) + 1
		}
		out, used = append(out, item), used+size
	}
	return out, i, nil
}

// listTurns is the Session's tasks without their history, oldest first: what
// its turns are addressed by.
func (h *Handler) listTurns(ctx context.Context, session *apiv1.Session) ([]*a2atype.Task, error) {
	none := 0
	routed := interactionContext(ctx)
	request := &a2atype.ListTasksRequest{Tenant: sessionTenant(session), ContextID: session.GetId(), PageSize: 100, HistoryLength: &none}
	var tasks []*a2atype.Task
	for {
		listed, err := h.gateway.ListTasks(routed, request)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, listed.Tasks...)
		if listed.NextPageToken == "" {
			return tasks, nil
		}
		if len(tasks) >= maxListedTurns {
			return nil, fmt.Errorf("session %s has more than %d turns, more than this tool reads", session.GetId(), maxListedTurns)
		}
		request.PageToken = listed.NextPageToken
	}
}

// fetchTasks reads tasks whole, fetchBatch at a time, in their order.
func (h *Handler) fetchTasks(ctx context.Context, session *apiv1.Session, tasks []*a2atype.Task) ([]*a2atype.Task, error) {
	out := make([]*a2atype.Task, len(tasks))
	routed := interactionContext(ctx)
	tenant := sessionTenant(session)
	group, groupCtx := errgroup.WithContext(routed)
	group.SetLimit(fetchBatch)
	for i, task := range tasks {
		group.Go(func() error {
			whole, err := h.gateway.GetTask(groupCtx, &a2atype.GetTaskRequest{Tenant: tenant, ID: task.ID})
			if err != nil {
				return fmt.Errorf("turn %s: %w", task.ID, err)
			}
			if whole.ContextID != "" && whole.ContextID != session.GetId() {
				return fmt.Errorf("turn %s does not belong to session %s", task.ID, session.GetId())
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

// turnIndex is the index of the turn ref names in tasks: "last", an index or
// a task ID; -1 for none.
func turnIndex(tasks []*a2atype.Task, ref string) int {
	if ref == "last" {
		return len(tasks) - 1
	}
	if n, err := strconv.Atoi(ref); err == nil {
		if n < 0 || n >= len(tasks) {
			return -1
		}
		return n
	}
	for i, task := range tasks {
		if string(task.ID) == ref {
			return i
		}
	}
	return -1
}

func detailLevel(level, def string, allowed ...string) (string, error) {
	if level == "" {
		return def, nil
	}
	if !slices.Contains(allowed, level) {
		return "", fmt.Errorf("detail must be one of %s", strings.Join(allowed, ", "))
	}
	return level, nil
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
	return session.GetAgent().GetNamespace() + "/" + session.GetAgent().GetName()
}

func sessionResult(session *apiv1.Session) (*mcp.CallToolResult, SessionOutput, error) {
	output := SessionOutput{Session: sessionSummary(session)}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{
		Text: fmt.Sprintf("Session %s is %s", session.GetId(), session.GetState()),
	}}}, output, nil
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
