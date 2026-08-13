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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// AgentTemplateLocalReference identifies a resource in the AgentTemplate's namespace.
type AgentTemplateLocalReference struct {
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
}

// AgentTemplateTypedLocalReference identifies a typed resource in the AgentTemplate's namespace.
type AgentTemplateTypedLocalReference struct {
	// +kubebuilder:validation:Enum=RemoteMCPServer
	// +kubebuilder:validation:MinLength=1
	// +required
	Kind string `json:"kind"`
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
}

// AgentTemplateConfigMapKeyReference identifies a key in a same-namespace ConfigMap.
type AgentTemplateConfigMapKeyReference struct {
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	// +required
	Key string `json:"key"`
}

// AgentTemplatePromptTemplateSpec enables Go template rendering and ConfigMap includes.
type AgentTemplatePromptTemplateSpec struct {
	// DataSources are same-namespace ConfigMaps available to include("source/key").
	// +optional
	// +kubebuilder:validation:MaxItems=20
	// +listType=map
	// +listMapKey=name
	DataSources []AgentTemplatePromptSource `json:"dataSources,omitempty"`
}

// AgentTemplatePromptSource makes a same-namespace ConfigMap available to a prompt template.
type AgentTemplatePromptSource struct {
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
	// Alias is the name used by include. The ConfigMap name is used when omitted.
	// +optional
	// +kubebuilder:validation:MinLength=1
	Alias string `json:"alias,omitempty"`
}

// MCPToolBinding selects tools from a same-namespace MCP server.
type MCPToolBinding struct {
	// +required
	Server AgentTemplateTypedLocalReference `json:"server"`
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=50
	// +kubebuilder:validation:items:MinLength=1
	// +listType=set
	// +required
	Tools []string `json:"tools"`
}

// AgentToolIsolation controls whether a referenced template shares its parent's runtime boundary.
// +kubebuilder:validation:Enum=Shared;Dedicated
type AgentToolIsolation string

const (
	AgentToolIsolationShared    AgentToolIsolation = "Shared"
	AgentToolIsolationDedicated AgentToolIsolation = "Dedicated"
)

// AgentToolBinding exposes another same-namespace AgentTemplate as a logical tool.
type AgentToolBinding struct {
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
	// Description tells the parent when to route work to this binding.
	// +kubebuilder:validation:MinLength=1
	// +required
	Description string `json:"description"`
	// +required
	TemplateRef AgentTemplateLocalReference `json:"templateRef"`
	// +kubebuilder:default=Shared
	// +optional
	Isolation AgentToolIsolation `json:"isolation,omitempty"`
}

// ToolBinding selects exactly one MCP or AgentTemplate-backed tool source.
// +kubebuilder:validation:XValidation:rule="has(self.mcp) != has(self.agent)",message="exactly one of mcp or agent must be specified"
type ToolBinding struct {
	// +optional
	MCP *MCPToolBinding `json:"mcp,omitempty"`
	// +optional
	Agent *AgentToolBinding `json:"agent,omitempty"`
}

// AgentTemplateSkill identifies one standalone skill and its immutable source.
type AgentTemplateSkill struct {
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
	// +required
	Source ArtifactSource `json:"source"`
}

// GitArtifact identifies immutable content at a full Git commit ID.
type GitArtifact struct {
	// +kubebuilder:validation:MinLength=1
	// +required
	URL string `json:"url"`
	// +kubebuilder:validation:Pattern=`^([0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`
	// +required
	Commit string `json:"commit"`
}

// S3Object identifies one immutable S3 object version.
type S3Object struct {
	// +kubebuilder:validation:MinLength=1
	// +required
	Bucket string `json:"bucket"`
	// +kubebuilder:validation:MinLength=1
	// +required
	Key string `json:"key"`
	// +kubebuilder:validation:MinLength=1
	// +required
	VersionID string `json:"versionId"`
}

// BucketArtifact selects the supported object-store provider.
type BucketArtifact struct {
	// +required
	S3 S3Object `json:"s3"`
}

// ArtifactSource selects exactly one immutable artifact.
// +kubebuilder:validation:XValidation:rule="(has(self.oci) ? 1 : 0) + (has(self.git) ? 1 : 0) + (has(self.bucket) ? 1 : 0) == 1",message="exactly one of oci, git or bucket must be specified"
// +kubebuilder:validation:XValidation:rule="!has(self.path) || (!self.path.startsWith('/') && !self.path.split('/').exists(p, p == '..'))",message="path must be relative and must not contain '..' segments"
type ArtifactSource struct {
	// OCI is a digest-pinned image reference.
	// +kubebuilder:validation:Pattern=`^[^[:space:]@]+@sha256:[0-9a-f]{64}$`
	// +optional
	OCI string `json:"oci,omitempty"`
	// +optional
	Git *GitArtifact `json:"git,omitempty"`
	// +optional
	Bucket *BucketArtifact `json:"bucket,omitempty"`
	// Path selects a directory within the immutable artifact.
	// +optional
	// +kubebuilder:validation:MaxLength=1024
	Path string `json:"path,omitempty"`
}

// PluginBundle selects Agent Skills from one immutable Agent Plugins package.
type PluginBundle struct {
	// +required
	Source ArtifactSource `json:"source"`
	// An empty selection enables nothing.
	// +kubebuilder:validation:MaxItems=50
	// +kubebuilder:validation:items:MinLength=1
	// +listType=set
	// +optional
	Skills []string `json:"skills,omitempty"`
}

// HarnessAttachments lists the Harnesses permitted to run this AgentTemplate.
type HarnessAttachments struct {
	// +kubebuilder:validation:MaxItems=32
	// +listType=map
	// +listMapKey=name
	// +optional
	Include []AgentTemplateLocalReference `json:"include,omitempty"`
}

// AgentTemplateSpec defines portable agent behavior.
// +kubebuilder:validation:XValidation:rule="!(has(self.systemPrompt) && has(self.systemPromptFrom))",message="systemPrompt and systemPromptFrom are mutually exclusive"
type AgentTemplateSpec struct {
	// +required
	ModelConfig AgentTemplateLocalReference `json:"modelConfig"`
	// +optional
	Description string `json:"description,omitempty"`
	// +optional
	SystemPrompt string `json:"systemPrompt,omitempty"`
	// SystemPromptFrom references prompt text in a same-namespace ConfigMap.
	// +optional
	SystemPromptFrom *AgentTemplateConfigMapKeyReference `json:"systemPromptFrom,omitempty"`
	// +optional
	PromptTemplate *AgentTemplatePromptTemplateSpec `json:"promptTemplate,omitempty"`
	// +kubebuilder:validation:MaxItems=50
	// +optional
	Tools []ToolBinding `json:"tools,omitempty"`
	// +kubebuilder:validation:MaxItems=50
	// +listType=map
	// +listMapKey=name
	// +optional
	Skills []AgentTemplateSkill `json:"skills,omitempty"`
	// +kubebuilder:validation:MaxItems=20
	// +optional
	Plugins []PluginBundle `json:"plugins,omitempty"`
	// +optional
	Harnesses HarnessAttachments `json:"harnesses,omitempty"`
}

const (
	AgentTemplateConditionAccepted     = "Accepted"
	AgentTemplateConditionResolvedRefs = "ResolvedRefs"
	AgentTemplateConditionCompatible   = "Compatible"
	AgentTemplateConditionReady        = "Ready"
)

// AgentTemplateHarnessStatus reports runtime revision state for one explicitly requested Harness.
type AgentTemplateHarnessStatus struct {
	// Harness names an entry in spec.harnesses.include.
	// +kubebuilder:validation:MinLength=1
	// +required
	Harness string `json:"harness"`
	// +kubebuilder:validation:MinLength=1
	// +required
	DesiredRevision string `json:"desiredRevision"`
	// +kubebuilder:validation:MinLength=1
	// +optional
	LatestSuccessfulRevision string `json:"latestSuccessfulRevision,omitempty"`
	// +kubebuilder:validation:MaxItems=4
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// AgentTemplateStatus is the controller-observed state for each requested Harness.
type AgentTemplateStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Harnesses has at most one entry for each explicitly requested Harness.
	// +kubebuilder:validation:MaxItems=32
	// +listType=map
	// +listMapKey=harness
	// +optional
	Harnesses []AgentTemplateHarnessStatus `json:"harnesses,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=agenttemplates,singular=agenttemplate,categories=kagent
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// AgentTemplate defines portable agent behavior and its acceptable Harnesses.
type AgentTemplate struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Spec AgentTemplateSpec `json:"spec"`
	// +optional
	Status AgentTemplateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AgentTemplateList contains AgentTemplate resources.
type AgentTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, &AgentTemplate{}, &AgentTemplateList{})
		return nil
	})
}
