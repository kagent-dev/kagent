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

package v1alpha1

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"trpc.group/trpc-go/trpc-a2a-go/server"
)

const (
	AgentConditionTypeAccepted = "Accepted"
)

// AgentSpec defines the desired state of Agent.
type AgentSpec struct {
	Description string `json:"description,omitempty"`
	// +kubebuilder:validation:MinLength=1
	SystemMessage string `json:"systemMessage,omitempty"`
	// +optional
	ModelConfig string `json:"modelConfig,omitempty"`
	// Whether to stream the response from the model.
	// If not specified, the default value is true.
	// +optional
	Stream *bool `json:"stream,omitempty"`
	// +kubebuilder:validation:MaxItems=20
	Tools []*Tool `json:"tools,omitempty"`
	// +optional
	Memory []string `json:"memory,omitempty"`
	// A2AConfig instantiates an A2A server for this agent,
	// served on the HTTP port of the kagent kubernetes
	// controller (default 8083).
	// The A2A server URL will be served at
	// <kagent-controller-ip>:8083/api/a2a/<agent-namespace>/<agent-name>
	// Read more about the A2A protocol here: https://github.com/google/A2A
	// +optional
	A2AConfig *A2AConfig `json:"a2aConfig,omitempty"`
}

// ToolProviderType represents the tool provider type
// +kubebuilder:validation:Enum=Builtin;McpServer;Agent
type ToolProviderType string

const (
	ToolProviderType_Builtin   ToolProviderType = "Builtin"
	ToolProviderType_McpServer ToolProviderType = "McpServer"
	ToolProviderType_Agent     ToolProviderType = "Agent"
)

// +kubebuilder:validation:XValidation:message="type.builtin must be nil if the type is not Builtin",rule="!(has(self.builtin) && self.type != 'Builtin')"
// +kubebuilder:validation:XValidation:message="type.builtin must be specified for Builtin filter.type",rule="!(!has(self.builtin) && self.type == 'Builtin')"
// +kubebuilder:validation:XValidation:message="type.mcpServer must be nil if the type is not McpServer",rule="!(has(self.mcpServer) && self.type != 'McpServer')"
// +kubebuilder:validation:XValidation:message="type.mcpServer must be specified for McpServer filter.type",rule="!(!has(self.mcpServer) && self.type == 'McpServer')"
// +kubebuilder:validation:XValidation:message="type.agent must be nil if the type is not Agent",rule="!(has(self.agent) && self.type != 'Agent')"
// +kubebuilder:validation:XValidation:message="type.agent must be specified for Agent filter.type",rule="!(!has(self.agent) && self.type == 'Agent')"
type Tool struct {
	// +kubebuilder:validation:Enum=Builtin;McpServer;Agent
	Type ToolProviderType `json:"type,omitempty"`
	// +optional
	Builtin *BuiltinTool `json:"builtin,omitempty"`
	// +optional
	McpServer *McpServerTool `json:"mcpServer,omitempty"`
	// +optional
	Agent *AgentTool `json:"agent,omitempty"`
}

type AgentTool struct {
	// Reference to the Agent resource to use as a tool.
	// Can either be a reference to the name of an Agent in the same namespace as the referencing Agent, or a reference to the name of an Agent in a different namespace in the form <namespace>/<name>
	// +kubebuilder:validation:MinLength=1
	Ref string `json:"ref,omitempty"`
}

type BuiltinTool struct {
	// the name of the builtin tool
	Name string `json:"name,omitempty"`
	// note: this implementation is due to the kubebuilder limitation https://github.com/kubernetes-sigs/controller-tools/issues/636
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	Config map[string]AnyType `json:"config,omitempty"`
}

type McpServerTool struct {
	// the name of the ToolServer that provides the tool. can either be a reference to the name of a ToolServer in the same namespace as the referencing Agent, or a reference to the name of an ToolServer in a different namespace in the form <namespace>/<name>
	ToolServer string `json:"toolServer,omitempty"`
	// The names of the tools to be provided by the ToolServer
	// For a list of all the tools provided by the server,
	// the client can query the status of the ToolServer object after it has been created
	ToolNames []string `json:"toolNames,omitempty"`
}

type AnyType struct {
	json.RawMessage `json:",inline"`
}

type A2AConfig struct {
	// +kubebuilder:validation:MinItems=1
	Skills []AgentSkill `json:"skills,omitempty"`
}

type AgentSkill server.AgentSkill

// AgentStatus defines the observed state of Agent.
type AgentStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Accepted",type="string",JSONPath=".status.conditions[0].status",description="Whether or not the agent has been accepted by the system."
// +kubebuilder:printcolumn:name="ModelConfig",type="string",JSONPath=".spec.modelConfig",description="The ModelConfig resource referenced by this agent."

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
