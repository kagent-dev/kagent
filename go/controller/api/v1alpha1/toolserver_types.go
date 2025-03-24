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
	"github.com/kagent-dev/kagent/go/autogen/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"
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

type StdioMcpServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Stderr  string            `json:"stderr,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
}

type SseMcpServerConfig struct {
	URL            string                 `json:"url"`
	Headers        map[string]interface{} `json:"headers,omitempty"`
	Timeout        Duration               `json:"timeout,omitempty"`
	SseReadTimeout Duration               `json:"sse_read_timeout,omitempty"`
}

// Duration is a custom type to handle time.Duration marshaling
type Duration struct {
	time.Duration
}

// MarshalYAML formats the duration as a string with units
func (d Duration) MarshalYAML() (interface{}, error) {
	return d.String(), nil
}

// UnmarshalYAML parses the duration from a string
func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var str string
	if err := unmarshal(&str); err != nil {
		return err
	}
	dur, err := time.ParseDuration(str)
	if err != nil {
		return err
	}
	d.Duration = dur
	return nil
}

// ToolServerStatus defines the observed state of ToolServer.
type ToolServerStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	ObservedGeneration int64              `json:"observedGeneration"`
	Conditions         []metav1.Condition `json:"conditions"`
	DiscoveredTools    []*MCPTool         `json:"discoveredTools"`
}

type MCPTool struct {
	Name      string         `json:"name"`
	Component *api.Component `json:"component"`
	//Description  string              `json:"description"`
	//InputSchema  AnyType             `json:"input_schema"`
	//ServerParams MCPToolServerParams `json:"server_params"`
}

type MCPToolServerParams struct {
	Stdio *StdioMcpServerConfig `json:"stdio,omitempty"`
	Sse   *SseMcpServerConfig   `json:"sse,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

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
