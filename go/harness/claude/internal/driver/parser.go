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
	"time"

	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

type parser struct {
	emitted          map[string]string
	currentMessageID string
	activeBlock      *contentBlockRef
	tools            map[string]string
	emittedToolCalls map[string]struct{}
	emittedResults   map[string]struct{}
	terminal         bool
	now              func() time.Time
	openCall         *runtime.ModelCall
	openUsage        claudeUsage
	streamedMessages map[string]struct{}
	// snapshots holds each subagent's latest unstreamed call, keyed by parent_tool_use_id.
	snapshots map[string]snapshotCall
}

const agentToolName = "Agent"

type snapshotCall struct {
	messageID string
	call      runtime.ModelCall
}

type claudeUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
}

func (u claudeUsage) apply(call *runtime.ModelCall) {
	call.InputTokens = u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
	call.CacheReadTokens, call.CacheWriteTokens = u.CacheReadInputTokens, u.CacheCreationInputTokens
	call.OutputTokens = u.OutputTokens
}

// merge overlays the non-zero fields of a message_delta usage on its message_start usage.
func (u claudeUsage) merge(delta claudeUsage) claudeUsage {
	u.InputTokens = cmp.Or(delta.InputTokens, u.InputTokens)
	u.CacheCreationInputTokens = cmp.Or(delta.CacheCreationInputTokens, u.CacheCreationInputTokens)
	u.CacheReadInputTokens = cmp.Or(delta.CacheReadInputTokens, u.CacheReadInputTokens)
	u.OutputTokens = cmp.Or(delta.OutputTokens, u.OutputTokens)
	return u
}

func newParser(now func() time.Time) *parser {
	return &parser{
		emitted: map[string]string{}, tools: map[string]string{},
		emittedToolCalls: map[string]struct{}{}, emittedResults: map[string]struct{}{},
		now: now, streamedMessages: map[string]struct{}{}, snapshots: map[string]snapshotCall{},
	}
}

// flushOpenCall emits a started call that never received final usage.
func (p *parser) flushOpenCall(emit func(Event) error) error {
	if p.openCall == nil {
		return nil
	}
	call := p.openCall
	p.openCall = nil
	call.End, call.UsagePartial = p.now(), true
	// The conventions report a generation that ended without a finish reason as "error".
	call.StopReason = tracing.FinishReasonError
	return emit(Event{Kind: EventModelCall, ModelCall: call})
}

// flushCalls emits every model call still held when the stream ends.
func (p *parser) flushCalls(emit func(Event) error) error {
	if err := p.flushOpenCall(emit); err != nil {
		return err
	}
	return p.flushSnapshots(emit)
}

// flushSnapshots emits buffered calls of subagents that never reported their final usage.
func (p *parser) flushSnapshots(emit func(Event) error) error {
	for _, parent := range slices.Sorted(maps.Keys(p.snapshots)) {
		pending := p.snapshots[parent]
		delete(p.snapshots, parent)
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

// ParseJSONL parses a JSONL stream of Claude events and emits them to the
// provided event sink.
func ParseJSONL(ctx context.Context, r io.Reader, maxEventBytes int, emit func(Event) error) error {
	if maxEventBytes <= 0 {
		return fmt.Errorf("max event bytes must be positive")
	}
	p := newParser(time.Now)
	reader := bufio.NewReaderSize(r, min(maxEventBytes+1, 64*1024))
	for {
		line, err := readBoundedLine(reader, maxEventBytes)
		if len(bytes.TrimSpace(line)) > 0 {
			if parseErr := p.parseLine(ctx, line, emit); parseErr != nil {
				return errors.Join(parseErr, p.flushCalls(emit))
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			// A closed pipe is how an abandoned process ends, and its open call still happened.
			return errors.Join(fmt.Errorf("read Claude event: %w", err), p.flushCalls(emit))
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
		SessionID string          `json:"session_id"`
		IsError   bool            `json:"is_error"`
		Result    string          `json:"result"`
		Event     json.RawMessage `json:"event"`
		Message   json.RawMessage `json:"message"`
		Origin    struct {
			Kind string `json:"kind"`
		} `json:"origin"`

		ParentToolUseID string          `json:"parent_tool_use_id"`
		ToolUseResult   json.RawMessage `json:"tool_use_result"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return fmt.Errorf("decode Claude event: %w", err)
	}
	switch envelope.Type {
	case "system":
		if envelope.Subtype == "init" && envelope.SessionID != "" {
			return emit(Event{Kind: EventSessionStarted, SessionID: envelope.SessionID})
		}
	case "stream_event":
		return p.parseStreamEvent(envelope.Event, envelope.ParentToolUseID != "", emit)
	case "assistant":
		return p.parseAssistant(envelope.Message, envelope.ParentToolUseID, emit)
	case "user":
		return p.parseUser(ctx, envelope.Message, envelope.ToolUseResult, emit)
	case "result":
		if envelope.Origin.Kind == "task-notification" {
			return nil
		}
		if err := p.flushCalls(emit); err != nil {
			return err
		}
		p.terminal = true
		if envelope.IsError || envelope.Subtype != "success" {
			message := envelope.Result
			if message == "" {
				message = "Claude execution failed"
			}
			return emit(Event{Kind: EventFailed, Category: envelope.Subtype, SafeMessage: message})
		}
		return emit(Event{Kind: EventCompleted, SessionID: envelope.SessionID, Result: envelope.Result})
	}
	return nil
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
		if err := p.flushOpenCall(emit); err != nil {
			return err
		}
		p.streamedMessages[event.Message.ID] = struct{}{}
		p.openCall = &runtime.ModelCall{ResponseModel: event.Message.Model, Start: p.now()}
		p.openUsage = event.Message.Usage
		event.Message.Usage.apply(p.openCall)
	case "message_delta":
		if subagent || p.openCall == nil {
			return nil
		}
		call := p.openCall
		p.openCall = nil
		p.openUsage.merge(event.Usage).apply(call)
		call.StopReason, call.End = event.Delta.StopReason, p.now()
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
	}
	return nil
}

func (p *parser) parseAssistant(raw json.RawMessage, parent string, emit func(Event) error) error {
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
	// Claude Code writes "<synthetic>" messages itself, without a model call.
	unstreamed := message.ID != "" && !streamed && message.Model != "<synthetic>"
	if unstreamed {
		call := runtime.ModelCall{ResponseModel: message.Model, StopReason: message.StopReason, UsagePartial: true}
		message.Usage.apply(&call)
		if err := p.holdSnapshot(parent, message.ID, call, emit); err != nil {
			return err
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
			if err := emit(Event{Kind: EventToolActivity, ToolID: content.ID, ToolName: content.Name, ToolPhase: "started", Metadata: content.Input}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *parser) parseUser(ctx context.Context, raw, toolUseResult json.RawMessage, emit func(Event) error) error {
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
			if err := p.endSubagent(ctx, content.ToolUseID, toolUseResult, emit); err != nil {
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

// holdSnapshot keeps a subagent call until a newer message replaces it, so it carries the latest usage.
// Subagent calls arrive only as assistant snapshots whose usage predates the final delta.
func (p *parser) holdSnapshot(parent, messageID string, call runtime.ModelCall, emit func(Event) error) error {
	// Subagent calls carry no timing, so the span runs from the first to the last snapshot of one message.
	call.Start = p.now()
	call.End = call.Start
	if pending, ok := p.snapshots[parent]; ok && pending.messageID == messageID {
		call.Start = pending.call.Start
	} else if ok {
		if err := emit(Event{Kind: EventModelCall, ModelCall: &pending.call}); err != nil {
			return err
		}
	}
	p.snapshots[parent] = snapshotCall{messageID: messageID, call: call}
	return nil
}

// endSubagent emits the subagent's held call. tool_use_result.usage is the exact usage of its final call.
func (p *parser) endSubagent(ctx context.Context, parent string, toolUseResult json.RawMessage, emit func(Event) error) error {
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
	final := runtime.ModelCall{ResponseModel: result.ResolvedModel, Start: p.now()}
	final.End = final.Start
	result.Usage.apply(&final)
	if held {
		// The held snapshot is the final call itself when its input matches. tool_use_result has no message ID
		// (agentId names the whole subagent), so an earlier call with identical input merges into the final one.
		if pending.call.InputTokens == final.InputTokens && pending.call.CacheReadTokens == final.CacheReadTokens {
			final.Start = pending.call.Start
		} else if err := emit(Event{Kind: EventModelCall, ModelCall: &pending.call}); err != nil {
			return err
		}
	}
	return emit(Event{Kind: EventModelCall, ModelCall: &final})
}

func (p *parser) blockKey(index int) string {
	return p.currentMessageID + ":" + strconv.Itoa(index)
}
