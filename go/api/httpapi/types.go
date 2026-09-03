package httpapi

import (
	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// Common types

func NewResponse[T any](data T, message string, error bool) StandardResponse[T] {
	return StandardResponse[T]{
		Error:   error,
		Data:    data,
		Message: message,
	}
}

// StandardResponse represents the standard response format used by many endpoints
type StandardResponse[T any] struct {
	Error   bool   `json:"error"`
	Data    T      `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
}

// Version represents the version information
type VersionResponse struct {
	KAgentVersion string `json:"kagent_version"`
	GitCommit     string `json:"git_commit"`
	BuildDate     string `json:"build_date"`
}

// ModelConfigResource is the HTTP response for a ModelConfig: ref + raw CRD spec/status.
type ModelConfigResource struct {
	Ref    string                     `json:"ref"`
	Spec   v1alpha3.ModelConfigSpec   `json:"spec"`
	Status v1alpha3.ModelConfigStatus `json:"status,omitempty"`
}

// SecretMaterial describes a Secret key/value pair to create or update alongside a ModelConfig.
type SecretMaterial struct {
	Name  string `json:"name"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

// CreateModelConfigRequest is a thin wrapper: ref + optional inline apiKey + full CRD spec.
type CreateModelConfigRequest struct {
	Ref string `json:"ref"`
	// APIKey is an optional inline API key to store in a generated Secret.
	APIKey string `json:"apiKey,omitempty"`
	// Secrets are optional companion Secrets to create or update alongside the ModelConfig.
	Secrets []SecretMaterial         `json:"secrets,omitempty"`
	Spec    v1alpha3.ModelConfigSpec `json:"spec"`
}

// UpdateModelConfigRequest is a thin wrapper: optional inline apiKey + full CRD spec.
type UpdateModelConfigRequest struct {
	APIKey  *string                  `json:"apiKey,omitempty"`
	Spec    v1alpha3.ModelConfigSpec `json:"spec"`
	Secrets []SecretMaterial         `json:"secrets,omitempty"`
}

// Session types

// SessionRequest represents a session creation/update request
type SessionRequest struct {
	AgentRef *string                 `json:"agent_ref,omitempty"`
	Name     *string                 `json:"name,omitempty"`
	ID       *string                 `json:"id,omitempty"`
	Source   *database.SessionSource `json:"source,omitempty"`
}

// Run represents a run from the database
type Task = database.Task

// Message represents a message from the database
type Message = database.Event

// Session represents a session from the database
type Session = database.Session

// Tool types

// Tool represents a tool from the database
type Tool = database.Tool

// ToolServer types

// ToolServerResponse represents a tool server response
type ToolServerResponse struct {
	Ref             string              `json:"ref"`
	GroupKind       string              `json:"groupKind"`
	DiscoveredTools []*v1alpha3.MCPTool `json:"discoveredTools"`
}

// Namespace types

// NamespaceResponse represents a namespace response
type NamespaceResponse struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Provider types

// ProviderInfo represents information about a provider
type ProviderInfo struct {
	Name           string   `json:"name"`
	Type           string   `json:"type"`
	RequiredParams []string `json:"requiredParams"`
	OptionalParams []string `json:"optionalParams"`
}
