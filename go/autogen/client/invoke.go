package client

import "github.com/kagent-dev/kagent/go/autogen/api"

type InvokeTaskRequest struct {
	Task       string         `json:"task"`
	TeamConfig *api.Component `json:"team_config"`
}

type InvokeTaskResult struct {
	Duration   float64 `json:"duration"`
	TaskResult struct {
		Messages []struct {
			Content  interface{} `json:"content"`
			Metadata struct {
			} `json:"metadata"`
			ModelsUsage *struct {
				CompletionTokens int `json:"completion_tokens"`
				PromptTokens     int `json:"prompt_tokens"`
			} `json:"models_usage"`
			Source string `json:"source"`
		} `json:"messages"`
		StopReason string `json:"stop_reason"`
	} `json:"task_result"`
	Usage string `json:"usage"`
}

func (c *Client) InvokeTask(req *InvokeTaskRequest) (*InvokeTaskResult, error) {
	var invoke InvokeTaskResult
	err := c.doRequest("POST", "/invoke", req, &invoke)
	return &invoke, err
}
