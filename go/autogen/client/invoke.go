package client

import (
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
	ch := streamSseResponse(resp.Body)
	return ch, nil
}
