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
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	// AllowedNamespaces defines which namespaces are allowed to reference this RemoteMCPServer.
	// This follows the Gateway API pattern for cross-namespace route attachments.
	// If not specified, only Agents in the same namespace can reference this RemoteMCPServer.
	// See: https://gateway-api.sigs.k8s.io/guides/multiple-ns/#cross-namespace-routing
	// +optional
	AllowedNamespaces *AllowedNamespaces `json:"allowedNamespaces,omitempty"`
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
// +kubebuilder:printcolumn:name="Protocol",type="string",JSONPath=".spec.protocol"
// +kubebuilder:printcolumn:name="URL",type="string",JSONPath=".spec.url"
// +kubebuilder:printcolumn:name="Accepted",type="string",JSONPath=".status.conditions[?(@.type=='Accepted')].status"

// RemoteMCPServer is the Schema for the RemoteMCPServers API.
type RemoteMCPServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RemoteMCPServerSpec   `json:"spec,omitempty"`
	Status RemoteMCPServerStatus `json:"status,omitempty"`
}

// ResolveHeaders resolves all HeadersFrom entries using the object's namespace.
func (r *RemoteMCPServer) ResolveHeaders(ctx context.Context, client client.Client) (map[string]string, error) {
	result := map[string]string{}

	for _, h := range r.Spec.HeadersFrom {
		k, v, err := h.Resolve(ctx, client, r.Namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve header: %v", err)
		}

		result[k] = v
	}

	return result, nil
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
