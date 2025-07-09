package database

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/kagent-dev/kagent/go/internal/autogen/api"
	"gorm.io/gorm"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// JSONMap is a custom type for handling JSON columns in GORM
type JSONMap map[string]interface{}

// Scan implements the sql.Scanner interface
func (j *JSONMap) Scan(value interface{}) error {
	if value == nil {
		*j = make(JSONMap)
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("failed to scan JSONMap: value is not []byte")
	}

	return json.Unmarshal(bytes, j)
}

// Value implements the driver.Valuer interface
func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// Agent represents an agent configuration
type Agent struct {
	gorm.Model
	Name      string        `gorm:"unique;not null" json:"name"`
	Component api.Component `gorm:"type:json;not null" json:"component"`
}

type Message struct {
	ID        string         `gorm:"primaryKey;not null" json:"id"`
	UserID    string         `gorm:"primaryKey;not null" json:"user_id"`
	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`

	Data      string  `gorm:"type:text;not null" json:"data"` // JSON serialized protocol.Message
	SessionID *string `gorm:"index" json:"session_id"`
	TaskID    *string `gorm:"index" json:"task_id"`
}

func (m *Message) Parse() (protocol.Message, error) {
	var data protocol.Message
	err := json.Unmarshal([]byte(m.Data), &data)
	if err != nil {
		return protocol.Message{}, err
	}
	return data, nil
}

func ParseMessages(messages []Message) ([]protocol.Message, error) {
	result := make([]protocol.Message, 0, len(messages))
	for _, message := range messages {
		parsedMessage, err := message.Parse()
		if err != nil {
			return nil, err
		}
		result = append(result, parsedMessage)
	}
	return result, nil
}

type Session struct {
	ID        string         `gorm:"primaryKey;not null" json:"id"`
	Name      string         `gorm:"index;not null" json:"name"`
	UserID    string         `gorm:"primaryKey" json:"user_id"`
	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`

	AgentID *uint `gorm:"index" json:"agent_id"`
}

type Task struct {
	ID        string         `gorm:"primaryKey;not null" json:"id"`
	Name      *string        `gorm:"index" json:"name,omitempty"`
	UserID    string         `gorm:"primaryKey" json:"user_id"`
	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Data      string         `gorm:"type:text;not null" json:"data"` // JSON serialized task data
	SessionID *string        `gorm:"index" json:"session_id"`
}

func (t *Task) Parse() (protocol.Task, error) {
	var data protocol.Task
	err := json.Unmarshal([]byte(t.Data), &data)
	if err != nil {
		return protocol.Task{}, err
	}
	return data, nil
}

type PushNotification struct {
	gorm.Model
	TaskID string `gorm:"not null;index" json:"task_id"`
	Data   string `gorm:"type:text;not null" json:"data"` // JSON serialized push notification config
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
	gorm.Model
	Name       string        `gorm:"index;unique;not null" json:"name"`
	Component  api.Component `gorm:"type:json;not null" json:"component"`
	ServerName string        `gorm:"not null;index" json:"server_name,omitempty"`
}

// ToolServer represents a tool server that provides tools
type ToolServer struct {
	gorm.Model
	Name          string        `gorm:"primaryKey;not null" json:"name"`
	LastConnected *time.Time    `json:"last_connected,omitempty"`
	Component     api.Component `gorm:"type:json;not null" json:"component"`
}

// TableName methods to match Python table names
func (Agent) TableName() string            { return "agent" }
func (Message) TableName() string          { return "message" }
func (Session) TableName() string          { return "session" }
func (Task) TableName() string             { return "task" }
func (PushNotification) TableName() string { return "push_notification" }
func (Feedback) TableName() string         { return "feedback" }
func (Tool) TableName() string             { return "tool" }
func (ToolServer) TableName() string       { return "toolserver" }
