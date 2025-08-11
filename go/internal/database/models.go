package database

import (
	"encoding/json"
	"time"

	"github.com/kagent-dev/kagent/go/internal/adk"
	"gorm.io/gorm"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// Agent represents an agent configuration
type Agent struct {
	ID        string         `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`

	Config *adk.AgentConfig `gorm:"type:json;not null" json:"config"`
}

type Event struct {
	ID        string         `gorm:"primaryKey;not null" json:"id"`
	SessionID string         `gorm:"index" json:"session_id"`
	UserID    string         `gorm:"primaryKey;not null" json:"user_id"`
	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`

	Data string `gorm:"type:text;not null" json:"data"` // JSON serialized protocol.Message
}

func (m *Event) Parse() (protocol.Message, error) {
	var data protocol.Message
	err := json.Unmarshal([]byte(m.Data), &data)
	if err != nil {
		return protocol.Message{}, err
	}
	return data, nil
}

func ParseMessages(messages []Event) ([]*protocol.Message, error) {
	result := make([]*protocol.Message, 0, len(messages))
	for _, message := range messages {
		parsedMessage, err := message.Parse()
		if err != nil {
			return nil, err
		}
		result = append(result, &parsedMessage)
	}
	return result, nil
}

type Session struct {
	ID        string         `gorm:"primaryKey;not null" json:"id"`
	Name      *string        `gorm:"index" json:"name,omitempty"`
	UserID    string         `gorm:"primaryKey" json:"user_id"`
	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`

	AgentID *string `gorm:"index" json:"agent_id"`
}

type Task struct {
	ID        string         `gorm:"primaryKey;not null" json:"id"`
	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Data      string         `gorm:"type:text;not null" json:"data"` // JSON serialized task data
	SessionID string         `gorm:"index" json:"session_id"`
}

func (t *Task) Parse() (protocol.Task, error) {
	var data protocol.Task
	err := json.Unmarshal([]byte(t.Data), &data)
	if err != nil {
		return protocol.Task{}, err
	}
	return data, nil
}

func ParseTasks(tasks []Task) ([]*protocol.Task, error) {
	result := make([]*protocol.Task, 0, len(tasks))
	for _, task := range tasks {
		parsedTask, err := task.Parse()
		if err != nil {
			return nil, err
		}
		result = append(result, &parsedTask)
	}
	return result, nil
}

type PushNotification struct {
	ID        string         `gorm:"primaryKey;not null" json:"id"`
	TaskID    string         `gorm:"not null;index" json:"task_id"`
	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Data      string         `gorm:"type:text;not null" json:"data"` // JSON serialized push notification config
}

// FeedbackIssueType represents the category of feedback issue
type FeedbackIssueType string

const (
	FeedbackIssueTypeInstructions FeedbackIssueType = "instructions" // Did not follow instructions
	FeedbackIssueTypeFactual      FeedbackIssueType = "factual"      // Not factually correct
	FeedbackIssueTypeIncomplete   FeedbackIssueType = "incomplete"   // Incomplete response
	FeedbackIssueTypeTool         FeedbackIssueType = "tool"         // Should have run the tool
)

// Feedback represents user feedback on agent responses
type Feedback struct {
	gorm.Model
	UserID       string             `gorm:"primaryKey;not null" json:"user_id"`
	MessageID    uint               `gorm:"index;constraint:OnDelete:CASCADE" json:"message_id"`
	IsPositive   bool               `gorm:"default:false" json:"is_positive"`
	FeedbackText string             `gorm:"not null" json:"feedback_text"`
	IssueType    *FeedbackIssueType `json:"issue_type,omitempty"`
}

// Tool represents a single tool that can be used by an agent
type Tool struct {
	ID          string         `gorm:"primaryKey;not null" json:"id"`
	ServerName  string         `gorm:"primaryKey;not null" json:"server_name"`
	CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Description string         `json:"description"`
}

// ToolServer represents a tool server that provides tools
type ToolServer struct {
	CreatedAt     time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Name          string         `gorm:"primaryKey;not null" json:"name"`
	GroupKind     string         `gorm:"primaryKey;not null" json:"group_kind"`
	Description   string         `json:"description"`
	LastConnected *time.Time     `json:"last_connected,omitempty"`
}

// TableName methods to match Python table names
func (Agent) TableName() string            { return "agent" }
func (Event) TableName() string            { return "event" }
func (Session) TableName() string          { return "session" }
func (Task) TableName() string             { return "task" }
func (PushNotification) TableName() string { return "push_notification" }
func (Feedback) TableName() string         { return "feedback" }
func (Tool) TableName() string             { return "tool" }
func (ToolServer) TableName() string       { return "toolserver" }
