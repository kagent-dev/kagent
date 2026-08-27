package driver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

type parser struct {
	emitted          map[string]string
	currentMessageID string
	tools            map[string]string
	emittedToolCalls map[string]struct{}
	emittedResults   map[string]struct{}
	terminal         bool
}

func ParseJSONL(r io.Reader, maxEventBytes int, emit func(Event) error) error {
	if maxEventBytes <= 0 {
		return fmt.Errorf("max event bytes must be positive")
	}
	p := parser{
		emitted: map[string]string{}, tools: map[string]string{},
		emittedToolCalls: map[string]struct{}{}, emittedResults: map[string]struct{}{},
	}
	reader := bufio.NewReaderSize(r, min(maxEventBytes+1, 64*1024))
	for {
		line, err := readBoundedLine(reader, maxEventBytes)
		if len(bytes.TrimSpace(line)) > 0 {
			if parseErr := p.parseLine(line, emit); parseErr != nil {
				return parseErr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read Claude event: %w", err)
		}
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

func (p *parser) parseLine(line []byte, emit func(Event) error) error {
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
		return p.parseStreamEvent(envelope.Event, emit)
	case "assistant":
		return p.parseAssistant(envelope.Message, emit)
	case "user":
		return p.parseUser(envelope.Message, emit)
	case "result":
		if envelope.Origin.Kind == "task-notification" {
			return nil
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

func (p *parser) parseStreamEvent(raw json.RawMessage, emit func(Event) error) error {
	var event struct {
		Type    string `json:"type"`
		Index   int    `json:"index"`
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
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
	case "content_block_delta":
		if event.Delta.Type == "text_delta" && event.Delta.Text != "" {
			key := p.blockKey(event.Index)
			p.emitted[key] += event.Delta.Text
			return emit(Event{Kind: EventTextDelta, Text: event.Delta.Text})
		}
	case "content_block_start":
		if event.ContentBlock.Type == "tool_use" {
			if event.ContentBlock.ID == "" || event.ContentBlock.Name == "" {
				return fmt.Errorf("claude tool_use start requires an id and name")
			}
			if previous := p.tools[event.ContentBlock.ID]; previous != "" && previous != event.ContentBlock.Name {
				return fmt.Errorf("claude tool_use %q changed name from %q to %q", event.ContentBlock.ID, previous, event.ContentBlock.Name)
			}
			p.tools[event.ContentBlock.ID] = event.ContentBlock.Name
		}
	}
	return nil
}

func (p *parser) parseAssistant(raw json.RawMessage, emit func(Event) error) error {
	var message struct {
		ID      string `json:"id"`
		Content []struct {
			Type  string         `json:"type"`
			Text  string         `json:"text"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &message); err != nil {
		return fmt.Errorf("decode Claude assistant message: %w", err)
	}
	if message.ID != "" {
		p.currentMessageID = message.ID
	}
	for i, content := range message.Content {
		key := p.blockKey(i)
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

func (p *parser) parseUser(raw json.RawMessage, emit func(Event) error) error {
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
		if err := emit(Event{
			Kind: EventToolActivity, ToolID: content.ToolUseID, ToolName: name,
			ToolPhase: "completed", ToolResult: result, ToolError: content.IsError,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (p *parser) blockKey(index int) string {
	return p.currentMessageID + ":" + strconv.Itoa(index)
}
