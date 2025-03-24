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

const (
	AgentConditionTypeAccepted = "Accepted"
)

// AgentSpec defines the desired state of Agent.
type AgentSpec struct {
	Description string `json:"description,omitempty"`
	// +kubebuilder:validation:MinLength=1
	SystemMessage string `json:"systemMessage,omitempty"`
	// +kubebuilder:validation:MaxItems=20
	Tools []*Tool `json:"tools,omitempty"`
}

type Tool struct {
	// ONEOF: BuiltinTool, McpServerTool
	BuiltinTool   `json:",inline"`
	McpServerTool `json:",inline"`
}

type BuiltinTool struct {
	Provider string `json:"provider,omitempty"`
	// Description is a brief description of the tool.
	Description string `json:"description,omitempty"`
	// note: this implementation is due to the kubebuilder limitation https://github.com/kubernetes-sigs/controller-tools/issues/636
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	Config map[string]AnyType `json:"config,omitempty"`
}

type McpServerTool struct {
	// the name of the ToolServer that provides the tool. must exist in the same namespace as the Agent
	ToolServer string `json:"toolServer,omitempty"`
	// The name of the tool to be provided by the ToolServer
	// For a list of all the tools provided by the server,
	// the client can query the status of the ToolServer object after it has been created
	ToolName string `json:"toolName,omitempty"`
}

type AnyType struct {
	json.RawMessage `json:",inline"`
}

// AgentStatus defines the observed state of Agent.
type AgentStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Accepted",type="string",JSONPath=".status.conditions[0].status",description="Whether or not the agent has been accepted by the system."

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
