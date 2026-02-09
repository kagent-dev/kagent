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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ProviderConditionTypeReady indicates whether the provider is ready for use
	ProviderConditionTypeReady = "Ready"

	// ProviderConditionTypeSecretResolved indicates whether the provider's secret reference is valid
	ProviderConditionTypeSecretResolved = "SecretResolved"

	// ProviderConditionTypeModelsDiscovered indicates whether model discovery has succeeded
	ProviderConditionTypeModelsDiscovered = "ModelsDiscovered"

	// ProviderAnnotationForceDiscovery is set by clients to trigger immediate model discovery
	ProviderAnnotationForceDiscovery = "kagent.dev/force-discovery"
)

// SecretReference contains information to locate a secret.
type SecretReference struct {
	// Name is the name of the secret in the same namespace as the Provider
	// +required
	Name string `json:"name"`

	// Key is the key within the secret that contains the API key or credential
	// +required
	Key string `json:"key"`
}

// ProviderSpec defines the desired state of Provider.
//
// +kubebuilder:validation:XValidation:message="endpoint must be a valid URL starting with http:// or https://",rule="self.endpoint.startsWith('http://') || self.endpoint.startsWith('https://')"
// +kubebuilder:validation:XValidation:message="secretRef.name and secretRef.key are required",rule="has(self.secretRef) && has(self.secretRef.name) && size(self.secretRef.name) > 0 && has(self.secretRef.key) && size(self.secretRef.key) > 0"
type ProviderSpec struct {
	// Type is the model provider type (OpenAI, Anthropic, etc.)
	// +required
	// +kubebuilder:validation:Required
	Type ModelProvider `json:"type"`

	// Endpoint is the API endpoint URL for the provider
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^https?://.*`
	Endpoint string `json:"endpoint"`

	// SecretRef references the Kubernetes Secret containing the API key
	// +required
	// +kubebuilder:validation:Required
	SecretRef SecretReference `json:"secretRef"`
}

// ProviderStatus defines the observed state of Provider.
type ProviderStatus struct {
	// ObservedGeneration reflects the generation of the most recently observed Provider spec
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the Provider's state
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// DiscoveredModels is the cached list of model IDs available from this provider
	// +optional
	DiscoveredModels []string `json:"discoveredModels,omitempty"`

	// ModelCount is the number of discovered models (for kubectl display)
	// +optional
	ModelCount int `json:"modelCount,omitempty"`

	// LastDiscoveryTime is the timestamp of the last successful model discovery
	// +optional
	LastDiscoveryTime *metav1.Time `json:"lastDiscoveryTime,omitempty"`

	// SecretHash is a hash of the referenced secret data, used to detect secret changes
	// +optional
	SecretHash string `json:"secretHash,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:categories=kagent,shortName=prov
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type"
// +kubebuilder:printcolumn:name="Endpoint",type="string",JSONPath=".spec.endpoint"
// +kubebuilder:printcolumn:name="Models",type="integer",JSONPath=".status.modelCount"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:storageversion

// Provider is the Schema for the providers API.
// It represents a model provider configuration with automatic model discovery.
type Provider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProviderSpec   `json:"spec,omitempty"`
	Status ProviderStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProviderList contains a list of Provider.
type ProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Provider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Provider{}, &ProviderList{})
}
