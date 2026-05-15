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
	"database/sql"
	"database/sql/driver"
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// ToolServerSpec defines the desired state of ToolServer.
type ToolServerSpec struct {
	// +required
	Description string `json:"description"`
	// +required
	Config ToolServerConfig `json:"config"`
}

type ToolServerType string

const (
	ToolServerTypeStdio          ToolServerType = "stdio"
	ToolServerTypeSse            ToolServerType = "sse"
	ToolServerTypeStreamableHttp ToolServerType = "streamableHttp"
)

// Only one of stdio, sse, or streamableHttp can be specified.
// +kubebuilder:validation:XValidation:rule="(has(self.stdio) && !has(self.sse) && !has(self.streamableHttp)) || (!has(self.stdio) && has(self.sse) && !has(self.streamableHttp)) || (!has(self.stdio) && !has(self.sse) && has(self.streamableHttp))",message="Exactly one of stdio, sse, or streamableHttp must be specified"
type ToolServerConfig struct {
	// +optional
	Type ToolServerType `json:"type,omitempty"`
	// +optional
	Stdio *StdioMcpServerConfig `json:"stdio,omitempty"`
	// +optional
	Sse *SseMcpServerConfig `json:"sse,omitempty"`
	// +optional
	StreamableHttp *StreamableHttpServerConfig `json:"streamableHttp,omitempty"`
}

var _ sql.Scanner = (*ToolServerConfig)(nil)

func (t *ToolServerConfig) Scan(src any) error {
	switch v := src.(type) {
	case []uint8:
		return json.Unmarshal(v, t)
	}
	return nil
}

var _ driver.Valuer = (*ToolServerConfig)(nil)

func (t ToolServerConfig) Value() (driver.Value, error) {
	return json.Marshal(t)
}

type ValueSourceType string

const (
	ConfigMapValueSource ValueSourceType = "ConfigMap"
	SecretValueSource    ValueSourceType = "Secret"
)

// ValueSource defines a source for configuration values from a Secret or ConfigMap
type ValueSource struct {
	// +kubebuilder:validation:Enum=ConfigMap;Secret
	// +required
	Type ValueSourceType `json:"type"`
	// The reference to the ConfigMap or Secret. Can either be a reference to a resource in the same namespace,
	// or a reference to a resource in a different namespace in the form "namespace/name".
	// If namespace is not provided, the default namespace is used.
	// +optional
	ValueRef string `json:"valueRef,omitempty"`
	// +required
	Key string `json:"key"`
}

// ValueRef represents a configuration value
// +kubebuilder:validation:XValidation:rule="(has(self.value) && !has(self.valueFrom)) || (!has(self.value) && has(self.valueFrom))",message="Exactly one of value or valueFrom must be specified"
type ValueRef struct {
	// +required
	Name string `json:"name"`
	// +optional
	Value string `json:"value,omitempty"`
	// +optional
	ValueFrom *ValueSource `json:"valueFrom,omitempty"`
}

type StdioMcpServerConfig struct {
	// +required
	Command string `json:"command"`
	// +optional
	Args []string `json:"args,omitempty"`
	// +optional
	Env map[string]string `json:"env,omitempty"`
	// +optional
	EnvFrom []ValueRef `json:"envFrom,omitempty"`
	// Default value is 10 seconds
	// +kubebuilder:default:=10
	// +optional
	ReadTimeoutSeconds uint8 `json:"readTimeoutSeconds,omitempty"`
}

type HttpToolServerConfig struct {
	// +required
	URL string `json:"url"`
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	// +optional
	Headers map[string]AnyType `json:"headers,omitempty"`
	// +optional
	HeadersFrom []ValueRef `json:"headersFrom,omitempty"`
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`
	// +optional
	SseReadTimeout *metav1.Duration `json:"sseReadTimeout,omitempty"`
}

type SseMcpServerConfig struct {
	HttpToolServerConfig `json:",inline"`
}

type StreamableHttpServerConfig struct {
	HttpToolServerConfig `json:",inline"`
	// +optional
	TerminateOnClose *bool `json:"terminateOnClose,omitempty"`
}

// ToolServerStatus defines the observed state of ToolServer.
type ToolServerStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +optional
	DiscoveredTools []*MCPTool `json:"discoveredTools,omitempty"`
}

type MCPTool struct {
	// +required
	Name string `json:"name"`
	// +required
	Description string `json:"description"`
	// +optional
	Component *Component `json:"component,omitempty"`
}

type Component struct {
	// +required
	Provider string `json:"provider"`
	// +required
	ComponentType string `json:"component_type"`
	// +required
	Version int `json:"version"`
	// +required
	ComponentVersion int `json:"component_version"`
	// +required
	Description string `json:"description"`
	// +required
	Label string `json:"label"`
	// note: this implementation is due to the kubebuilder limitation https://github.com/kubernetes-sigs/controller-tools/issues/636
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	// +optional
	Config map[string]AnyType `json:"config,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ts,categories=kagent

// ToolServer is the Schema for the toolservers API.
type ToolServer struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec ToolServerSpec `json:"spec,omitempty"`
	// +optional
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
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, &ToolServer{}, &ToolServerList{})
		return nil
	})
}
