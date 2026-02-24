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

// +kubebuilder:validation:AtLeastOneOf=refs,gitRefs
type SkillForAgent struct {
	// Fetch images insecurely from registries (allowing HTTP and skipping TLS verification).
	// Meant for development and testing purposes only.
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`

	// The list of skill images to fetch.
	// +kubebuilder:validation:MaxItems=20
	// +kubebuilder:validation:MinItems=1
	// +optional
	Refs []string `json:"refs,omitempty"`

	// Reference to a Secret containing git credentials.
	// Applied to all gitRefs entries.
	// The secret should contain a `token` key for HTTPS auth,
	// or `ssh-privatekey` for SSH auth.
	// +optional
	GitAuthSecretRef *corev1.LocalObjectReference `json:"gitAuthSecretRef,omitempty"`

	// Git repositories to fetch skills from.
	// +kubebuilder:validation:MaxItems=20
	// +kubebuilder:validation:MinItems=1
	// +optional
	GitRefs []GitRepo `json:"gitRefs,omitempty"`
}

// GitRepo specifies a single Git repository to fetch skills from.
type GitRepo struct {
	// URL of the git repository (HTTPS or SSH).
	// +kubebuilder:validation:Required
	URL string `json:"url"`

	// Git reference: branch name, tag, or commit SHA.
	// +optional
	// +kubebuilder:default="main"
	Ref string `json:"ref,omitempty"`

	// Subdirectory within the repo to use as the skill root.
	// +optional
	Path string `json:"path,omitempty"`

	// Name for the skill directory under /skills. Defaults to the repo name.
	// +optional
	Name string `json:"name,omitempty"`
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

	// Memory configuration for the agent.
	// +optional
	Memory *MemorySpec `json:"memory,omitempty"`

	// Context configures context management for this agent.
	// This includes event compaction (compression) and context caching.
	// +optional
	Context *ContextConfig `json:"context,omitempty"`
}

// ContextConfig configures context management for an agent.
type ContextConfig struct {
	// Compaction configures event history compaction.
	// When enabled, older events in the conversation are compacted (compressed/summarized)
	// to reduce context size while preserving key information.
	// +optional
	Compaction *ContextCompressionConfig `json:"compaction,omitempty"`
}

// ContextCompressionConfig configures event history compaction/compression.
type ContextCompressionConfig struct {
	// The number of *new* user-initiated invocations that, once fully represented in the session's events, will trigger a compaction.
	// +optional
	// +kubebuilder:default=5
	// +kubebuilder:validation:Minimum=1
	CompactionInterval *int `json:"compactionInterval,omitempty"`
	// The number of preceding invocations to include from the end of the last compacted range. This creates an overlap between consecutive compacted summaries, maintaining context.
	// +optional
	// +kubebuilder:default=2
	// +kubebuilder:validation:Minimum=0
	OverlapSize *int `json:"overlapSize,omitempty"`
	// Summarizer configures an LLM-based summarizer for event compaction.
	// If not specified, compacted events are dropped from the context without summarization.
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
	ModelConfig *string `json:"modelConfig,omitempty"`
	// PromptTemplate is a custom prompt template for the summarizer.
	// See the ADK LlmEventSummarizer for template details:
	// https://github.com/google/adk-python/blob/main/src/google/adk/apps/llm_event_summarizer.py
	// +optional
	PromptTemplate *string `json:"promptTemplate,omitempty"`
}

// MemorySpec enables long-term memory for an agent.
type MemorySpec struct {
	// ModelConfig is the name of the ModelConfig object whose embedding
	// provider will be used to generate memory vectors.
	// +kubebuilder:validation:Required
	ModelConfig string `json:"modelConfig"`

	// TTLDays controls how many days a stored memory entry remains valid before
	// it is eligible for pruning. Defaults to 15 days when unset or zero.
	// +optional
	// +kubebuilder:validation:Minimum=1
	TTLDays int `json:"ttlDays,omitempty"`
}

type DeclarativeDeploymentSpec struct {
	// +optional
	ImageRegistry string `json:"imageRegistry,omitempty"`

	SharedDeploymentSpec `json:",inline"`
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

// +kubebuilder:validation:XValidation:rule="!has(self.requireApproval) || !has(self.toolNames) || self.requireApproval.all(t, self.toolNames.exists(n, n == t))",message="each requireApproval entry must also appear in toolNames"
type McpServerTool struct {
	// The reference to the ToolServer that provides the tool.
	// +optional
	TypedLocalReference `json:",inline"`

	// The names of the tools to be provided by the ToolServer
	// For a list of all the tools provided by the server,
	// the client can query the status of the ToolServer object after it has been created
	ToolNames []string `json:"toolNames,omitempty"`

	// RequireApproval lists tool names that require human approval before
	// execution. Each name must also appear in ToolNames. When a tool in
	// this list is invoked by the agent, execution pauses and the user is
	// prompted to approve or reject the call.
	// +optional
	RequireApproval []string `json:"requireApproval,omitempty"`

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
