package client

import "github.com/kagent-dev/kagent/go/autogen/api"

type ValidationRequest struct {
	Component *api.Component `json:"component"`
}

type ValidationError struct {
	Field      string  `json:"field"`
	Error      string  `json:"error"`
	Suggestion *string `json:"suggestion,omitempty"`
}

type ValidationResponse struct {
	IsValid  bool               `json:"is_valid"`
	Errors   []*ValidationError `json:"errors"`
	Warnings []*ValidationError `json:"warnings"`
}

func (c *Client) Validate(req *ValidationRequest) (*ValidationResponse, error) {
	var resp ValidationResponse
	err := c.doRequest("POST", "/validate", req, &resp)
	return &resp, err
}
