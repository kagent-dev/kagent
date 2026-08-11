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
)

type AgentType string

// AgentSpec defines the desired state of Agent.
type AgentSpec struct {
	// +optional
	Description string `json:"description,omitempty"`
	// +kubebuilder:validation:MinLength=1
	// +optional
	SystemMessage string `json:"systemMessage,omitempty"`
	// Can either be a reference to the name of a ModelConfig in the same namespace as the referencing Agent, or a reference to the name of a ModelConfig in a different namespace in the form <namespace>/<name>
	// +optional
	ModelConfig string `json:"modelConfig,omitempty"`
	// Whether to stream the response from the model.
	// If not specified, the default value is true.
	// +optional
	Stream *bool `json:"stream,omitempty"`
	// +kubebuilder:validation:MaxItems=20
	// +optional
	Tools []*Tool `json:"tools,omitempty"`
	// Can either be a reference to the name of a Memory in the same namespace as the referencing Agent, or a reference to the name of a Memory in a different namespace in the form <namespace>/<name>
	// +optional
	Memory []string `json:"memory,omitempty"`
	// A2AConfig instantiates an A2A server for this agent,
	// served on the HTTP port of the kagent kubernetes
	// controller (default 8083).
	// The A2A server URL will be served at
	// <kagent-controller-ip>:8083/api/a2a-sandboxes/<agent-namespace>/<agent-name>
	// Read more about the A2A protocol here: https://github.com/a2aproject/A2A
	// +optional
	A2AConfig *A2AConfig `json:"a2aConfig,omitempty"`
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
	// +optional
	Type ToolProviderType `json:"type,omitempty"`
	// +optional
	McpServer *McpServerTool `json:"mcpServer,omitempty"`
	// +optional
	Agent *AgentTool `json:"agent,omitempty"`
}

type AgentTool struct {
	// Reference to the Agent resource to use as a tool.
	// Can either be a reference to the name of an Agent in the same namespace as the referencing Agent, or a reference to the name of an Agent in a different namespace in the form <namespace>/<name>
	// +kubebuilder:validation:MinLength=1
	// +optional
	Ref string `json:"ref,omitempty"`
}

type McpServerTool struct {
	// the name of the ToolServer that provides the tool. can either be a reference to the name of a ToolServer in the same namespace as the referencing Agent, or a reference to the name of an ToolServer in a different namespace in the form <namespace>/<name>
	// +optional
	ToolServer string `json:"toolServer,omitempty"`
	// The names of the tools to be provided by the ToolServer
	// For a list of all the tools provided by the server,
	// the client can query the status of the ToolServer object after it has been created
	// +optional
	ToolNames []string `json:"toolNames,omitempty"`
}

type AnyType struct {
	json.RawMessage `json:",inline"`
}

type A2AConfig struct {
	// +kubebuilder:validation:MinItems=1
	// +optional
	Skills []AgentSkill `json:"skills,omitempty"`
}

// AgentSkill describes a specific capability or function of the agent.
type AgentSkill struct {
	// ID is the unique identifier for the skill.
	// +optional
	ID string `json:"id,omitempty"`
	// Name is the human-readable name of the skill.
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
	// Description is an optional detailed description of the skill.
	// +optional
	Description string `json:"description,omitempty"`
	// Tags are optional tags for categorization.
	// +optional
	// +kubebuilder:validation:MaxItems=20
	Tags []string `json:"tags,omitempty"`
	// Examples are optional usage examples.
	// +optional
	// +kubebuilder:validation:MaxItems=20
	Examples []string `json:"examples,omitempty"`
	// InputModes are the supported input data modes/types.
	// +optional
	InputModes []string `json:"inputModes,omitempty"`
	// OutputModes are the supported output data modes/types.
	// +optional
	OutputModes []string `json:"outputModes,omitempty"`
}

const (
	AgentConditionTypeAccepted = "Accepted"
	AgentConditionTypeReady    = "Ready"
)

// AgentStatus defines the observed state of Agent.
type AgentStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// This is used to determine if the agent config has changed.
	// If it has changed, the agent will be restarted.
	// +optional
	ConfigHash []byte `json:"configHash,omitempty"`
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}
