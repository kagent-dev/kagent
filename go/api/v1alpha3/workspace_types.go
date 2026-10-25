// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package v1alpha3

import corev1 "k8s.io/api/core/v1"

// WorkspaceSpec selects the initial files captured in the prepared runtime.
// An empty object selects an empty workspace.
type WorkspaceSpec struct {
	// Git initializes /data/workspace before golden readiness.
	// +optional
	Git *GitWorkspaceSource `json:"git,omitempty"`
}

// GitWorkspaceSource prepares one repository. New instances reuse the prepared
// commit; resumes and forks preserve their saved files without fetching again.
type GitWorkspaceSource struct {
	// URL is an HTTPS repository URL without embedded credentials or a query.
	// +kubebuilder:validation:Pattern=`^https://[^[:space:]@?#]+$`
	// +kubebuilder:validation:MaxLength=2048
	// +required
	URL string `json:"url"`
	// Ref selects a branch, tag, or full commit when preparing a new runtime.
	// Use a full commit for reproducibility across runtime revisions.
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9][A-Za-z0-9._/-]*$`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +required
	Ref string `json:"ref"`
	// CredentialRef selects a same-namespace Secret key containing the complete
	// Git HTTPS Authorization value. Only golden preparation receives this grant.
	// +kubebuilder:validation:XValidation:rule="has(self.name) && self.name != '' && (!has(self.optional) || !self.optional)",message="credentialRef requires a Secret name and cannot be optional"
	// +optional
	CredentialRef *corev1.SecretKeySelector `json:"credentialRef,omitempty"`
	// RefreshToken requests a new prepared checkout when changed. It is not
	// passed to Git. Existing instances retain their saved workspace.
	// +kubebuilder:validation:MaxLength=128
	// +optional
	RefreshToken *string `json:"refreshToken,omitempty"`
}
