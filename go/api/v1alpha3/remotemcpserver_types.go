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

package v1alpha3

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
// +kubebuilder:validation:XValidation:message="headersFrom cannot set Authorization when tokenPassthrough is enabled",rule="!(has(self.tokenPassthrough) && self.tokenPassthrough) || !has(self.headersFrom) || !self.headersFrom.exists(h, h.name.lowerAscii() == 'authorization')"
// +kubebuilder:validation:AtMostOneOf=actorJwt;tokenPassthrough
// +kubebuilder:validation:XValidation:message="headersFrom cannot set Authorization when actorJwt is enabled",rule="!has(self.actorJwt) || !has(self.headersFrom) || !self.headersFrom.exists(h, h.name.lowerAscii() == 'authorization')"
// +kubebuilder:validation:XValidation:message="sessionIdHeader must not match a name in headersFrom",rule="!has(self.sessionIdHeader) || !has(self.headersFrom) || !self.headersFrom.exists(h, h.name.lowerAscii() == self.sessionIdHeader.lowerAscii())"
type RemoteMCPServerSpec struct {
	// ActorJWT authenticates as the executing Actor with Authorization: Bearer.
	// Substrate injects the token; tool discovery runs inside the Actor.
	// +optional
	ActorJWT *ActorJWT `json:"actorJwt,omitempty"`

	// +required
	Description string `json:"description"`
	// +kubebuilder:default=STREAMABLE_HTTP
	// +optional
	Protocol RemoteMCPServerProtocol `json:"protocol,omitempty"`
	// +kubebuilder:validation:MinLength=1
	// +required
	URL string `json:"url"`
	// HeadersFrom supplies request headers. Secret-backed headers are injected by
	// the egress gateway and cannot use routing, framing, hop-by-hop,
	// proxy-authentication, or Cookie headers. At most 64 headers may be supplied.
	// +optional
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:XValidation:rule="!has(self.valueFrom) || self.valueFrom.type != 'Secret' || self.name.matches('^[!#$%&\\\\x27*+.^_`|~0-9A-Za-z-]+$')",message="Secret-backed headers require a valid HTTP header name",fieldPath=".name"
	// +kubebuilder:validation:items:XValidation:rule="!has(self.valueFrom) || self.valueFrom.type != 'Secret' || !(self.name.lowerAscii() in ['host', 'connection', 'content-length', 'transfer-encoding', 'te', 'trailer', 'upgrade', 'keep-alive', 'proxy-authorization', 'proxy-authenticate', 'cookie'])",message="this header cannot carry an egress credential",fieldPath=".name"
	HeadersFrom []ValueRef `json:"headersFrom,omitempty"`
	// SessionIDHeader is the name of the HTTP header that carries the kagent session ID on requests to this server.
	// When empty, no session ID is sent.
	// Tool discovery has no session, so the header is left out there.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:XValidation:rule="self.matches('^[!#$%&\\x27*+.^_`|~0-9A-Za-z-]+$')",message="sessionIdHeader must be a valid HTTP header name"
	// +kubebuilder:validation:XValidation:rule="!(self.lowerAscii() in ['host', 'connection', 'content-length', 'transfer-encoding', 'te', 'trailer', 'upgrade', 'keep-alive', 'proxy-authorization', 'proxy-authenticate', 'cookie', 'authorization'])",message="sessionIdHeader cannot use this header name"
	SessionIDHeader string `json:"sessionIdHeader,omitempty"`
	// TokenPassthrough forwards the bearer token from the incoming A2A request
	// to this server as its Authorization header. Requests without a caller
	// token fail. Controller tool discovery has no caller token, so servers that
	// require one should disable discovery with the kagent.dev/discovery label.
	// +optional
	TokenPassthrough *bool `json:"tokenPassthrough,omitempty"`
	// +optional
	// +kubebuilder:default="30s"
	// +kubebuilder:validation:XValidation:rule="duration(self) > duration('0s') && duration(self) <= duration('9223372036s')",message="timeout must be positive and at most 9223372036s"
	Timeout *metav1.Duration `json:"timeout,omitempty"`
	// +optional
	// +kubebuilder:validation:XValidation:rule="duration(self) > duration('0s') && duration(self) <= duration('9223372036s')",message="sseReadTimeout must be positive and at most 9223372036s"
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
	// DisableVerify turns off certificate validation for development or testing.
	// Custom CA bundles are not supported. TLS must be unset for HTTP URLs.
	// +optional
	TLS *TLSConfig `json:"tls,omitempty"`
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
	// Deferred with custom CA Secret references and rotation handling.
	// SecretHash string `json:"secretHash,omitempty"`
}

type MCPTool struct {
	// +required
	Name string `json:"name"`
	// +required
	Description string `json:"description"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
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
