package client

import (
	"fmt"

	"github.com/kagent-dev/kagent/go/autogen/api"
)

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

func (r ValidationResponse) ErrorMsg() string {
	var msg string
	for _, e := range r.Errors {
		msg += fmt.Sprintf("Error: %s\n [%s]\n", e.Error, e.Field)
		if e.Suggestion != nil {
			msg += fmt.Sprintf("Suggestion: %s\n", *e.Suggestion)
		}
	}
	for _, w := range r.Warnings {
		msg += fmt.Sprintf("Warning: %s\n [%s]\n", w.Error, w.Field)
		if w.Suggestion != nil {
			msg += fmt.Sprintf("Suggestion: %s\n", *w.Suggestion)
		}
	}

	return msg
}

func (c *client) Validate(req *ValidationRequest) (*ValidationResponse, error) {
	var resp ValidationResponse
	err := c.doRequest("POST", "/validate", req, &resp)
	return &resp, err
}
