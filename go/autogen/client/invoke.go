package client

import (
	"bufio"
	"bytes"

	"github.com/kagent-dev/kagent/go/autogen/api"
)

type InvokeTaskRequest struct {
	Task       string         `json:"task"`
	TeamConfig *api.Component `json:"team_config"`
}

type InvokeTaskResult struct {
	Duration   float64    `json:"duration"`
	TaskResult TaskResult `json:"task_result"`
	Usage      string     `json:"usage"`
}

func (c *Client) InvokeTask(req *InvokeTaskRequest) (*InvokeTaskResult, error) {
	var invoke InvokeTaskResult
	err := c.doRequest("POST", "/invoke", req, &invoke)
	return &invoke, err
}

func (c *Client) InvokeTaskStream(req *InvokeTaskRequest) (<-chan *SseEvent, error) {
	resp, err := c.startRequest("POST", "/invoke/stream", req)
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(resp.Body)
	ch := make(chan *SseEvent)
	go func() {
		defer close(ch)
		currentEvent := &SseEvent{}
		for scanner.Scan() {
			line := scanner.Bytes()
			if bytes.Contains(line, []byte("event")) {
				currentEvent.Event = string(bytes.TrimPrefix(line, []byte("event:")))
			}
			if bytes.Contains(line, []byte("data")) {
				currentEvent.Data = bytes.TrimPrefix(line, []byte("data:"))
				ch <- currentEvent
				currentEvent = &SseEvent{}
			}
		}
	}()
	return ch, nil
}
