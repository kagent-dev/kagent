package database

import (
	"encoding/json"
	"time"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/pgvector/pgvector-go"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

type Agent struct {
	ID        string     `json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`

	Type   string           `json:"type"`
	Config *adk.AgentConfig `json:"config"`
}

type Event struct {
	ID        string     `json:"id"`
	SessionID string     `json:"session_id"`
	UserID    string     `json:"user_id"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`

	Data string `json:"data"` // JSON-serialized protocol.Message
}

func (m *Event) Parse() (protocol.Message, error) {
	var data protocol.Message
	if err := json.Unmarshal([]byte(m.Data), &data); err != nil {
		return protocol.Message{}, err
	}
	return data, nil
}

func ParseMessages(messages []Event) ([]*protocol.Message, error) {
	result := make([]*protocol.Message, 0, len(messages))
	for _, message := range messages {
		parsed, err := message.Parse()
		if err != nil {
			return nil, err
		}
		result = append(result, &parsed)
	}
	return result, nil
}

// SessionSource represents the origin of a session.
type SessionSource string

const (
	// SessionSourceUser indicates the session was initiated by a user.
	SessionSourceUser SessionSource = "user"
	// SessionSourceAgent indicates the session was created by a parent agent's A2A call.
	SessionSourceAgent SessionSource = "agent"
)

type Session struct {
	ID        string     `json:"id"`
	Name      *string    `json:"name,omitempty"`
	UserID    string     `json:"user_id"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`

	AgentID *string `json:"agent_id,omitempty"`
	// Source indicates how this session was created.
	// SessionSourceUser = user-initiated, SessionSourceAgent = created by a parent agent's A2A call.
	Source *SessionSource `json:"source,omitempty"`
}

type Task struct {
	ID        string     `json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	Data      string     `json:"data"` // JSON-serialized task data
	SessionID string     `json:"session_id"`
}

func (t *Task) Parse() (protocol.Task, error) {
	var data protocol.Task
	if err := json.Unmarshal([]byte(t.Data), &data); err != nil {
		return protocol.Task{}, err
	}
	return data, nil
}

func ParseTasks(tasks []Task) ([]*protocol.Task, error) {
	result := make([]*protocol.Task, 0, len(tasks))
	for _, task := range tasks {
		parsed, err := task.Parse()
		if err != nil {
			return nil, err
		}
		result = append(result, &parsed)
	}
	return result, nil
}

type PushNotification struct {
	ID        string     `json:"id"`
	TaskID    string     `json:"task_id"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	Data      string     `json:"data"` // JSON-serialized push notification config
}

// FeedbackIssueType represents the category of feedback issue
type FeedbackIssueType string

const (
	FeedbackIssueTypeInstructions FeedbackIssueType = "instructions"
	FeedbackIssueTypeFactual      FeedbackIssueType = "factual"
	FeedbackIssueTypeIncomplete   FeedbackIssueType = "incomplete"
	FeedbackIssueTypeTool         FeedbackIssueType = "tool"
)

type Feedback struct {
	ID           int64              `json:"id"`
	CreatedAt    *time.Time         `json:"created_at,omitempty"`
	UpdatedAt    *time.Time         `json:"updated_at,omitempty"`
	DeletedAt    *time.Time         `json:"deleted_at,omitempty"`
	UserID       string             `json:"user_id"`
	MessageID    *int64             `json:"message_id,omitempty"`
	IsPositive   bool               `json:"is_positive"`
	FeedbackText string             `json:"feedback_text"`
	IssueType    *FeedbackIssueType `json:"issue_type,omitempty"`
}

type Tool struct {
	ID          string     `json:"id"`
	ServerName  string     `json:"server_name"`
	GroupKind   string     `json:"group_kind"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
	Description string     `json:"description"`
}

type ToolServer struct {
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty"`
	Name          string     `json:"name"`
	GroupKind     string     `json:"group_kind"`
	Description   string     `json:"description"`
	LastConnected *time.Time `json:"last_connected,omitempty"`
}

type LangGraphCheckpoint struct {
	UserID             string     `json:"user_id"`
	ThreadID           string     `json:"thread_id"`
	CheckpointNS       string     `json:"checkpoint_ns"`
	CheckpointID       string     `json:"checkpoint_id"`
	ParentCheckpointID *string    `json:"parent_checkpoint_id,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	DeletedAt          *time.Time `json:"deleted_at,omitempty"`
	Metadata           string     `json:"metadata"`
	Checkpoint         string     `json:"checkpoint"`
	CheckpointType     string     `json:"checkpoint_type"`
	Version            int32      `json:"version"`
}

type LangGraphCheckpointWrite struct {
	UserID       string     `json:"user_id"`
	ThreadID     string     `json:"thread_id"`
	CheckpointNS string     `json:"checkpoint_ns"`
	CheckpointID string     `json:"checkpoint_id"`
	WriteIdx     int32      `json:"write_idx"`
	Value        string     `json:"value"`
	ValueType    string     `json:"value_type"`
	Channel      string     `json:"channel"`
	TaskID       string     `json:"task_id"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
}

type CrewAIAgentMemory struct {
	UserID     string     `json:"user_id"`
	ThreadID   string     `json:"thread_id"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	DeletedAt  *time.Time `json:"deleted_at,omitempty"`
	MemoryData string     `json:"memory_data"`
}

type CrewAIFlowState struct {
	UserID     string     `json:"user_id"`
	ThreadID   string     `json:"thread_id"`
	MethodName string     `json:"method_name"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	DeletedAt  *time.Time `json:"deleted_at,omitempty"`
	StateData  string     `json:"state_data"`
}

type Memory struct {
	ID          string          `json:"id"`
	AgentName   string          `json:"agent_name"`
	UserID      string          `json:"user_id"`
	Content     string          `json:"content"`
	Embedding   pgvector.Vector `json:"embedding"`
	Metadata    string          `json:"metadata"`
	CreatedAt   time.Time       `json:"created_at"`
	ExpiresAt   *time.Time      `json:"expires_at,omitempty"`
	AccessCount int32           `json:"access_count"`
}

// AgentMemorySearchResult is the result of a vector similarity search over Memory.
type AgentMemorySearchResult struct {
	Memory
	Score float64 `json:"score"`
}
