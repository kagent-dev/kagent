package database

import (
	"encoding/json"
	"time"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/kagent-dev/kagent/go/core/internal/egress"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
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

// AgentDefinition tracks the desired and last successful runtime for an Agent UID.
type AgentDefinition struct {
	Namespace       string
	AgentName       string
	AgentUID        string
	DesiredRevision string
}

type RuntimeRevision struct {
	Revision              string
	Namespace             string
	AgentName             string
	AgentUID              string
	SourceSnapshot        json.RawMessage
	AgentCard             *a2apb.AgentCard
	Credentials           []egress.Credential
	EgressDestinations    []string
	ActorTemplateAtespace string
	ActorTemplateName     string
	ActorTemplateUID      string
}

// SessionQuery narrows a page of sessions to an optional Agent.
type SessionQuery struct {
	UserID   string
	AllUsers bool
	Agent    *apiv1alpha1.ResourceReference
	AfterID  string
	Limit    int
}

// SessionTaskSnapshot records the external snapshot at an A2A turn boundary.
// Only an explicit checkpoint retains a copy after the Actor advances or is deleted.
type SessionTaskSnapshot struct {
	Atespace     string
	URI          string
	ContentScope string
}

// RuntimeArtifact identifies backend resources retained by either runtime kind.
// Agent configuration and SandboxTemplate provenance stay in their extensions.
type RuntimeArtifact struct {
	Revision              string
	Kind                  string
	Namespace             string
	ActorTemplateAtespace string
	ActorTemplateName     string
	ActorTemplateUID      string
	DeletedAt             *time.Time
}
