package driver

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

type parser struct {
	emitted               map[string]string
	currentMessageID      string
	activeBlock           *contentBlockRef
	tools                 map[string]string
	emittedToolCalls      map[string]struct{}
	emittedResults        map[string]struct{}
	terminal              bool
	now                   func() time.Time
	openCall              *runtime.ModelCall
	openUsage             claudeUsage
	openCallToolRequested bool
	// openCallDone is set once a user line follows the open call. A new stream after that is
	// the next call; before it, the stream retries the open call.
	openCallDone     bool
	streamedMessages map[string]struct{}
	// snapshots holds each thread's latest unstreamed call, keyed by parent_tool_use_id.
	snapshots map[string]snapshotCall
	// callStart holds when each thread's next model call starts, keyed by parent_tool_use_id
	// ("" is the main thread).
	callStart map[string]time.Time
	// retried holds the main thread's attempts Claude Code gave up on and retried. They join the
	// next call, which keeps their start and adds their tokens, since the API read each attempt's input.
	retried *runtime.ModelCall
	// startUsage holds the session's token totals per model when this process started, and
	// emittedUsage sums the calls emitted since. A nil startUsage is unknown, so no UsageOnly event is sent.
	startUsage   map[string]modelUsage
	emittedUsage map[string]modelUsage
}

const agentToolName = "Agent"

type snapshotCall struct {
	messageID string
	call      runtime.ModelCall
	// retried holds the attempts the call retried, joined when the call is emitted.
	retried *runtime.ModelCall
}

type claudeUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	OutputTokensDetails      struct {
		ThinkingTokens int64 `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
}

func (u claudeUsage) apply(call *runtime.ModelCall) {
	call.InputTokens = u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
	call.CacheReadTokens, call.CacheWriteTokens = u.CacheReadInputTokens, u.CacheCreationInputTokens
	call.OutputTokens = u.OutputTokens
	call.ReasoningOutputTokens = u.OutputTokensDetails.ThinkingTokens
}

// modelUsage is one model's entry in Claude Code's session totals (result.modelUsage). Unlike
// ModelCall.InputTokens, its InputTokens leaves out the cache read and cache write tokens.
type modelUsage struct {
	InputTokens              int64 `json:"inputTokens"`
	OutputTokens             int64 `json:"outputTokens"`
	CacheReadInputTokens     int64 `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64 `json:"cacheCreationInputTokens"`
	ThinkingTokens           int64 `json:"thinkingTokens"`
}

func (u *modelUsage) add(call *runtime.ModelCall) {
	u.InputTokens += call.InputTokens - call.CacheReadTokens - call.CacheWriteTokens
	u.OutputTokens += call.OutputTokens
	u.CacheReadInputTokens += call.CacheReadTokens
	u.CacheCreationInputTokens += call.CacheWriteTokens
	u.ThinkingTokens += call.ReasoningOutputTokens
}

func (u modelUsage) plus(o modelUsage) modelUsage {
	return modelUsage{u.InputTokens + o.InputTokens, u.OutputTokens + o.OutputTokens,
		u.CacheReadInputTokens + o.CacheReadInputTokens, u.CacheCreationInputTokens + o.CacheCreationInputTokens,
		u.ThinkingTokens + o.ThinkingTokens}
}

// merge overlays the non-zero fields of a message_delta usage on its message_start usage.
func (u claudeUsage) merge(delta claudeUsage) claudeUsage {
	u.InputTokens = cmp.Or(delta.InputTokens, u.InputTokens)
	u.CacheCreationInputTokens = cmp.Or(delta.CacheCreationInputTokens, u.CacheCreationInputTokens)
	u.CacheReadInputTokens = cmp.Or(delta.CacheReadInputTokens, u.CacheReadInputTokens)
	u.OutputTokens = cmp.Or(delta.OutputTokens, u.OutputTokens)
	u.OutputTokensDetails.ThinkingTokens = cmp.Or(delta.OutputTokensDetails.ThinkingTokens, u.OutputTokensDetails.ThinkingTokens)
	return u
}

func newParser(now func() time.Time) *parser {
	return &parser{
		emitted: map[string]string{}, tools: map[string]string{},
		emittedToolCalls: map[string]struct{}{}, emittedResults: map[string]struct{}{},
		now: now, streamedMessages: map[string]struct{}{}, snapshots: map[string]snapshotCall{}, callStart: map[string]time.Time{},
		emittedUsage: map[string]modelUsage{},
	}
}

// flushOpenCall emits a started call that never received final usage.
func (p *parser) flushOpenCall(emit func(Event) error) error {
	if p.openCall == nil {
		return nil
	}
	call := p.openCall
	p.openCall = nil
	p.joinRetries(call)
	call.End, call.UsagePartial = cmp.Or(call.End, p.now()), true
	// The conventions report a generation that ended without a finish reason as "error".
	call.StopReason = tracing.FinishReasonError
	return emit(Event{Kind: EventModelCall, ModelCall: call})
}

// failCall emits an API-rejected request. If an outstanding tool request already ended the
// streamed response, classify the partial call as tool_call rather than an API failure.
func (p *parser) failCall(parent, errorType string, at time.Time, emit func(Event) error) error {
	call := &runtime.ModelCall{ParentToolID: parent, Start: cmp.Or(p.callStart[parent], at), End: at}
	toolRequested := parent == "" && p.openCall != nil && p.openCallToolRequested && !p.openCallDone
	if parent == "" && p.openCall != nil {
		call, p.openCall = p.openCall, nil
		call.End = cmp.Or(call.End, p.now())
		p.openCallToolRequested = false
	}
	if parent == "" {
		p.joinRetries(call)
	}
	p.callStart[parent] = at
	call.StopReason, call.UsagePartial = tracing.FinishReasonError, true
	if toolRequested {
		call.StopReason = tracing.FinishReasonToolCall
	} else {
		call.ErrorType = errorType
	}
	return emit(Event{Kind: EventModelCall, ModelCall: call})
}

// retry counts a retry of the main thread's call. The attempt still open was given up on, so it
// joins the retried attempts.
func (p *parser) retry() {
	if p.retried == nil {
		p.retried = &runtime.ModelCall{}
	}
	retried := p.retried
	retried.Retries++
	// A request rejected before any stream takes the thread's call start.
	retried.Start = cmp.Or(retried.Start, p.openCallStart())
	if attempt := p.openCall; attempt != nil {
		p.openCall = nil
		retried.ResponseModel = cmp.Or(retried.ResponseModel, attempt.ResponseModel)
		retried.InputTokens += attempt.InputTokens
		retried.CacheReadTokens += attempt.CacheReadTokens
		retried.CacheWriteTokens += attempt.CacheWriteTokens
		retried.OutputTokens += attempt.OutputTokens
		retried.ReasoningOutputTokens += attempt.ReasoningOutputTokens
	}
}

// openCallStart is when the main thread's current attempt started.
func (p *parser) openCallStart() time.Time {
	if p.openCall != nil {
		return p.openCall.Start
	}
	return p.callStart[""]
}

// joinRetries gives the main thread's call the start, tokens and count of its retried attempts.
func (p *parser) joinRetries(call *runtime.ModelCall) {
	addRetries(call, p.retried)
	p.retried = nil
}

func addRetries(call, retried *runtime.ModelCall) {
	if retried == nil {
		return
	}
	call.Start = cmp.Or(retried.Start, call.Start)
	call.ResponseModel = cmp.Or(call.ResponseModel, retried.ResponseModel)
	call.Retries += retried.Retries
	call.InputTokens += retried.InputTokens
	call.CacheReadTokens += retried.CacheReadTokens
	call.CacheWriteTokens += retried.CacheWriteTokens
	call.OutputTokens += retried.OutputTokens
	call.ReasoningOutputTokens += retried.ReasoningOutputTokens
}

// flushCalls emits every model call still held when the stream ends.
func (p *parser) flushCalls(emit func(Event) error) error {
	// Attempts retried with no attempt after them still made a call.
	if p.retried != nil && p.openCall == nil {
		p.openCall = &runtime.ModelCall{Start: p.now()}
	}
	if err := p.flushOpenCall(emit); err != nil {
		return err
	}
	return p.flushSnapshots(emit)
}

// flushSnapshots emits the unstreamed calls still held when the stream ends.
func (p *parser) flushSnapshots(emit func(Event) error) error {
	for _, parent := range slices.Sorted(maps.Keys(p.snapshots)) {
		pending := p.snapshots[parent]
		delete(p.snapshots, parent)
		addRetries(&pending.call, pending.retried)
		if err := emit(Event{Kind: EventModelCall, ModelCall: &pending.call}); err != nil {
			return err
		}
	}
	return nil
}

type contentBlockRef struct {
	messageID string
	index     int
}

func parseJSONL(ctx context.Context, r io.Reader, maxEventBytes int, startUsage map[string]modelUsage, seed *DeferredTool, emit func(Event) error) error {
	if maxEventBytes <= 0 {
		return fmt.Errorf("max event bytes must be positive")
	}
	done := make(chan struct{})
	defer close(done)
	var arrived time.Time
	p := newParser(func() time.Time { return arrived })
	p.startUsage = startUsage
	if seed != nil {
		p.tools[seed.ID] = seed.Name
		p.emittedToolCalls[seed.ID] = struct{}{}
	}
	emit = p.countEmitted(emit)
	for item := range readStamped(r, maxEventBytes, done) {
		arrived = item.arrived
		if len(bytes.TrimSpace(item.line)) > 0 {
			if parseErr := p.parseLine(ctx, item.line, emit); parseErr != nil {
				return errors.Join(parseErr, p.flushCalls(emit))
			}
		}
		if item.err == io.EOF {
			break
		}
		if item.err != nil {
			// A closed pipe is how an abandoned process ends, and its open call still happened.
			return errors.Join(fmt.Errorf("read Claude event: %w", item.err), p.flushCalls(emit))
		}
	}
	if err := p.flushCalls(emit); err != nil {
		return err
	}
	if !p.terminal {
		return fmt.Errorf("claude process exited without a terminal result event")
	}
	return nil
}

// waitingLines bounds the queue between the reader and parser. Beyond this limit,
// reading pauses and later lines receive their timestamp when reading resumes.
const waitingLines = 64

type stampedLine struct {
	line    []byte
	arrived time.Time
	err     error
}

// readStamped timestamps each line before queuing it for parsing.
// The bounded queue still applies backpressure when the consumer falls behind.
func readStamped(r io.Reader, max int, done <-chan struct{}) <-chan stampedLine {
	lines := make(chan stampedLine, waitingLines)
	go func() {
		defer close(lines)
		reader := bufio.NewReaderSize(r, min(max+1, 64*1024))
		for {
			line, err := readBoundedLine(reader, max)
			select {
			case lines <- stampedLine{line: line, arrived: time.Now(), err: err}:
			case <-done:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return lines
}

func readBoundedLine(r *bufio.Reader, max int) ([]byte, error) {
	var line []byte
	for {
		fragment, err := r.ReadSlice('\n')
		if len(line)+len(fragment) > max {
			return nil, fmt.Errorf("claude event exceeds %d bytes", max)
		}
		line = append(line, fragment...)
		if err != bufio.ErrBufferFull {
			return line, err
		}
	}
}

func (p *parser) parseLine(ctx context.Context, line []byte, emit func(Event) error) error {
	var envelope struct {
		Type      string          `json:"type"`
		Subtype   string          `json:"subtype"`
		Status    string          `json:"status"`
		SessionID string          `json:"session_id"`
		IsError   bool            `json:"is_error"`
		Result    string          `json:"result"`
		Event     json.RawMessage `json:"event"`
		// ModelUsage is the session's totals so far, including subagents and earlier processes.
		ModelUsage map[string]modelUsage `json:"modelUsage"`
		Message    json.RawMessage       `json:"message"`
		StopReason string                `json:"stop_reason"`
		Deferred   *DeferredTool         `json:"deferred_tool_use"`
		Origin     struct {
			Kind string `json:"kind"`
		} `json:"origin"`

		ParentToolUseID string          `json:"parent_tool_use_id"`
		ToolUseResult   json.RawMessage `json:"tool_use_result"`
		RequestID       string          `json:"request_id"`
		Timestamp       string          `json:"timestamp"`
		// Claude Code writes a failed request as a "<synthetic>" assistant message with these set.
		IsAPIErrorMessage bool   `json:"is_api_error_message"`
		Error             string `json:"error"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return fmt.Errorf("decode Claude event: %w", err)
	}
	switch envelope.Type {
	case "system":
		if envelope.Subtype == "init" && envelope.SessionID != "" {
			p.callStart[""] = p.now()
			return emit(Event{Kind: EventSessionStarted, SessionID: envelope.SessionID})
		}
		if envelope.Subtype == "api_retry" && envelope.ParentToolUseID == "" {
			p.retry()
		}
		// Claude Code prints this before each main-thread call, but not before its retries.
		if envelope.Subtype == "status" && envelope.Status == "requesting" && envelope.ParentToolUseID == "" {
			p.callStart[""] = p.now()
		}
	case "stream_event":
		return p.parseStreamEvent(envelope.Event, envelope.ParentToolUseID != "", emit)
	case "assistant":
		lineTime := p.lineTime(envelope.Timestamp)
		if envelope.IsAPIErrorMessage {
			if err := p.failCall(envelope.ParentToolUseID, envelope.Error, lineTime, emit); err != nil {
				return err
			}
		}
		return p.parseAssistant(envelope.Message, envelope.ParentToolUseID, envelope.RequestID, lineTime, emit)
	case "user":
		lineTime := p.lineTime(envelope.Timestamp)
		// A prompt or tool result: that thread's next model call starts here.
		p.callStart[envelope.ParentToolUseID] = lineTime
		if envelope.ParentToolUseID == "" && p.openCall != nil {
			p.openCallDone = true
		}
		return p.parseUser(ctx, envelope.Message, envelope.ToolUseResult, lineTime, emit)
	case "result":
		if envelope.Origin.Kind == "task-notification" {
			return nil
		}
		if err := p.flushCalls(emit); err != nil {
			return err
		}
		if err := p.emitUnreportedUsage(ctx, envelope.ModelUsage, emit); err != nil {
			return err
		}
		p.terminal = true
		if envelope.StopReason == "tool_deferred" {
			if envelope.IsError || envelope.Subtype != "success" || envelope.Deferred == nil || envelope.Deferred.ID == "" || envelope.Deferred.Name == "" || envelope.SessionID == "" {
				return fmt.Errorf("invalid Claude deferred result")
			}
			return emit(Event{Kind: EventDeferred, SessionID: envelope.SessionID, Deferred: envelope.Deferred})
		}
		if envelope.IsError || envelope.Subtype != "success" {
			message := envelope.Result
			if message == "" {
				message = "Claude execution failed"
			}
			return emit(Event{Kind: EventFailed, SafeMessage: message})
		}
		return emit(Event{Kind: EventCompleted, SessionID: envelope.SessionID})
	}
	return nil
}

func (p *parser) countEmitted(emit func(Event) error) func(Event) error {
	return func(event Event) error {
		if event.Kind == EventModelCall && !event.ModelCall.UsageOnly {
			usage := p.emittedUsage[event.ModelCall.ResponseModel]
			usage.add(event.ModelCall)
			p.emittedUsage[event.ModelCall.ResponseModel] = usage
		}
		return emit(event)
	}
}

// emitUnreportedUsage emits, per model, the tokens in the session totals that no call reported,
// such as the final output of subagent calls.
func (p *parser) emitUnreportedUsage(ctx context.Context, totals map[string]modelUsage, emit func(Event) error) error {
	if p.startUsage == nil || len(totals) == 0 {
		return nil
	}
	emittedUsage := map[string]modelUsage{}
	for model, usage := range p.emittedUsage {
		if usage == (modelUsage{}) {
			continue
		}
		key, ok := totalsKey(model, totals)
		// A model the totals do not name, such as a Bedrock or Vertex ID, could count twice.
		if !ok {
			logging.FromContext(ctx).WarnContext(ctx, "claude session totals leave out a model its calls reported, so this turn adds no tokens that only the totals show", "model", model)
			return nil
		}
		emittedUsage[key] = emittedUsage[key].plus(usage)
	}
	for _, model := range slices.Sorted(maps.Keys(totals)) {
		total, start, emitted := totals[model], p.startUsage[model], emittedUsage[model]
		// A call Claude Code does not count, such as a canceled one, makes the difference negative.
		rest := func(total, start, emitted int64) int64 { return max(total-start-emitted, 0) }
		call := runtime.ModelCall{RequestModel: model, UsageOnly: true,
			OutputTokens:          rest(total.OutputTokens, start.OutputTokens, emitted.OutputTokens),
			CacheReadTokens:       rest(total.CacheReadInputTokens, start.CacheReadInputTokens, emitted.CacheReadInputTokens),
			CacheWriteTokens:      rest(total.CacheCreationInputTokens, start.CacheCreationInputTokens, emitted.CacheCreationInputTokens),
			ReasoningOutputTokens: rest(total.ThinkingTokens, start.ThinkingTokens, emitted.ThinkingTokens),
		}
		call.InputTokens = rest(total.InputTokens, start.InputTokens, emitted.InputTokens) + call.CacheReadTokens + call.CacheWriteTokens
		if call.InputTokens+call.OutputTokens+call.ReasoningOutputTokens == 0 {
			continue
		}
		if err := emit(Event{Kind: EventModelCall, ModelCall: &call}); err != nil {
			return err
		}
	}
	return nil
}

// totalsKey finds the totals entry of a response model. The totals use the requested name, such as
// claude-sonnet-4-5, and a response can name the dated model, such as claude-sonnet-4-5-20250929.
func totalsKey(model string, totals map[string]modelUsage) (string, bool) {
	if _, ok := totals[model]; ok {
		return model, true
	}
	var found string
	for key := range totals {
		if strings.HasPrefix(model, key+"-") && len(key) > len(found) {
			found = key
		}
	}
	return found, found != ""
}

func (p *parser) parseStreamEvent(raw json.RawMessage, subagent bool, emit func(Event) error) error {
	var event struct {
		Type    string `json:"type"`
		Index   int    `json:"index"`
		Message struct {
			ID string `json:"id"`

			Model string      `json:"model"`
			Usage claudeUsage `json:"usage"`
		} `json:"message"`
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`

			StopReason string `json:"stop_reason"`
		} `json:"delta"`
		Usage        claudeUsage `json:"usage"`
		ContentBlock struct {
			Type  string         `json:"type"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		} `json:"content_block"`
	}
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return fmt.Errorf("decode Claude stream event: %w", err)
	}
	switch event.Type {
	case "message_start":
		p.currentMessageID = event.Message.ID
		p.activeBlock = nil
		if subagent {
			return nil
		}
		// Claude Code retries a stream cut off mid-response with a new one and no api_retry line.
		if p.openCall != nil && !p.openCallDone {
			p.retry()
		}
		if err := p.flushOpenCall(emit); err != nil {
			return err
		}
		p.openCallDone = false
		p.streamedMessages[event.Message.ID] = struct{}{}
		// message_start comes after the first token, so the call starts when the request was sent.
		p.openCall = &runtime.ModelCall{ResponseModel: event.Message.Model, ResponseID: event.Message.ID, Start: cmp.Or(p.callStart[""], p.now())}
		p.openCallToolRequested = false
		p.openUsage = event.Message.Usage
		event.Message.Usage.apply(p.openCall)
	case "message_delta":
		if subagent || p.openCall == nil {
			return nil
		}
		call := p.openCall
		p.openCall = nil
		p.openUsage.merge(event.Usage).apply(call)
		p.joinRetries(call)
		call.StopReason, call.End = claudeFinishReason(event.Delta.StopReason), p.now()
		p.openCallToolRequested = false
		p.callStart[""] = call.End
		return emit(Event{Kind: EventModelCall, ModelCall: call})
	case "content_block_delta":
		if event.Delta.Type == "text_delta" && event.Delta.Text != "" {
			key := p.blockKey(event.Index)
			p.emitted[key] += event.Delta.Text
			return emit(Event{Kind: EventTextDelta, Text: event.Delta.Text})
		}
	case "content_block_start":
		p.activeBlock = &contentBlockRef{messageID: p.currentMessageID, index: event.Index}
		if event.ContentBlock.Type == "tool_use" {
			if event.ContentBlock.ID == "" || event.ContentBlock.Name == "" {
				return fmt.Errorf("claude tool_use start requires an id and name")
			}
			if previous := p.tools[event.ContentBlock.ID]; previous != "" && previous != event.ContentBlock.Name {
				return fmt.Errorf("claude tool_use %q changed name from %q to %q", event.ContentBlock.ID, previous, event.ContentBlock.Name)
			}
			p.tools[event.ContentBlock.ID] = event.ContentBlock.Name
		}
	case "content_block_stop":
		if p.activeBlock != nil && p.activeBlock.messageID == p.currentMessageID && p.activeBlock.index == event.Index {
			p.activeBlock = nil
		}
	case "message_stop":
		p.activeBlock = nil
		// A stream that stops without final usage ended here, even if a tool runs before the next call.
		if !subagent && p.openCall != nil && p.openCall.End.IsZero() {
			p.openCall.End = p.now()
		}
	}
	return nil
}

func (p *parser) parseAssistant(raw json.RawMessage, parent, requestID string, lineTime time.Time, emit func(Event) error) error {
	var message struct {
		ID      string `json:"id"`
		Content []struct {
			Type  string         `json:"type"`
			Text  string         `json:"text"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		} `json:"content"`

		Model      string      `json:"model"`
		StopReason string      `json:"stop_reason"`
		Usage      claudeUsage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &message); err != nil {
		return fmt.Errorf("decode Claude assistant message: %w", err)
	}
	if message.ID != "" {
		p.currentMessageID = message.ID
	}
	_, streamed := p.streamedMessages[message.ID]
	// request_id is only on the assistant envelope, which arrives before the stream's message_delta.
	if streamed && p.openCall != nil && p.openCall.ResponseID == message.ID {
		p.openCall.RequestID = cmp.Or(p.openCall.RequestID, requestID)
	}
	// Claude Code writes "<synthetic>" messages itself, without a model call.
	unstreamed := message.ID != "" && !streamed && message.Model != "<synthetic>"
	if unstreamed {
		// Claude Code sends a main-thread call all at once only when streaming failed, so its
		// token count is final. A subagent's count can still grow, so it stays partial.
		call := runtime.ModelCall{ResponseModel: message.Model, ResponseID: message.ID, RequestID: requestID,
			StopReason: claudeFinishReason(message.StopReason), UsagePartial: parent != ""}
		message.Usage.apply(&call)
		// A main-thread stream cut off without an end is retried without streaming.
		if parent == "" && p.openCall != nil {
			p.retry()
		}
		if err := p.holdSnapshot(parent, message.ID, call, lineTime, emit); err != nil {
			return err
		}
		if parent == "" && p.retried != nil {
			held := p.snapshots[parent]
			held.retried, p.retried = p.retried, nil
			p.snapshots[parent] = held
		}
	}
	for i, content := range message.Content {
		key := p.blockKey(i)
		// Claude Code emits an assistant envelope for the currently open content
		// block. See https://code.claude.com/docs/en/agent-sdk/streaming-output
		if len(message.Content) == 1 && p.activeBlock != nil && p.activeBlock.messageID == p.currentMessageID {
			key = p.blockKey(p.activeBlock.index)
		}
		switch content.Type {
		case "text":
			previous := p.emitted[key]
			if previous == "" {
				p.emitted[key] = content.Text
				if content.Text != "" {
					if err := emit(Event{Kind: EventTextDelta, Text: content.Text}); err != nil {
						return err
					}
				}
			} else if len(content.Text) > len(previous) && content.Text[:len(previous)] == previous {
				suffix := content.Text[len(previous):]
				p.emitted[key] = content.Text
				if suffix != "" {
					if err := emit(Event{Kind: EventTextDelta, Text: suffix}); err != nil {
						return err
					}
				}
			}
		case "tool_use":
			if content.ID == "" || content.Name == "" {
				return fmt.Errorf("claude assistant tool_use requires an id and name")
			}
			if previous := p.tools[content.ID]; previous != "" && previous != content.Name {
				return fmt.Errorf("claude tool_use %q changed name from %q to %q", content.ID, previous, content.Name)
			}
			p.tools[content.ID] = content.Name
			if _, emitted := p.emittedToolCalls[content.ID]; emitted {
				continue
			}
			p.emittedToolCalls[content.ID] = struct{}{}
			// A tool runs once its block is whole, so a reply that never gets final usage ends here.
			if parent == "" && p.openCall != nil {
				p.openCallToolRequested = true
				if p.openCall.End.IsZero() {
					p.openCall.End = p.now()
				}
			}
			if err := emit(Event{Kind: EventToolActivity, ToolID: content.ID, ToolName: content.Name, ToolPhase: "started", Metadata: content.Input, ParentToolID: parent}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *parser) parseUser(ctx context.Context, raw, toolUseResult json.RawMessage, lineTime time.Time, emit func(Event) error) error {
	var message struct {
		Content []struct {
			Type      string          `json:"type"`
			ToolUseID string          `json:"tool_use_id"`
			Content   json.RawMessage `json:"content"`
			IsError   bool            `json:"is_error"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &message); err != nil {
		return fmt.Errorf("decode Claude user message: %w", err)
	}
	for _, content := range message.Content {
		if content.Type != "tool_result" {
			continue
		}
		name := p.tools[content.ToolUseID]
		if content.ToolUseID == "" || name == "" {
			return fmt.Errorf("claude tool_result references unknown tool_use id %q", content.ToolUseID)
		}
		if _, emitted := p.emittedResults[content.ToolUseID]; emitted {
			return fmt.Errorf("claude tool_result for %q was emitted more than once", content.ToolUseID)
		}
		var result any
		if len(content.Content) != 0 && string(content.Content) != "null" {
			if err := json.Unmarshal(content.Content, &result); err != nil {
				return fmt.Errorf("decode Claude tool_result %q content: %w", content.ToolUseID, err)
			}
		}
		p.emittedResults[content.ToolUseID] = struct{}{}
		// Other tools, such as MCP tools, may report their own usage here.
		if name == agentToolName {
			if err := p.endSubagent(ctx, content.ToolUseID, toolUseResult, lineTime, emit); err != nil {
				return err
			}
		}
		// tool_use_result describes a single tool result, so it must not count twice.
		toolUseResult = nil
		if err := emit(Event{
			Kind: EventToolActivity, ToolID: content.ToolUseID, ToolName: name,
			ToolPhase: "completed", ToolResult: result, ToolError: content.IsError,
		}); err != nil {
			return err
		}
	}
	return nil
}

// lineTime is when Claude Code wrote the line, or when it arrived if it has no valid timestamp.
func (p *parser) lineTime(timestamp string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, timestamp); err == nil {
		return t
	}
	return p.now()
}

// holdSnapshot keeps an unstreamed call until a newer message replaces it, so it carries the latest usage.
// Subagent calls arrive only as assistant snapshots whose usage predates the final delta.
func (p *parser) holdSnapshot(parent, messageID string, call runtime.ModelCall, lineTime time.Time, emit func(Event) error) error {
	// Claude Code reports an unstreamed call only when it ends, so the call starts at its thread's previous line.
	call.ParentToolID, call.Start, call.End = parent, cmp.Or(p.callStart[parent], lineTime), lineTime
	p.callStart[parent] = lineTime
	var retried *runtime.ModelCall
	if pending, ok := p.snapshots[parent]; ok && pending.messageID == messageID {
		call.Start, retried = pending.call.Start, pending.retried
	} else if ok {
		addRetries(&pending.call, pending.retried)
		if err := emit(Event{Kind: EventModelCall, ModelCall: &pending.call}); err != nil {
			return err
		}
	}
	p.snapshots[parent] = snapshotCall{messageID: messageID, call: call, retried: retried}
	return nil
}

// endSubagent emits the subagent's held call. tool_use_result.usage is the exact usage of its final call.
func (p *parser) endSubagent(ctx context.Context, parent string, toolUseResult json.RawMessage, lineTime time.Time, emit func(Event) error) error {
	var result struct {
		ResolvedModel string       `json:"resolvedModel"`
		Usage         *claudeUsage `json:"usage"`
	}
	// A background Agent reports text here, which carries no usage.
	if err := json.Unmarshal(toolUseResult, &result); err != nil {
		if bytes.HasPrefix(bytes.TrimSpace(toolUseResult), []byte("{")) {
			logging.FromContext(ctx).WarnContext(ctx, "failed to decode Claude subagent usage", "error", err)
		}
		result.Usage = nil
	}
	pending, held := p.snapshots[parent]
	delete(p.snapshots, parent)
	if result.Usage == nil {
		if !held {
			return nil
		}
		return emit(Event{Kind: EventModelCall, ModelCall: &pending.call})
	}
	final := runtime.ModelCall{ResponseModel: result.ResolvedModel, ParentToolID: parent, Start: cmp.Or(p.callStart[parent], lineTime), End: lineTime}
	delete(p.callStart, parent)
	result.Usage.apply(&final)
	if held {
		// The held snapshot is the final call itself when its input matches. tool_use_result has no message ID
		// (agentId names the whole subagent), so an earlier call with identical input merges into the final one.
		if pending.call.InputTokens == final.InputTokens && pending.call.CacheReadTokens == final.CacheReadTokens {
			final.Start, final.ResponseID, final.RequestID = pending.call.Start, pending.call.ResponseID, pending.call.RequestID
		} else if err := emit(Event{Kind: EventModelCall, ModelCall: &pending.call}); err != nil {
			return err
		}
	}
	return emit(Event{Kind: EventModelCall, ModelCall: &final})
}

func (p *parser) blockKey(index int) string {
	return p.currentMessageID + ":" + strconv.Itoa(index)
}

// claudeFinishReason maps an Anthropic stop_reason onto the GenAI finish reasons;
// a reason the conventions do not list keeps its own name.
func claudeFinishReason(stopReason string) string {
	switch stopReason {
	case "end_turn", "stop_sequence":
		return tracing.FinishReasonStop
	case "tool_use":
		return tracing.FinishReasonToolCall
	case "max_tokens", "model_context_window_exceeded":
		return tracing.FinishReasonLength
	case "refusal":
		return tracing.FinishReasonContentFilter
	}
	return stopReason
}
