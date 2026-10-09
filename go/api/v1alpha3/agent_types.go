/*
Copyright 2026.

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// AgentSpec pairs portable behavior with a runner. References are local to the
// Agent's namespace, including references nested in inline specs.
// +kubebuilder:validation:XValidation:rule="has(self.template) != has(self.templateRef)",message="exactly one of template or templateRef must be specified"
// +kubebuilder:validation:XValidation:rule="has(self.harness) != has(self.harnessRef)",message="exactly one of harness or harnessRef must be specified"
type AgentSpec struct {
	// +optional
	Template *AgentTemplateSpec `json:"template,omitempty"`
	// +kubebuilder:validation:XValidation:rule="has(self.name) && self.name != ''",message="templateRef.name must not be empty"
	// +optional
	TemplateRef *corev1.LocalObjectReference `json:"templateRef,omitempty"`
	// +optional
	Harness *HarnessSpec `json:"harness,omitempty"`
	// +kubebuilder:validation:XValidation:rule="has(self.name) && self.name != ''",message="harnessRef.name must not be empty"
	// +optional
	HarnessRef *corev1.LocalObjectReference `json:"harnessRef,omitempty"`
	// Egress lists HTTP(S) origins the Agent may reach besides the destinations
	// its revision compiles (the model, its MCP servers, its skill and plugin
	// sources, telemetry), such as "https://proxy.golang.org". The host may
	// have "*" as its leftmost label: "https://*.githubusercontent.com" matches
	// one label, such as raw.githubusercontent.com. A wildcard needs two labels
	// under it, so nothing opens a whole top-level domain or every host.
	// +optional
	// +listType=map
	// +listMapKey=origin
	// +kubebuilder:validation:MaxItems=64
	Egress []EgressEntry `json:"egress,omitempty"`
}

// EgressEntry is one origin a runtime may reach.
// +kubebuilder:validation:XValidation:rule="!has(self.headers) || (self.origin.startsWith('https://') && !self.origin.contains('*'))",message="headers need an exact https origin"
type EgressEntry struct {
	// Origin is an HTTP(S) origin, such as "https://proxy.golang.org".
	// +required
	// +kubebuilder:validation:MaxLength=270
	// +kubebuilder:validation:Pattern=`^https?://(\*\.([a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?\.)+|([a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?\.)*)[a-z]([-a-z0-9]{0,61}[a-z0-9])?(:([1-9][0-9]{0,3}|[1-5][0-9]{4}|6[0-4][0-9]{3}|65[0-4][0-9]{2}|655[0-2][0-9]|6553[0-5]))?$`
	Origin string `json:"origin"`
	// Headers the egress gateway sets, so the runtime never holds their
	// values. The gateway only replaces a header the request sends, so the
	// runtime sends each with a placeholder. Credentials bind per host, not
	// per port. Whoever can write the resource can send any Secret in its
	// namespace to the origin.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=16
	// +kubebuilder:validation:XValidation:rule="self.all(a, self.exists_one(b, a.name.lowerAscii() == b.name.lowerAscii()))",message="header names must be unique, ignoring case"
	Headers []EgressHeader `json:"headers,omitempty"`
}

// EgressHeader is a request header the egress gateway sets.
// +kubebuilder:validation:XValidation:rule="self.name.lowerAscii() != 'host'",message="the gateway cannot set the Host header"
type EgressHeader struct {
	// +required
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:Pattern=`^[!#$%&'*+.^_|~0-9A-Za-z-]+$`
	Name string `json:"name"`
	// Prefix goes before the value, such as "Bearer ".
	// +optional
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^[^\x00-\x08\x0a-\x1f\x7f]*$`
	Prefix string `json:"prefix,omitempty"`
	// +required
	ValueFrom EgressHeaderSource `json:"valueFrom"`
}

// EgressHeaderSource is where a header's value comes from.
// +kubebuilder:validation:XValidation:rule="has(self.secretKeyRef)",message="secretKeyRef is required"
type EgressHeaderSource struct {
	// SecretKeyRef is a key of a Secret in the resource's namespace.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self.name != '' && self.key != ''",message="secretKeyRef needs a name and a key"
	// +kubebuilder:validation:XValidation:rule="!has(self.optional) || !self.optional",message="a header's Secret cannot be optional"
	SecretKeyRef *corev1.SecretKeySelector `json:"secretKeyRef,omitempty"`
}

const (
	AgentConditionAccepted     = "Accepted"
	AgentConditionResolvedRefs = "ResolvedRefs"
	AgentConditionCompatible   = "Compatible"
	AgentConditionReady        = "Ready"
)

// AgentStatus reports compilation and preparation of an Agent.
type AgentStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +kubebuilder:validation:MinLength=1
	// +optional
	DesiredRevision string `json:"desiredRevision,omitempty"`
	// +kubebuilder:validation:MinLength=1
	// +optional
	LatestSuccessfulRevision string `json:"latestSuccessfulRevision,omitempty"`
	// Warnings reports non-blocking compatibility decisions made while compiling
	// this Agent.
	// +kubebuilder:validation:MaxItems=100
	// +listType=set
	// +optional
	Warnings []string `json:"warnings,omitempty"`
	// +kubebuilder:validation:MaxItems=4
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=agents,singular=agent,categories=kagent
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Agent is a runnable definition with an explicit template and harness.
type Agent struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +required
	Spec AgentSpec `json:"spec"`
	// +optional
	Status AgentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AgentList contains Agent resources.
type AgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Agent `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, &Agent{}, &AgentList{})
		return nil
	})
}
