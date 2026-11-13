// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package v1alpha3

import "k8s.io/apimachinery/pkg/types"

// RuntimeEnvVar configures one literal runtime environment variable.
type RuntimeEnvVar struct {
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`

	// Value is a literal value, including an empty string.
	// +required
	Value string `json:"value"`

	// Deferred until secret-backed environment injection is supported.
	// CredentialRef *corev1.SecretKeySelector `json:"credentialRef,omitempty"`
}

// RuntimeSnapshotPolicy configures storage for Substrate snapshots.
type RuntimeSnapshotPolicy struct {
	// Location is the snapshot storage location used by Substrate.
	// +kubebuilder:validation:Pattern=`^[^[:space:]]+$`
	// +required
	Location string `json:"location"`
}

// RuntimeSubstratePolicy contains the Substrate policy shared by all runtime variants.
//
// +kubebuilder:validation:XValidation:rule="self.workerPoolRef.name.size() > 0",message="workerPoolRef name must not be empty"
type RuntimeSubstratePolicy struct {
	// WorkerPoolRef references a WorkerPool. An omitted namespace means the
	// resource's namespace. Another namespace must be listed in the
	// controller's substrate.workerPoolNamespaces setting.
	// +required
	WorkerPoolRef WorkerPoolReference `json:"workerPoolRef"`

	// SnapshotPolicy configures runtime snapshot storage.
	// +required
	SnapshotPolicy RuntimeSnapshotPolicy `json:"snapshotPolicy"`
}

// WorkerPoolReference identifies a Substrate WorkerPool; an omitted namespace
// means the owner's namespace.
type WorkerPoolReference struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	// +required
	Name string `json:"name"`
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^$|^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// NamespacedName resolves the reference relative to its owner.
func (r WorkerPoolReference) NamespacedName(ownerNamespace string) types.NamespacedName {
	if r.Namespace != "" {
		ownerNamespace = r.Namespace
	}
	return types.NamespacedName{Namespace: ownerNamespace, Name: r.Name}
}
