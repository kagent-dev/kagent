package database

import (
	"encoding/json"
	"time"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"

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
	AgentCard             *a2apb.AgentCard
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
// filter on this", so an empty query lists the caller's own instances.
type AgentInstanceQuery struct {
	UserID      string
	AllUsers    bool
	MatchLabels map[string]string
	// AgentTemplate and Harness name the agent whose conversations are wanted.
	// They are matched against the (AgentTemplate, Harness) pair the instance's
	// prepared revision was built from, not against its labels, so they select
	// instances stored before either field existed.
	AgentTemplate *apiv1alpha1.ResourceReference
	Harness       *apiv1alpha1.ResourceReference
	AfterID       string
	Limit         int
}

// AgentInstanceTaskSnapshot records the external snapshot at an A2A turn boundary.
// Only an explicit checkpoint retains a copy after the Actor advances or is deleted.
type AgentInstanceTaskSnapshot struct {
	Atespace     string
	URI          string
	ContentScope string
}
