package api

import (
	"encoding/json"
)

type Component struct {
	Provider         string                 `json:"provider"`
	ComponentType    string                 `json:"component_type"`
	Version          int                    `json:"version"`
	ComponentVersion int                    `json:"component_version"`
	Description      string                 `json:"description"`
	Label            string                 `json:"label"`
	Config           map[string]interface{} `json:"config"`
}

func (c *Component) ToConfig() (map[string]interface{}, error) {
	if c == nil {
		return nil, nil
	}

	return toConfig(c)
}

func MustToConfig(c ComponentConfig) map[string]interface{} {
	config, err := c.ToConfig()
	if err != nil {
		panic(err)
	}
	return config
}

func MustFromConfig(c ComponentConfig, config map[string]interface{}) {
	err := c.FromConfig(config)
	if err != nil {
		panic(err)
	}
}

type ComponentConfig interface {
	ToConfig() (map[string]interface{}, error)
	FromConfig(map[string]interface{}) error
}

func toConfig(c any) (map[string]interface{}, error) {
	byt, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}

	result := make(map[string]interface{})
	err = json.Unmarshal(byt, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func fromConfig(c any, config map[string]interface{}) error {
	byt, err := json.Marshal(config)
	if err != nil {
		return err
	}

	return json.Unmarshal(byt, c)
}

// FeedbackIssueType represents the category of feedback issue
type FeedbackIssueType string

const (
	FeedbackIssueTypeInstructions FeedbackIssueType = "instructions" // Did not follow instructions
	FeedbackIssueTypeFactual      FeedbackIssueType = "factual"      // Not factually correct
	FeedbackIssueTypeIncomplete   FeedbackIssueType = "incomplete"   // Incomplete response
	FeedbackIssueTypeTool         FeedbackIssueType = "tool"         // Should have run the tool
)

// FeedbackSubmissionRequest defines the request payload for submitting feedback
type FeedbackSubmissionRequest struct {
	// Whether the feedback is positive
	IsPositive bool `json:"isPositive"`

	// The feedback text provided by the user
	FeedbackText string `json:"feedbackText"`

	// The type of issue for negative feedback
	IssueType string `json:"issueType,omitempty"`

	// Content of the message that received feedback
	MessageContent string `json:"messageContent"`

	// Source of the message (agent name)
	MessageSource string `json:"messageSource"`

	// Contents of messages preceding the feedback
	PrecedingMessagesContents []string `json:"precedingMessagesContents,omitempty"`

	// Session information
	SessionInfo string `json:"sessionInfo,omitempty"`

	// Timestamp of the feedback submission
	Timestamp string `json:"timestamp,omitempty"`

	// Client information
	ClientInfo map[string]interface{} `json:"clientInfo,omitempty"`
}

// FeedbackSubmissionResponse defines the response payload for feedback submission
type FeedbackSubmissionResponse struct {
	// Whether the operation was successful
	Status bool `json:"status"`

	// A message describing the result of the operation
	Message string `json:"message"`

	// Additional data related to the feedback submission
	Data map[string]interface{} `json:"data,omitempty"`
}
