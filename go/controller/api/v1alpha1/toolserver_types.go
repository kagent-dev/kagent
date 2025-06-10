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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ToolServerSpec defines the desired state of ToolServer.
type ToolServerSpec struct {
	Description string           `json:"description"`
	Config      ToolServerConfig `json:"config"`
}

type ToolServerConfig struct {
	Stdio *StdioMcpServerConfig `json:"stdio,omitempty"`
	Sse   *SseMcpServerConfig   `json:"sse,omitempty"`
}

type ValueSourceType string

const (
	ConfigMapValueSource ValueSourceType = "ConfigMap"
	SecretValueSource    ValueSourceType = "Secret"
)

// ValueSource defines a source for configuration values from a Secret or ConfigMap
type ValueSource struct {
	// +kubebuilder:validation:Enum=ConfigMap;Secret
	Type ValueSourceType `json:"type"`
	// The reference to the ConfigMap or Secret. Can either be a reference to a resource in the same namespace,
	// or a reference to a resource in a different namespace in the form "namespace/name".
	// If namespace is not provided, the default namespace is used.
	// +optional
	ValueRef string `json:"valueRef"`
	Key      string `json:"key"`
}

// ValueRef represents a configuration value
// +kubebuilder:validation:XValidation:rule="(has(self.value) && !has(self.valueFrom)) || (!has(self.value) && has(self.valueFrom))",message="Exactly one of value or valueFrom must be specified"
type ValueRef struct {
	Name string `json:"name"`
	// +optional
	Value string `json:"value,omitempty"`
	// +optional
	ValueFrom *ValueSource `json:"valueFrom,omitempty"`
}

type StdioMcpServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	EnvFrom []ValueRef        `json:"envFrom,omitempty"`
}

type SseMcpServerConfig struct {
	URL string `json:"url"`
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	Headers        map[string]AnyType `json:"headers,omitempty"`
	HeadersFrom    []ValueRef         `json:"headersFrom,omitempty"`
	Timeout        string             `json:"timeout,omitempty"`
	SseReadTimeout string             `json:"sse_read_timeout,omitempty"`
}

// ToolServerStatus defines the observed state of ToolServer.
type ToolServerStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	ObservedGeneration int64              `json:"observedGeneration"`
	Conditions         []metav1.Condition `json:"conditions"`
	// +kubebuilder:validation:Optional
	DiscoveredTools []*MCPTool `json:"discoveredTools"`
}

type MCPTool struct {
	Name      string    `json:"name"`
	Component Component `json:"component"`
}

type Component struct {
	Provider         string `json:"provider"`
	ComponentType    string `json:"component_type"`
	Version          int    `json:"version"`
	ComponentVersion int    `json:"component_version"`
	Description      string `json:"description"`
	Label            string `json:"label"`
	// note: this implementation is due to the kubebuilder limitation https://github.com/kubernetes-sigs/controller-tools/issues/636
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	Config map[string]AnyType `json:"config,omitempty"`
}

type MCPToolServerParams struct {
	Stdio *StdioMcpServerConfig `json:"stdio,omitempty"`
	Sse   *SseMcpServerConfig   `json:"sse,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ts

// ToolServer is the Schema for the toolservers API.
type ToolServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ToolServerSpec   `json:"spec,omitempty"`
	Status ToolServerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ToolServerList contains a list of ToolServer.
type ToolServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ToolServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ToolServer{}, &ToolServerList{})
}
