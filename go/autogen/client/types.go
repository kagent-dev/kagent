package client

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/kagent-dev/kagent/go/autogen/api"
)

type BaseObject struct {
	Component *api.Component `json:"component"`
	CreatedAt string         `json:"created_at,omitempty"`
	UpdatedAt string         `json:"updated_at,omitempty"`
	UserID    string         `json:"user_id"`
	Version   string         `json:"version,omitempty"`
	Id        int            `json:"id,omitempty"`
}

type Team struct {
	BaseObject
	Component *api.Component `json:"component"`
}

type Tool struct {
	BaseObject
	Component *api.Component `json:"component"`
	ServerID  *int           `json:"server_id,omitempty"`
}

type StdioMcpServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

type SseMcpServerConfig struct {
	URL            string                 `json:"url"`
	Headers        map[string]interface{} `json:"headers,omitempty"`
	Timeout        *int                   `json:"timeout,omitempty"`
	SseReadTimeout *int                   `json:"sse_read_timeout,omitempty"`
}

type ToolServer struct {
	Id            int           `json:"id,omitempty"`
	Component     api.Component `json:"component"`
	CreatedAt     string        `json:"created_at,omitempty"`
	UpdatedAt     string        `json:"updated_at,omitempty"`
	UserID        string        `json:"user_id,omitempty"`
	LastConnected string        `json:"last_connected,omitempty"`
	Version       string        `json:"version,omitempty"`
}

type ModelsUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func (m *ModelsUsage) Add(other *ModelsUsage) {
	if other == nil {
		return
	}
	m.PromptTokens += other.PromptTokens
	m.CompletionTokens += other.CompletionTokens
}

func (m *ModelsUsage) String() string {
	return fmt.Sprintf("Prompt Tokens: %d, Completion Tokens: %d", m.PromptTokens, m.CompletionTokens)
}

type TaskMessageMap map[string]interface{}

type RunMessage struct {
	CreatedAt   *string                `json:"created_at,omitempty"`
	UpdatedAt   *string                `json:"updated_at,omitempty"`
	Version     *string                `json:"version,omitempty"`
	SessionID   int                    `json:"session_id"`
	MessageMeta map[string]interface{} `json:"message_meta"`
	ID          int                    `json:"id"`
	UserID      *string                `json:"user_id"`
	Config      map[string]interface{} `json:"config"`
	RunID       int                    `json:"run_id"`
}

type CreateRunRequest struct {
	SessionID int    `json:"session_id"`
	UserID    string `json:"user_id"`
}

type CreateRunResult struct {
	ID int `json:"run_id"`
}

type SessionRuns struct {
	Runs []Run `json:"runs"`
}

type Run struct {
	ID           int           `json:"id"`
	SessionID    int           `json:"session_id"`
	CreatedAt    string        `json:"created_at"`
	Status       string        `json:"status"`
	Task         Task          `json:"task"`
	TeamResult   TeamResult    `json:"team_result"`
	Messages     []*RunMessage `json:"messages"`
	ErrorMessage string        `json:"error_message"`
}

type Task struct {
	Source      string      `json:"source"`
	Content     interface{} `json:"content"`
	MessageType string      `json:"message_type"`
}

type TeamResult struct {
	TaskResult TaskResult `json:"task_result"`
	Usage      string     `json:"usage"`
	Duration   float64    `json:"duration"`
}

type TaskResult struct {
	Messages   []TaskMessageMap `json:"messages"`
	StopReason string           `json:"stop_reason"`
}

// APIResponse is the common response wrapper for all API responses
type APIResponse struct {
	Status  bool        `json:"status"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

type Session struct {
	ID        int    `json:"id"`
	UserID    string `json:"user_id"`
	Version   string `json:"version"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	TeamID    *int   `json:"team_id"`
}

type CreateSession struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
	TeamID *int   `json:"team_id"`
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

// InvokeRequest represents the request payload for session invocation
type InvokeRequest struct {
	Task       string         `json:"task"`
	TeamConfig *api.Component `json:"team_config"`
}

var (
	NotFoundError = errors.New("not found")
)

func streamSseResponse(r io.ReadCloser) chan *SseEvent {
	scanner := bufio.NewScanner(r)
	ch := make(chan *SseEvent)
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

// FeedbackIssueType represents the category of feedback issue
type FeedbackIssueType string

const (
	FeedbackIssueTypeInstructions FeedbackIssueType = "instructions" // Did not follow instructions
	FeedbackIssueTypeFactual      FeedbackIssueType = "factual"      // Not factually correct
	FeedbackIssueTypeIncomplete   FeedbackIssueType = "incomplete"   // Incomplete response
	FeedbackIssueTypeTool         FeedbackIssueType = "tool"         // Should have run the tool
)

// FeedbackSubmission defines the request payload for submitting feedback
// and also serves as the response object when listing feedback.
type FeedbackSubmission struct {
	// ID is the unique identifier for the feedback, present in responses.
	ID int `json:"id,omitempty"`
	// CreatedAt is the timestamp of feedback creation, present in responses.
	CreatedAt string `json:"created_at,omitempty"`
	// UpdatedAt is the timestamp of the last update, present in responses.
	UpdatedAt string `json:"updated_at,omitempty"`
	// Version of the feedback object, present in responses.
	Version string `json:"version,omitempty"`

	// UserID is the identifier for the user submitting the feedback.
	UserID string `json:"user_id,omitempty"`
	// IsPositive indicates if the feedback is positive. Required for request.
	IsPositive bool `json:"is_positive"`
	// FeedbackText is the textual content of the feedback. Required for request.
	FeedbackText string `json:"feedback_text"`
	// IssueType categorizes the feedback if it's negative. Optional.
	IssueType *FeedbackIssueType `json:"issue_type,omitempty"`
	// MessageID is the ID of the message this feedback pertains to.
	MessageID int `json:"message_id,omitempty"`
}
