// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package v1alpha3

import corev1 "k8s.io/api/core/v1"

// RuntimeEnvVar configures one runtime environment variable: either a literal
// value or a gateway-injected credential.
// +kubebuilder:validation:XValidation:rule="has(self.value) || has(self.credentialRef)",message="one of value or credentialRef must be specified"
// +kubebuilder:validation:XValidation:rule="!has(self.credentialRef) || !has(self.value) || size(self.value) == 0",message="value and credentialRef are mutually exclusive"
type RuntimeEnvVar struct {
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`

	// Value is a literal value, including an empty string. Required unless
	// credentialRef is set.
	// +optional
	Value string `json:"value"`

	// CredentialRef binds a Secret key to one HTTP(S) destination. The runtime
	// process never receives the Secret value: the variable is set to an inert
	// placeholder, and Substrate's egress gateway replaces Header on requests
	// to the destination host with Prefix followed by the current Secret value.
	// The destination origin is added to the agent's egress allow-list.
	//
	// Use it for API keys and bearer tokens consumed by HTTP clients inside the
	// runtime. It cannot provide values that must be read by the process itself,
	// such as signing keys or web-identity token files.
	// +optional
	CredentialRef *RuntimeCredentialRef `json:"credentialRef,omitempty"`
}

// RuntimeCredentialRef selects a key of a Secret in the owning resource's
// namespace and the HTTP destination the egress gateway injects it into.
// +kubebuilder:validation:XValidation:rule="!(self.header.lowerAscii() in ['host', 'connection', 'content-length', 'transfer-encoding', 'te', 'upgrade', 'proxy-authorization', 'cookie'])",message="header must not be a hop-by-hop, framing, proxy or cookie header"
type RuntimeCredentialRef struct {
	// Name of the Secret in the owning resource's namespace.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`
	// +required
	Name string `json:"name"`

	// Key within the Secret. The full value is injected after Prefix.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[-._a-zA-Z0-9]+$`
	// +required
	Key string `json:"key"`

	// URL is the HTTP(S) destination. Only its scheme, DNS hostname and port
	// are used: the gateway matches the hostname, and the origin is allowed for
	// egress. IP addresses are not supported.
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:Pattern=`^https?://[^[:space:]/?#@]+(/[^[:space:]?#]*)?$`
	// +kubebuilder:validation:XValidation:rule="isURL(self)",message="url must be an absolute HTTP(S) URL"
	// +required
	URL string `json:"url"`

	// Header is the HTTP request header the gateway replaces. Requests that do
	// not carry the header are forwarded unchanged, so the client must send it
	// with any value (the placeholder works).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern=`^[-!#$%&'*+.^_|~0-9A-Za-z]+$`
	// +required
	Header string `json:"header"`

	// Prefix is prepended verbatim to the Secret value, for example "Bearer ".
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^[ -~]*$`
	// +optional
	Prefix string `json:"prefix,omitempty"`
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
	// WorkerPoolRef references a WorkerPool in the resource's namespace.
	// +required
	WorkerPoolRef corev1.LocalObjectReference `json:"workerPoolRef"`

	// SnapshotPolicy configures runtime snapshot storage.
	// +required
	SnapshotPolicy RuntimeSnapshotPolicy `json:"snapshotPolicy"`
}
