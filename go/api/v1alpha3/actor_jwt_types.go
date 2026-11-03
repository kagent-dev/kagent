// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package v1alpha3

// ActorJWT identifies the executing Actor independently of the invocation caller.
// The gateway injects the token; its value never enters the runtime.
type ActorJWT struct {
	// Audiences names the services permitted to accept this token. The runtime
	// control-plane audience is reserved and cannot be exported downstream.
	// +required
	// +listType=set
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=512
	// +kubebuilder:validation:XValidation:rule="!self.exists(a, a == 'kagent-runtime')",message="the runtime control-plane audience is reserved"
	Audiences []string `json:"audiences"`
	// ExpirationSeconds requests a token lifetime. Substrate refreshes tokens at
	// the gateway independently of the invocation caller's token lifetime.
	// +optional
	// +kubebuilder:default=900
	// +kubebuilder:validation:Minimum=300
	// +kubebuilder:validation:Maximum=3600
	ExpirationSeconds *int64 `json:"expirationSeconds,omitempty"`
}
