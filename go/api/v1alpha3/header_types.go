// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package v1alpha3

import corev1 "k8s.io/api/core/v1"

// Header sets an HTTP header from a credential source for the declared endpoint.
// Literal values are not supported by this contract.
type Header struct {
	// Name is the HTTP credential header, for example Authorization or X-API-Key.
	// +required
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:Pattern=`^[!#$%&'*+.^_\x60|~0-9A-Za-z-]+$`
	// +kubebuilder:validation:XValidation:rule="!(self.lowerAscii() in ['host', 'connection', 'content-length', 'transfer-encoding', 'te', 'trailer', 'upgrade', 'keep-alive', 'proxy-authorization', 'proxy-authenticate', 'cookie'])",message="this header cannot carry an egress credential"
	Name string `json:"name"`
	// Prefix is prepended verbatim, including any separating space. For example, "Bearer ".
	// +optional
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:Pattern=`^[^\x00-\x08\x0a-\x1f\x7f]*$`
	Prefix *string `json:"prefix,omitempty"`
	// ValueFrom selects exactly one credential source.
	// +required
	ValueFrom HeaderValueSource `json:"valueFrom"`
}

// HeaderValueSource selects the credential used to populate a header.
// +kubebuilder:validation:ExactlyOneOf=secretKeyRef;tokenPassthrough
type HeaderValueSource struct {
	// SecretKeyRef selects a required Secret key in the resource's namespace.
	// +optional
	// +kubebuilder:validation:XValidation:rule="has(self.name) && self.name != '' && self.key != ''",message="secretKeyRef requires a name and key"
	// +kubebuilder:validation:XValidation:rule="!has(self.optional) || !self.optional",message="credentials cannot be optional"
	SecretKeyRef *corev1.SecretKeySelector `json:"secretKeyRef,omitempty"`
	// TokenPassthrough selects the authenticated invocation's bearer credential.
	// Missing or expired credentials fail without selecting another source.
	// +optional
	TokenPassthrough *TokenPassthrough `json:"tokenPassthrough,omitempty"`
}

// TokenPassthrough forwards the invocation credential unchanged. It does not
// exchange tokens, alter audiences, or select the resource creator's identity.
type TokenPassthrough struct{}
