package client

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

type TaskResult struct {
	// These are all of type Event, but we don't want to unmarshal them here
	// because we want to handle them in the caller
	Messages   []json.RawMessage `json:"messages"`
	StopReason string            `json:"stop_reason"`
}

// APIResponse is the common response wrapper for all API responses
type APIResponse struct {
	Status  bool        `json:"status"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// ProviderModels maps provider names to a list of their supported model names.
type ProviderModels map[string][]ModelInfo

// ModelInfo holds details about a specific model.
type ModelInfo struct {
	Name            string `json:"name"`
	FunctionCalling bool   `json:"function_calling"`
}

type SseEvent struct {
	Event string `json:"event"`
	Data  []byte `json:"data"`
}

func (e *SseEvent) String() string {
	return fmt.Sprintf("event: %s\ndata: %s\n\n", e.Event, e.Data)
}

func streamSseResponse(r io.ReadCloser) chan *SseEvent {
	scanner := bufio.NewScanner(r)
	ch := make(chan *SseEvent, 10)
	go func() {
		defer close(ch)
		defer r.Close()
		currentEvent := &SseEvent{}
		for scanner.Scan() {
			line := scanner.Bytes()
			if bytes.HasPrefix(line, []byte("event:")) {
				currentEvent.Event = string(bytes.TrimPrefix(line, []byte("event:")))
			}
			if bytes.HasPrefix(line, []byte("data:")) {
				currentEvent.Data = bytes.TrimPrefix(line, []byte("data:"))
				ch <- currentEvent
				currentEvent = &SseEvent{}
			}
		}
	}()
	return ch
}
