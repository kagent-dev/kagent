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
	"database/sql"
	"database/sql/driver"
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:validation:Enum=SSE;STREAMABLE_HTTP
type RemoteMCPServerProtocol string

const (
	RemoteMCPServerProtocolSse            RemoteMCPServerProtocol = "SSE"
	RemoteMCPServerProtocolStreamableHttp RemoteMCPServerProtocol = "STREAMABLE_HTTP"
)

// RemoteMCPServerSpec defines the desired state of RemoteMCPServer.
type RemoteMCPServerSpec struct {
	Description string `json:"description"`
	// +kubebuilder:default=STREAMABLE_HTTP
	// +optional
	Protocol RemoteMCPServerProtocol `json:"protocol"`
	// +kubebuilder:validation:MinLength=1
	URL string `json:"url"`
	// +optional
	HeadersFrom []ValueRef `json:"headersFrom,omitempty"`
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`
	// +optional
	SseReadTimeout *metav1.Duration `json:"sseReadTimeout,omitempty"`
	// +optional
	// +kubebuilder:default=true
	TerminateOnClose *bool `json:"terminateOnClose,omitempty"`
}

var _ sql.Scanner = (*RemoteMCPServerSpec)(nil)

func (t *RemoteMCPServerSpec) Scan(src any) error {
	switch v := src.(type) {
	case []uint8:
		return json.Unmarshal(v, t)
	}
	return nil
}

var _ driver.Valuer = (*RemoteMCPServerSpec)(nil)

func (t RemoteMCPServerSpec) Value() (driver.Value, error) {
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
	Type ValueSourceType `json:"type"`
	// The name of the ConfigMap or Secret.
	Name string `json:"name"`
	// The key of the ConfigMap or Secret.
	Key string `json:"key"`
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

// RemoteMCPServerStatus defines the observed state of RemoteMCPServer.
type RemoteMCPServerStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	ObservedGeneration int64              `json:"observedGeneration"`
	Conditions         []metav1.Condition `json:"conditions"`
	// +kubebuilder:validation:Optional
	DiscoveredTools []*MCPTool `json:"discoveredTools"`
}

type MCPTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=rmcps,categories=kagent
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Protocol",type="string",JSONPath=".spec.config.protocol"
// +kubebuilder:printcolumn:name="URL",type="string",JSONPath=".spec.config.url"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Accepted",type="string",JSONPath=".status.conditions[?(@.type=='Accepted')].status"

// RemoteMCPServer is the Schema for the RemoteMCPServers API.
type RemoteMCPServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RemoteMCPServerSpec   `json:"spec,omitempty"`
	Status RemoteMCPServerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// RemoteMCPServerList contains a list of RemoteMCPServer.
type RemoteMCPServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RemoteMCPServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RemoteMCPServer{}, &RemoteMCPServerList{})
}
