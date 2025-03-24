package client

import (
	"fmt"

	"github.com/kagent-dev/kagent/go/autogen/api"
)

type Team struct {
	Component *api.Component `json:"component"`
	CreatedAt *string        `json:"created_at,omitempty"`
	UpdatedAt *string        `json:"updated_at,omitempty"`
	UserID    string         `json:"user_id"`
	Version   *string        `json:"version,omitempty"`
	Id        int            `json:"id,omitempty"`
}

type Tool struct {
	Id        *int          `json:"id,omitempty"`
	Component api.Component `json:"component"`
	CreatedAt *string       `json:"created_at,omitempty"`
	UpdatedAt *string       `json:"updated_at,omitempty"`
	UserID    *string       `json:"user_id,omitempty"`
	Version   *string       `json:"version,omitempty"`
}

type ToolServer struct {
	Id            *int          `json:"id,omitempty"`
	Component     api.Component `json:"component"`
	CreatedAt     *string       `json:"created_at,omitempty"`
	UpdatedAt     *string       `json:"updated_at,omitempty"`
	UserID        *string       `json:"user_id,omitempty"`
	LastConnected *string       `json:"last_connected,omitempty"`
	Version       *string       `json:"version,omitempty"`
}

type ModelsUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func (m *ModelsUsage) Add(other *ModelsUsage) {
	m.PromptTokens += other.PromptTokens
	m.CompletionTokens += other.CompletionTokens
}

func (m *ModelsUsage) String() string {
	return fmt.Sprintf("Prompt Tokens: %d, Completion Tokens: %d", m.PromptTokens, m.CompletionTokens)
}

type TaskMessage struct {
	Source      string       `json:"source"`
	ModelsUsage *ModelsUsage `json:"models_usage"`
	Content     string       `json:"content"`
	Type        string       `json:"type"`
}

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
	ID           string        `json:"id"`
	SessionID    int           `json:"session_id"`
	CreatedAt    string        `json:"created_at"`
	Status       string        `json:"status"`
	Task         Task          `json:"task"`
	TeamResult   TeamResult    `json:"team_result"`
	Messages     []*RunMessage `json:"messages"`
	ErrorMessage string        `json:"error_message"`
}

type Task struct {
	Source      string `json:"source"`
	Content     string `json:"content"`
	MessageType string `json:"message_type"`
}

type TeamResult struct {
	TaskResult TaskResult `json:"task_result"`
	Usage      string     `json:"usage"`
	Duration   float64    `json:"duration"`
}

type TaskResult struct {
	Messages   []TaskMessage `json:"messages"`
	StopReason string        `json:"stop_reason"`
}

// APIResponse is the common response wrapper for all API responses
type APIResponse struct {
	Status  bool        `json:"status"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

type Session struct {
	ID      int    `json:"id"`
	UserID  string `json:"user_id"`
	Version string `json:"version"`
	TeamID  int    `json:"team_id"`
	Name    string `json:"name"`
}

type CreateSession struct {
	UserID string `json:"user_id"`
	TeamID int    `json:"team_id"`
	Name   string `json:"name"`
}
