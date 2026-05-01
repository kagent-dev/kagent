package httpapi

import (
	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Common types

// APIError represents an error response from the API
type APIError struct {
	Error string `json:"error"`
}

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

// Provider represents a provider configuration
type Provider struct {
	Name string `json:"name"`
	Type string `json:"type"`
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
	Spec   v1alpha2.ModelConfigSpec   `json:"spec"`
	Status v1alpha2.ModelConfigStatus `json:"status,omitempty"`
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
	Spec    v1alpha2.ModelConfigSpec `json:"spec"`
}

// UpdateModelConfigRequest is a thin wrapper: optional inline apiKey + full CRD spec.
type UpdateModelConfigRequest struct {
	APIKey  *string                  `json:"apiKey,omitempty"`
	Spec    v1alpha2.ModelConfigSpec `json:"spec"`
	Secrets []SecretMaterial         `json:"secrets,omitempty"`
}

// Agent types

type AgentResource struct {
	APIVersion string               `json:"apiVersion,omitempty"`
	Kind       string               `json:"kind,omitempty"`
	Metadata   metav1.ObjectMeta    `json:"metadata,omitempty"`
	Spec       v1alpha2.AgentSpec   `json:"spec,omitempty"`
	Status     v1alpha2.AgentStatus `json:"status,omitempty"`
}

func AgentResourceFrom(agent v1alpha2.AgentObject) *AgentResource {
	if agent == nil {
		return nil
	}

	spec := agent.GetAgentSpec()
	status := agent.GetAgentStatus()
	gvk := agent.GetObjectKind().GroupVersionKind()
	apiVersion := gvk.GroupVersion().String()
	kind := gvk.Kind
	var metadata metav1.ObjectMeta
	if apiVersion == "" {
		apiVersion = v1alpha2.GroupVersion.String()
	}
	if kind == "" {
		if agent.GetWorkloadMode() == v1alpha2.WorkloadModeSandbox {
			kind = "SandboxAgent"
		} else {
			kind = "Agent"
		}
	}
	switch typed := agent.(type) {
	case *v1alpha2.Agent:
		metadata = *typed.ObjectMeta.DeepCopy()
	case *v1alpha2.SandboxAgent:
		metadata = *typed.ObjectMeta.DeepCopy()
	default:
		metadata = metav1.ObjectMeta{
			Name:            agent.GetName(),
			Namespace:       agent.GetNamespace(),
			Labels:          agent.GetLabels(),
			Annotations:     agent.GetAnnotations(),
			ResourceVersion: agent.GetResourceVersion(),
			Generation:      agent.GetGeneration(),
		}
	}

	res := &AgentResource{
		APIVersion: apiVersion,
		Kind:       kind,
		Metadata:   metadata,
	}
	if spec != nil {
		res.Spec = *spec.DeepCopy()
	}
	if status != nil {
		res.Status = *status.DeepCopy()
	}
	return res
}

type AgentResponse struct {
	ID    string         `json:"id"`
	Agent *AgentResource `json:"agent"`
	// Config         *adk.AgentConfig       `json:"config"`
	ModelProvider   v1alpha2.ModelProvider `json:"modelProvider"`
	Model           string                 `json:"model"`
	ModelConfigRef  string                 `json:"modelConfigRef"`
	MemoryRefs      []string               `json:"memoryRefs"`
	Tools           []*v1alpha2.Tool       `json:"tools"`
	DeploymentReady bool                   `json:"deploymentReady"`
	Accepted        bool                   `json:"accepted"`
	WorkloadMode    v1alpha2.WorkloadMode  `json:"workloadMode,omitempty"`
}

// Session types

// SessionRequest represents a session creation/update request
type SessionRequest struct {
	AgentRef *string                 `json:"agent_ref,omitempty"`
	Name     *string                 `json:"name,omitempty"`
	ID       *string                 `json:"id,omitempty"`
	Source   *database.SessionSource `json:"source,omitempty"`
}

// Run types

// RunRequest represents a run creation request
type RunRequest struct {
	Task string `json:"task"`
}

// Run represents a run from the database
type Task = database.Task

// Message represents a message from the database
type Message = database.Event

// Session represents a session from the database
type Session = database.Session

// Agent represents an agent from the database
type Agent = database.Agent

// Tool types

// Tool represents a tool from the database
type Tool = database.Tool

// Feedback represents a feedback from the database
type Feedback = database.Feedback

// ToolServer types

// ToolServerResponse represents a tool server response
type ToolServerResponse struct {
	Ref             string              `json:"ref"`
	GroupKind       string              `json:"groupKind"`
	DiscoveredTools []*v1alpha2.MCPTool `json:"discoveredTools"`
}

// Memory types

// MemoryResponse represents a memory response
type MemoryResponse struct {
	Ref             string         `json:"ref"`
	ProviderName    string         `json:"providerName"`
	APIKeySecretRef string         `json:"apiKeySecretRef"`
	APIKeySecretKey string         `json:"apiKeySecretKey"`
	MemoryParams    map[string]any `json:"memoryParams"`
}

// CreateMemoryRequest represents a request to create a memory
type CreateMemoryRequest struct {
	Ref            string                   `json:"ref"`
	Provider       Provider                 `json:"provider"`
	APIKey         string                   `json:"apiKey"`
	PineconeParams *v1alpha1.PineconeConfig `json:"pinecone,omitempty"`
}

// UpdateMemoryRequest represents a request to update a memory
type UpdateMemoryRequest struct {
	PineconeParams *v1alpha1.PineconeConfig `json:"pinecone,omitempty"`
}

// PromptTemplateSummary is a lightweight entry for listing prompt ConfigMaps.
type PromptTemplateSummary struct {
	Namespace string   `json:"namespace"`
	Name      string   `json:"name"`
	KeyCount  int      `json:"keyCount"`
	Keys      []string `json:"keys,omitempty"`
}

// PromptTemplateDetail includes all string keys for editing.
type PromptTemplateDetail struct {
	Namespace string            `json:"namespace"`
	Name      string            `json:"name"`
	Data      map[string]string `json:"data"`
}

// CreatePromptTemplateRequest creates a labeled ConfigMap in the namespace.
type CreatePromptTemplateRequest struct {
	Namespace string            `json:"namespace"`
	Name      string            `json:"name"`
	Data      map[string]string `json:"data"`
}

// UpdatePromptTemplateRequest replaces the data map of an existing ConfigMap.
type UpdatePromptTemplateRequest struct {
	Data map[string]string `json:"data"`
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

// SessionRunsResponse represents the response for session runs
type SessionRunsResponse struct {
	Status bool `json:"status"`
	Data   any  `json:"data"`
}

// SessionRunsData represents the data part of session runs response
type SessionRunsData struct {
	Runs []any `json:"runs"`
}
