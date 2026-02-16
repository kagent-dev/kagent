/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha2

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"trpc.group/trpc-go/trpc-a2a-go/server"
)

// AgentType represents the agent type
// +kubebuilder:validation:Enum=Declarative;BYO
type AgentType string

const (
	AgentType_Declarative AgentType = "Declarative"
	AgentType_BYO         AgentType = "BYO"
)

// AgentSpec defines the desired state of Agent.
// +kubebuilder:validation:XValidation:message="type must be specified",rule="has(self.type)"
// +kubebuilder:validation:XValidation:message="type must be either Declarative or BYO",rule="self.type == 'Declarative' || self.type == 'BYO'"
// +kubebuilder:validation:XValidation:message="declarative must be specified if type is Declarative, or byo must be specified if type is BYO",rule="(self.type == 'Declarative' && has(self.declarative)) || (self.type == 'BYO' && has(self.byo))"
type AgentSpec struct {
	// +kubebuilder:validation:Enum=Declarative;BYO
	// +kubebuilder:default=Declarative
	Type AgentType `json:"type"`

	// +optional
	BYO *BYOAgentSpec `json:"byo,omitempty"`
	// +optional
	Declarative *DeclarativeAgentSpec `json:"declarative,omitempty"`

	// +optional
	Description string `json:"description,omitempty"`

	// Skills to load into the agent. They will be pulled from the specified container images.
	// and made available to the agent under the `/skills` folder.
	// +optional
	Skills *SkillForAgent `json:"skills,omitempty"`

	// AllowedNamespaces defines which namespaces are allowed to reference this Agent as a tool.
	// This follows the Gateway API pattern for cross-namespace route attachments.
	// If not specified, only Agents in the same namespace can reference this Agent as a tool.
	// This field only applies when this Agent is used as a tool by another Agent.
	// See: https://gateway-api.sigs.k8s.io/guides/multiple-ns/#cross-namespace-routing
	// +optional
	AllowedNamespaces *AllowedNamespaces `json:"allowedNamespaces,omitempty"`
}

type SkillForAgent struct {
	// Fetch images insecurely from registries (allowing HTTP and skipping TLS verification).
	// Meant for development and testing purposes only.
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`

	// The list of skill images to fetch.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	Refs []string `json:"refs,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!has(self.systemMessage) || !has(self.systemMessageFrom)",message="systemMessage and systemMessageFrom are mutually exclusive"
type DeclarativeAgentSpec struct {
	// SystemMessage is a string specifying the system message for the agent
	// +optional
	SystemMessage string `json:"systemMessage,omitempty"`
	// SystemMessageFrom is a reference to a ConfigMap or Secret containing the system message.
	// +optional
	SystemMessageFrom *ValueSource `json:"systemMessageFrom,omitempty"`
	// The name of the model config to use.
	// If not specified, the default value is "default-model-config".
	// Must be in the same namespace as the Agent.
	// +optional
	ModelConfig string `json:"modelConfig,omitempty"`
	// Whether to stream the response from the model.
	// If not specified, the default value is false.
	// +optional
	Stream bool `json:"stream,omitempty"`
	// +kubebuilder:validation:MaxItems=20
	Tools []*Tool `json:"tools,omitempty"`
	// A2AConfig instantiates an A2A server for this agent,
	// served on the HTTP port of the kagent kubernetes
	// controller (default 8083).
	// The A2A server URL will be served at
	// <kagent-controller-ip>:8083/api/a2a/<agent-namespace>/<agent-name>
	// Read more about the A2A protocol here: https://github.com/google/A2A
	// +optional
	A2AConfig *A2AConfig `json:"a2aConfig,omitempty"`

	// +optional
	Deployment *DeclarativeDeploymentSpec `json:"deployment,omitempty"`

	// Allow code execution for python code blocks with this agent.
	// If true, the agent will automatically execute python code blocks in the LLM responses.
	// Code will be executed in a sandboxed environment.
	// +optional
	// due to a bug in adk (https://github.com/google/adk-python/issues/3921), this field is ignored for now.
	ExecuteCodeBlocks *bool `json:"executeCodeBlocks,omitempty"`

	// Context configures context management for this agent.
	// This includes event compaction (compression) and context caching.
	// +optional
	Context *ContextConfig `json:"context,omitempty"`

	// Memory configures the memory for the agent.
	// +optional
	Memory *MemoryConfig `json:"memory,omitempty"`

	// Resumability configures the resumability for the agent.
	// +optional
	Resumability *ResumabilityConfig `json:"resumability,omitempty"`
}

type DeclarativeDeploymentSpec struct {
	// +optional
	ImageRegistry string `json:"imageRegistry,omitempty"`

	SharedDeploymentSpec `json:",inline"`
}

// ResumabilityConfig configures the resumability for the agent.
type ResumabilityConfig struct {
	// IsResumable enables agent resumability.
	// +optional
	IsResumable bool `json:"isResumable,omitempty"`
}

// MemoryType represents the memory type
// +kubebuilder:validation:Enum=InMemory;VertexAI;Mcp
type MemoryType string

const (
	MemoryTypeInMemory MemoryType = "InMemory"
	MemoryTypeVertexAI MemoryType = "VertexAI"
	MemoryTypeMcp      MemoryType = "Mcp"
)

// MemoryConfig configures the memory for the agent.
// +kubebuilder:validation:XValidation:rule="!has(self.inMemory) || self.type == 'InMemory'",message="inMemory configuration is only allowed when type is InMemory"
// +kubebuilder:validation:XValidation:rule="!has(self.vertexAi) || self.type == 'VertexAI'",message="vertexAi configuration is only allowed when type is VertexAI"
// +kubebuilder:validation:XValidation:rule="!has(self.mcp) || self.type == 'Mcp'",message="mcp configuration is only allowed when type is Mcp"
type MemoryConfig struct {
	// +kubebuilder:default=InMemory
	Type MemoryType `json:"type"`

	// +optional
	InMemory *InMemoryConfig `json:"inMemory,omitempty"`
	// +optional
	VertexAI *VertexAIMemoryConfig `json:"vertexAi,omitempty"`
	// +optional
	Mcp *McpMemoryConfig `json:"mcp,omitempty"`
}

type InMemoryConfig struct {
}

type VertexAIMemoryConfig struct {
	// +optional
	ProjectID string `json:"projectID,omitempty"`
	// +optional
	Location string `json:"location,omitempty"`
}

type McpMemoryConfig struct {
	// Name is the name of the MCP server resource.
	Name string `json:"name"`
	// Kind is the kind of the MCP server resource.
	// +optional
	// +kubebuilder:default=MCPServer
	Kind string `json:"kind,omitempty"`
	// ApiGroup is the API group of the MCP server resource.
	// +optional
	// +kubebuilder:default=kagent.dev
	ApiGroup string `json:"apiGroup,omitempty"`
}

// ContextConfig configures context management for an agent.
// Context management includes event compaction (compression/summarization) and context caching.
type ContextConfig struct {
	// Compaction configures event history compaction.
	// When enabled, older events in the conversation are compacted (compressed/summarized)
	// to reduce context size while preserving key information.
	// +optional
	Compaction *ContextCompressionConfig `json:"compaction,omitempty"`
	// Cache configures context caching.
	// When enabled, prefix context is cached at the provider level to reduce
	// redundant processing of repeated context.
	// +optional
	Cache *ContextCacheConfig `json:"cache,omitempty"`
}

// ContextCompressionConfig configures event history compaction/compression.
// +kubebuilder:validation:XValidation:rule="has(self.compactionInterval) && has(self.overlapSize)",message="compactionInterval and overlapSize are required"
type ContextCompressionConfig struct {
	// The number of *new* user-initiated invocations that, once fully represented in the session's events, will trigger a compaction.
	// +kubebuilder:validation:Minimum=1
	CompactionInterval int `json:"compactionInterval"`
	// The number of preceding invocations to include from the end of the last compacted range. This creates an overlap between consecutive compacted summaries, maintaining context.
	// +kubebuilder:validation:Minimum=0
	OverlapSize int `json:"overlapSize"`
	// Summarizer configures an LLM-based summarizer for event compaction.
	// If not specified, compacted events are simply truncated without summarization.
	// +optional
	Summarizer *ContextSummarizerConfig `json:"summarizer,omitempty"`
	// Post-invocation token threshold trigger. If set, ADK will attempt a post-invocation compaction when the most recently
	// observed prompt token count meets or exceeds this threshold.
	// +optional
	TokenThreshold *int `json:"tokenThreshold,omitempty"`
	// EventRetentionSize is the number of most recent events to always retain.
	// +optional
	EventRetentionSize *int `json:"eventRetentionSize,omitempty"`
}

// ContextSummarizerConfig configures the LLM-based event summarizer.
type ContextSummarizerConfig struct {
	// ModelConfig is the name of a ModelConfig resource to use for summarization.
	// Must be in the same namespace as the Agent.
	// If not specified, uses the agent's own model.
	// +optional
	ModelConfig string `json:"modelConfig,omitempty"`
	// PromptTemplate is a custom prompt template for the summarizer.
	// +optional
	PromptTemplate string `json:"promptTemplate,omitempty"`
}

// ContextCacheConfig configures prefix context caching at the LLM provider level.
type ContextCacheConfig struct {
	// CacheIntervals specifies how often (in number of events) to update the cache.
	// Default: 10
	// +optional
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	CacheIntervals *int `json:"cacheIntervals,omitempty"`
	// TTLSeconds specifies the time-to-live for cached context in seconds.
	// Default: 1800 (30 minutes)
	// +optional
	// +kubebuilder:default=1800
	// +kubebuilder:validation:Minimum=0
	TTLSeconds *int `json:"ttlSeconds,omitempty"`
	// MinTokens is the minimum number of tokens before caching is activated.
	// Default: 0
	// +optional
	// +kubebuilder:default=0
	// +kubebuilder:validation:Minimum=0
	MinTokens *int `json:"minTokens,omitempty"`
}

type BYOAgentSpec struct {
	// Trust relationship to the agent.
	// +optional
	Deployment *ByoDeploymentSpec `json:"deployment,omitempty"`
}

type ByoDeploymentSpec struct {
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image,omitempty"`
	// +optional
	Cmd *string `json:"cmd,omitempty"`
	// +optional
	Args []string `json:"args,omitempty"`

	SharedDeploymentSpec `json:",inline"`
}

// +kubebuilder:validation:XValidation:message="serviceAccountName and serviceAccountConfig are mutually exclusive",rule="!(has(self.serviceAccountName) && has(self.serviceAccountConfig))"
type SharedDeploymentSpec struct {
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// +optional
	Volumes []corev1.Volume `json:"volumes,omitempty"`
	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`
	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// +optional
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`
	// +optional
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`
	// ServiceAccountName specifies the name of an existing ServiceAccount to use.
	// If this field is set, the Agent controller will not create a ServiceAccount for the agent.
	// This field is mutually exclusive with ServiceAccountConfig.
	// +optional
	ServiceAccountName *string `json:"serviceAccountName,omitempty"`
	// ServiceAccountConfig configures the ServiceAccount created by the Agent controller.
	// This field can only be used when ServiceAccountName is not set.
	// If ServiceAccountName is not set, a default ServiceAccount (named after the agent)
	// is created, and this config will be applied to it.
	// +optional
	ServiceAccountConfig *ServiceAccountConfig `json:"serviceAccountConfig,omitempty"`
}

type ServiceAccountConfig struct {
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ToolProviderType represents the tool provider type
// +kubebuilder:validation:Enum=McpServer;Agent
type ToolProviderType string

const (
	ToolProviderType_McpServer ToolProviderType = "McpServer"
	ToolProviderType_Agent     ToolProviderType = "Agent"
)

// +kubebuilder:validation:XValidation:message="type.mcpServer must be nil if the type is not McpServer",rule="!(has(self.mcpServer) && self.type != 'McpServer')"
// +kubebuilder:validation:XValidation:message="type.mcpServer must be specified for McpServer filter.type",rule="!(!has(self.mcpServer) && self.type == 'McpServer')"
// +kubebuilder:validation:XValidation:message="type.agent must be nil if the type is not Agent",rule="!(has(self.agent) && self.type != 'Agent')"
// +kubebuilder:validation:XValidation:message="type.agent must be specified for Agent filter.type",rule="!(!has(self.agent) && self.type == 'Agent')"
type Tool struct {
	// +kubebuilder:validation:Enum=McpServer;Agent
	Type ToolProviderType `json:"type,omitempty"`
	// +optional
	McpServer *McpServerTool `json:"mcpServer,omitempty"`
	// +optional
	Agent *TypedLocalReference `json:"agent,omitempty"`

	// HeadersFrom specifies a list of configuration values to be added as
	// headers to requests sent to the Tool from this agent. The value of
	// each header is resolved from either a Secret or ConfigMap in the same
	// namespace as the Agent. Headers specified here will override any
	// headers of the same name/key specified on the tool.
	// +optional
	HeadersFrom []ValueRef `json:"headersFrom,omitempty"`
}

func (s *Tool) ResolveHeaders(ctx context.Context, client client.Client, namespace string) (map[string]string, error) {
	result := map[string]string{}

	for _, h := range s.HeadersFrom {
		k, v, err := h.Resolve(ctx, client, namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve header: %v", err)
		}

		result[k] = v
	}

	return result, nil
}

// ToolConfirmation configures tool confirmation (human-in-the-loop) for an MCP server's tools.
// When present, all tools from this server require user approval before execution,
// unless exempted by one of the exception rules below.
type ToolConfirmation struct {
	// Skip confirmation for tools whose MCP annotations have readOnlyHint=true
	// +optional
	ExceptReadOnly *bool `json:"exceptReadOnly,omitempty"`

	// Skip confirmation for tools whose MCP annotations have idempotentHint=true
	// +optional
	ExceptIdempotent *bool `json:"exceptIdempotent,omitempty"`

	// Skip confirmation for tools whose MCP annotations have destructiveHint=false
	// +optional
	ExceptNonDestructive *bool `json:"exceptNonDestructive,omitempty"`

	// Skip confirmation for tools with these specific names
	// +optional
	ExceptTools []string `json:"exceptTools,omitempty"`
}

type McpServerTool struct {
	// The reference to the ToolServer that provides the tool.
	// +optional
	TypedLocalReference `json:",inline"`

	// The names of the tools to be provided by the ToolServer
	// For a list of all the tools provided by the server,
	// the client can query the status of the ToolServer object after it has been created
	ToolNames []string `json:"toolNames,omitempty"`

	// AllowedHeaders specifies which headers from the A2A request should be
	// propagated to MCP tool calls. Header names are case-insensitive.
	//
	// Authorization header behavior:
	// - Authorization headers CAN be propagated if explicitly listed in allowedHeaders
	// - When STS token propagation is enabled, STS-generated Authorization headers
	//   will take precedence and replace any Authorization header from the A2A request
	// - This is a security measure to prevent request headers from overwriting
	//   authentication tokens generated by the STS integration
	//
	// Example: ["x-user-email", "x-tenant-id"]
	// +optional
	AllowedHeaders []string `json:"allowedHeaders,omitempty"`

	// Confirm configures tool confirmation (human-in-the-loop) for this server's tools.
	// When present, all tools from this server require user approval before execution,
	// unless exempted by one of the exception rules.
	// +optional
	Confirm *ToolConfirmation `json:"confirm,omitempty"`
}

type TypedLocalReference struct {
	// +optional
	Kind string `json:"kind"`
	// +optional
	ApiGroup string `json:"apiGroup"`
	Name     string `json:"name"`
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

func (t *TypedLocalReference) GroupKind() schema.GroupKind {
	return schema.GroupKind{
		Group: t.ApiGroup,
		Kind:  t.Kind,
	}
}

func (t *TypedLocalReference) NamespacedName(defaultNamespace string) types.NamespacedName {
	namespace := t.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	return types.NamespacedName{
		Namespace: namespace,
		Name:      t.Name,
	}
}

type A2AConfig struct {
	// +kubebuilder:validation:MinItems=1
	Skills []AgentSkill `json:"skills,omitempty"`
}

type AgentSkill server.AgentSkill

const (
	AgentConditionTypeAccepted = "Accepted"
	AgentConditionTypeReady    = "Ready"
)

// AgentStatus defines the observed state of Agent.
type AgentStatus struct {
	ObservedGeneration int64              `json:"observedGeneration"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type",description="The type of the agent."
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status",description="Whether or not the agent is ready to serve requests."
// +kubebuilder:printcolumn:name="Accepted",type="string",JSONPath=".status.conditions[?(@.type=='Accepted')].status",description="Whether or not the agent has been accepted by the system."
// +kubebuilder:storageversion

// Agent is the Schema for the agents API.
type Agent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentSpec   `json:"spec,omitempty"`
	Status AgentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AgentList contains a list of Agent.
type AgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Agent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Agent{}, &AgentList{})
}
