package database

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
)

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

type Memory struct {
	ID          string          `json:"id"`
	AgentName   string          `json:"agent_name"`
	UserID      string          `json:"user_id"`
	Content     string          `json:"content"`
	Embedding   pgvector.Vector `json:"embedding"`
	Metadata    string          `json:"metadata"`
	CreatedAt   time.Time       `json:"created_at"`
	ExpiresAt   *time.Time      `json:"expires_at,omitempty"`
	AccessCount int64           `json:"access_count"`
}

// AgentMemorySearchResult is the result of a vector similarity search over Memory.
type AgentMemorySearchResult struct {
	Memory
	Score float64 `json:"score"`
}

type AgentTemplateHarnessPair struct {
	Namespace           string
	AgentTemplateName   string
	AgentTemplateUID    string
	HarnessName         string
	HarnessUID          string
	DesiredRevision     string
	AgentTemplateLabels map[string]string
}

type RuntimeRevision struct {
	Revision              string
	Namespace             string
	AgentTemplateName     string
	AgentTemplateUID      string
	HarnessName           string
	HarnessUID            string
	SourceSnapshot        json.RawMessage
	AgentCard             json.RawMessage
	EgressDestinations    []string
	ActorTemplateAtespace string
	ActorTemplateName     string
	ActorTemplateUID      string
}

type ActorTemplateHarness struct {
	Atespace    string
	Name        string
	UID         string
	HarnessName string
}

// AgentInstanceQuery narrows a page of AgentInstances. Zero values mean "do not
// filter on this", so an empty query lists the caller's own instances in the
// namespace.
type AgentInstanceQuery struct {
	Namespace   string
	UserID      string
	AllUsers    bool
	MatchLabels map[string]string
	// AgentTemplate and Harness name the agent whose conversations are wanted.
	// They are matched against the (AgentTemplate, Harness) pair the instance's
	// prepared revision was built from, not against its labels, so they select
	// instances stored before either field existed.
	AgentTemplate string
	Harness       string
	AfterID       string
	Limit         int
}

type AgentInstanceShare struct {
	ID         uuid.UUID
	Namespace  string
	InstanceID uuid.UUID
	Permission string
	TokenHash  []byte
	CreatedAt  time.Time
	// OwnerUserID is the user the shared AgentInstance belongs to.
	//
	// Populated only by the token lookup, which joins it in — that is what the
	// share grants. A visitor is authenticated as themselves and the token widens
	// what their account may reach to what the *owner* can see, so the instance
	// read has to run as the owner or it finds nothing.
	OwnerUserID string
}

// AgentInstanceTaskSnapshot identifies the immutable Substrate snapshot at a
// completed A2A turn boundary.
type AgentInstanceTaskSnapshot struct {
	Atespace     string
	Name         string
	UID          string
	ContentScope string
}

type AgentInstanceCheckpoint struct {
	ID                   uuid.UUID
	Namespace            string
	SourceInstanceID     uuid.UUID
	SourceContextID      uuid.UUID
	UserID               string
	RequestID            string
	HeadTaskID           string
	HistorySequence      int64
	SnapshotAtespace     string
	SnapshotName         string
	SnapshotUID          string
	SnapshotContentScope string
	PreparedRevision     string
	TagUID               string
	State                string
	Failure              string
	CreatedAt            time.Time
}
