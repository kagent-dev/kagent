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

const (
	MemoryConditionTypeAccepted = "Accepted"
)

// MemoryProvider represents the memory provider type
// +kubebuilder:validation:Enum=Pinecone
type MemoryProvider string

const (
	Pinecone MemoryProvider = "Pinecone"
)

// PineconeConfig contains Pinecone-specific configuration options
type PineconeConfig struct {
	// The index host to connect to
	// +required
	IndexHost string `json:"indexHost,omitempty"`
	// The number of results to return from the Pinecone index
	// +optional
	TopK int `json:"topK,omitempty"`
	// The namespace to use for the Pinecone index. If not provided, the default namespace will be used.
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// The fields to retrieve from the Pinecone index. If not provided, all fields will be retrieved.
	// +optional
	RecordFields []string `json:"recordFields,omitempty"`
	// The score threshold of results to include in the context. Results with a score below this threshold will be ignored.
	// +optional
	ScoreThreshold string `json:"scoreThreshold,omitempty"`
}

// MemorySpec defines the desired state of Memory.
type MemorySpec struct {
	// The provider of the memory
	// +kubebuilder:default=Pinecone
	Provider MemoryProvider `json:"provider"`

	// The reference to the secret that contains the API key. Can either be a reference to the name of a secret in the same namespace as the referencing Memory,
	// or a reference to the name of a secret in a different namespace in the form <namespace>/<name>
	// +optional
	APIKeySecretRef string `json:"apiKeySecretRef"`

	// The key in the secret that contains the API key
	// +optional
	APIKeySecretKey string `json:"apiKeySecretKey"`

	// The configuration for the Pinecone memory provider
	// +optional
	Pinecone *PineconeConfig `json:"pinecone,omitempty"`
}

// MemoryStatus defines the observed state of Memory.
type MemoryStatus struct {
	Conditions         []metav1.Condition `json:"conditions"`
	ObservedGeneration int64              `json:"observedGeneration"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Provider",type="string",JSONPath=".spec.provider"

// Memory is the Schema for the memories API.
type Memory struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MemorySpec   `json:"spec,omitempty"`
	Status MemoryStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MemoryList contains a list of Memory resources.
type MemoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Memory `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Memory{}, &MemoryList{})
}
