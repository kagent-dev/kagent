/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package v1alpha2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SandboxBackendType selects which sandbox control plane provisions the
// environment. Additional backends may be added in the future.
// +kubebuilder:validation:Enum=openshell
type SandboxBackendType string

const (
	SandboxBackendOpenshell SandboxBackendType = "openshell"
)

// SandboxSpec describes a generic remote execution environment that agents
// (or human operators) can attach to via exec or SSH.
//
// A Sandbox is distinct from a SandboxAgent: it has no agent runtime baked
// in. The backend is responsible for provisioning an environment that stays
// ready to accept incoming commands.
type SandboxSpec struct {
	// Backend selects the control plane to use. Required.
	// +kubebuilder:validation:Required
	Backend SandboxBackendType `json:"backend"`

	// Image is the container image to run in the sandbox, if the backend
	// supports per-sandbox images. Many backends (openshell) ignore this
	// and use a backend-configured default.
	// +optional
	Image string `json:"image,omitempty"`

	// Env is a list of environment variables injected into the sandbox.
	// Values use the Kubernetes EnvVar shape; ValueFrom references are
	// resolved server-side where supported.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// Network controls outbound access from the sandbox. When unset,
	// backend defaults apply.
	// +optional
	Network *SandboxNetwork `json:"network,omitempty"`
}

// SandboxNetwork captures the minimal network-policy knobs exposed to users.
type SandboxNetwork struct {
	// AllowedDomains is a list of DNS names the sandbox may reach.
	// +optional
	AllowedDomains []string `json:"allowedDomains,omitempty"`
}

// SandboxConnection describes how clients reach the provisioned sandbox.
type SandboxConnection struct {
	// Endpoint is the backend-specific address (gRPC target, SSH host:port,
	// ...) clients should use to reach the sandbox.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`
}

// SandboxStatusRef identifies a sandbox on an external control plane.
type SandboxStatusRef struct {
	Backend SandboxBackendType `json:"backend"`
	ID      string             `json:"id"`
}

// SandboxStatus is the observed state of a Sandbox.
type SandboxStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`

	// BackendRef points at the sandbox instance on the backend control
	// plane, once Ensure has succeeded at least once.
	// +optional
	BackendRef *SandboxStatusRef `json:"backendRef,omitempty"`

	// Connection is populated by the controller when the sandbox is ready.
	// +optional
	Connection *SandboxConnection `json:"connection,omitempty"`
}

// SandboxConditionType enumerates the condition types a Sandbox may report.
const (
	SandboxConditionTypeReady    = "Ready"
	SandboxConditionTypeAccepted = "Accepted"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=sbx,categories=kagent
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Backend",type="string",JSONPath=".spec.backend"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="ID",type="string",JSONPath=".status.backendRef.id"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Sandbox is a generic remote execution environment provisioned by a backend
// (e.g. OpenShell) and addressable by exec/SSH.
type Sandbox struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SandboxSpec   `json:"spec,omitempty"`
	Status SandboxStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SandboxList is a list of Sandbox resources.
type SandboxList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Sandbox `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Sandbox{}, &SandboxList{})
}
