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
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:validation:Enum=SSE;STREAMABLE_HTTP
type RemoteMCPServerProtocol string

const (
	RemoteMCPServerProtocolSse            RemoteMCPServerProtocol = "SSE"
	RemoteMCPServerProtocolStreamableHttp RemoteMCPServerProtocol = "STREAMABLE_HTTP"
)

// RemoteMCPServerSpec defines the desired state of RemoteMCPServer.
//
// +kubebuilder:validation:XValidation:message="spec.tls must be unset when spec.url has http:// scheme: a TLS opinion contradicts a plaintext URL. Either drop spec.tls, or use https:// / a scheme-less URL.",rule="!self.url.startsWith('http://') || !has(self.tls)"
type RemoteMCPServerSpec struct {
	// +required
	Description string `json:"description"`
	// +kubebuilder:default=STREAMABLE_HTTP
	// +optional
	Protocol RemoteMCPServerProtocol `json:"protocol,omitempty"`
	// +kubebuilder:validation:MinLength=1
	// +required
	URL string `json:"url"`
	// +optional
	HeadersFrom []ValueRef `json:"headersFrom,omitempty"`
	// +optional
	// +kubebuilder:default="30s"
	Timeout *metav1.Duration `json:"timeout,omitempty"`
	// +optional
	SseReadTimeout *metav1.Duration `json:"sseReadTimeout,omitempty"`
	// +optional
	// +kubebuilder:default=true
	TerminateOnClose *bool `json:"terminateOnClose,omitempty"`

	// AllowedNamespaces defines which namespaces are allowed to reference this RemoteMCPServer.
	// This follows the Gateway API pattern for cross-namespace route attachments.
	// If not specified, only Agents in the same namespace can reference this RemoteMCPServer.
	// See: https://gateway-api.sigs.k8s.io/guides/multiple-ns/#cross-namespace-route-attachment
	// +optional
	AllowedNamespaces *AllowedNamespaces `json:"allowedNamespaces,omitempty"`

	// TLS configuration for the upstream MCP server connection.
	// Use this for HTTPS upstreams that present a certificate the agent's
	// system trust store does not include (corporate CA, self-signed cert
	// on a test fixture, internal MCP gateway). Reuses the same TLSConfig
	// type as ModelConfig.spec.tls — disableVerify turns off certificate
	// validation entirely, caCertSecretRef + caCertSecretKey point at a
	// PEM bundle Secret in the same namespace, and disableSystemCAs
	// trusts only the named bundle.
	//
	// Note one asymmetry with ModelConfig: a spec-level XValidation rule
	// on RemoteMCPServer rejects spec.tls when spec.url has the http://
	// scheme (a TLS opinion contradicts a plaintext URL). ModelConfig has
	// no equivalent rule, so a TLS block can sit alongside any baseUrl.
	// +optional
	TLS *TLSConfig `json:"tls,omitempty"`
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
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +optional
	DiscoveredTools []*MCPTool `json:"discoveredTools,omitempty"`
	// SecretHash stores a hash of the TLS Secret referenced by spec.tls so
	// agents that consume this RemoteMCPServer can detect cert rotation and
	// roll on the next reconcile. Empty when spec.tls.caCertSecretRef is unset.
	// +optional
	SecretHash string `json:"secretHash,omitempty"`
}

type MCPTool struct {
	// +required
	Name string `json:"name"`
	// +required
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
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec RemoteMCPServerSpec `json:"spec,omitempty"`
	// +optional
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
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, &RemoteMCPServer{}, &RemoteMCPServerList{})
		return nil
	})
}
