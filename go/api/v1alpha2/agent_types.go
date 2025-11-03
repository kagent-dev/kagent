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
	// If not specified, the default value is true.
	// +optional
	Stream *bool `json:"stream,omitempty"`
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
	ExecuteCodeBlocks *bool `json:"executeCodeBlocks,omitempty"`
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

type McpServerTool struct {
	// The reference to the ToolServer that provides the tool.
	// Can either be a reference to the name of a ToolServer in the same namespace as the referencing Agent, or a reference to the name of an ToolServer in a different namespace in the form <namespace>/<name>
	// +optional
	TypedLocalReference `json:",inline"`

	// The names of the tools to be provided by the ToolServer
	// For a list of all the tools provided by the server,
	// the client can query the status of the ToolServer object after it has been created
	ToolNames []string `json:"toolNames,omitempty"`
}

type TypedLocalReference struct {
	// +optional
	Kind string `json:"kind"`
	// +optional
	ApiGroup string `json:"apiGroup"`
	Name     string `json:"name"`
}

func (t *TypedLocalReference) GroupKind() schema.GroupKind {
	return schema.GroupKind{
		Group: t.ApiGroup,
		Kind:  t.Kind,
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
