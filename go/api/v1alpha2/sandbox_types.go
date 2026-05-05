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
// +kubebuilder:validation:Enum=openshell;openclaw;nemoclaw
type SandboxBackendType string

const (
	SandboxBackendOpenshell SandboxBackendType = "openshell"
	SandboxBackendOpenClaw  SandboxBackendType = "openclaw"
	SandboxBackendNemoClaw  SandboxBackendType = "nemoclaw"
)

// SandboxChannelType selects a messenger integration for OpenClaw sandboxes.
// +kubebuilder:validation:Enum=telegram;discord;slack
type SandboxChannelType string

const (
	SandboxChannelTypeTelegram SandboxChannelType = "telegram"
	SandboxChannelTypeDiscord  SandboxChannelType = "discord"
	SandboxChannelTypeSlack    SandboxChannelType = "slack"
)

// SandboxChannelAccess controls whether the bot listens broadly or only on an allowlist.
// +kubebuilder:validation:Enum=allowlist;open;disabled
type SandboxChannelAccess string

const (
	SandboxChannelAccessAllowlist SandboxChannelAccess = "allowlist"
	SandboxChannelAccessOpen      SandboxChannelAccess = "open"
	SandboxChannelAccessDisabled  SandboxChannelAccess = "disabled"
)

// SandboxChannelCredential supplies a token from an inline value or a Secret/ConfigMap key.
//
// +kubebuilder:validation:XValidation:rule="!(has(self.valueFrom) && has(self.value) && self.value != '') && (has(self.valueFrom) || (has(self.value) && self.value != ''))",message="exactly one of value or valueFrom must be set"
type SandboxChannelCredential struct {
	// +kubebuilder:validation:MaxLength=8192
	Value string `json:"value,omitempty"`
	// +optional
	ValueFrom *ValueSource `json:"valueFrom,omitempty"`
}

// SandboxTelegramChannelSpec configures Telegram when SandboxChannel.type is Telegram.
//
// +kubebuilder:validation:XValidation:rule="!(size(self.allowedUserIDs) > 0 && has(self.allowedUserIDsFrom))",message="allowedUserIDs and allowedUserIDsFrom are mutually exclusive"
type SandboxTelegramChannelSpec struct {
	BotToken SandboxChannelCredential `json:"botToken"`
	// +optional
	AllowedUserIDs []string `json:"allowedUserIDs,omitempty"`
	// +optional
	AllowedUserIDsFrom *ValueSource `json:"allowedUserIDsFrom,omitempty"`
}

// SandboxDiscordChannelSpec configures Discord when SandboxChannel.type is Discord.
//
// +kubebuilder:validation:XValidation:rule="self.channelAccess != 'allowlist' || (has(self.allowlistChannels) && size(self.allowlistChannels) > 0)",message="allowlistChannels is required when channelAccess is allowlist"
type SandboxDiscordChannelSpec struct {
	BotToken SandboxChannelCredential `json:"botToken"`
	// +kubebuilder:validation:Required
	ChannelAccess SandboxChannelAccess `json:"channelAccess"`
	// +optional
	AllowlistChannels []string `json:"allowlistChannels,omitempty"`
}

// SandboxSlackChannelSpec configures Slack when SandboxChannel.type is Slack.
//
// +kubebuilder:validation:XValidation:rule="self.channelAccess != 'allowlist' || (has(self.allowlistChannels) && size(self.allowlistChannels) > 0)",message="allowlistChannels is required when channelAccess is allowlist"
type SandboxSlackChannelSpec struct {
	BotToken SandboxChannelCredential `json:"botToken"`
	AppToken SandboxChannelCredential `json:"appToken"`
	// +kubebuilder:validation:Required
	ChannelAccess SandboxChannelAccess `json:"channelAccess"`
	// +optional
	AllowlistChannels []string `json:"allowlistChannels,omitempty"`
	// +optional
	// +kubebuilder:default=true
	InteractiveReplies *bool `json:"interactiveReplies,omitempty"`
}

// SandboxChannel declares one messenger binding inside an OpenClaw/NemoClaw sandbox.
//
// +kubebuilder:validation:XValidation:rule="(self.type == 'telegram' && has(self.telegram) && !has(self.discord) && !has(self.slack)) || (self.type == 'discord' && has(self.discord) && !has(self.telegram) && !has(self.slack)) || (self.type == 'slack' && has(self.slack) && !has(self.telegram) && !has(self.discord))",message="exactly one of telegram, discord, or slack must be set and must match type"
type SandboxChannel struct {
	// Name is a stable id for this binding (OpenClaw channels.*.accounts key).
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:validation:Required
	Type SandboxChannelType `json:"type"`
	// +optional
	Telegram *SandboxTelegramChannelSpec `json:"telegram,omitempty"`
	// +optional
	Discord *SandboxDiscordChannelSpec `json:"discord,omitempty"`
	// +optional
	Slack *SandboxSlackChannelSpec `json:"slack,omitempty"`
}

// SandboxSpec describes a generic remote execution environment that agents
// (or human operators) can attach to via exec or SSH.
//
// A Sandbox is distinct from a SandboxAgent: it has no agent runtime baked
// in. The backend is responsible for provisioning an environment that stays
// ready to accept incoming commands.
//
// +kubebuilder:validation:XValidation:rule="!has(self.channels) || size(self.channels) == 0 || self.backend == 'openclaw' || self.backend == 'nemoclaw'",message="channels may only be set when backend is openclaw or nemoclaw"
type SandboxSpec struct {
	// Backend selects the control plane to use. Required.
	// +kubebuilder:validation:Required
	Backend SandboxBackendType `json:"backend"`

	// Description is a short human-readable summary shown in the UI (e.g. agents list).
	// +optional
	Description string `json:"description,omitempty"`

	// Image is the container image to run in the sandbox, if the backend
	// supports per-sandbox images. Backends openclaw and nemoclaw pin the image
	// to the NemoClaw sandbox base; openshell uses spec.image when set.
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

	// ModelConfigRef is the reference to the ModelConfig used to configure the sandbox.
	// When set with backend openclaw or nemoclaw, the controller registers the gateway provider and,
	// after the sandbox is Ready, writes OpenClaw config inside the VM (~/.openclaw/openclaw.json) and starts the gateway.
	// It is ignored for backend openshell.
	// +optional
	ModelConfigRef string `json:"modelConfigRef,omitempty"`

	// Channels configures Telegram, Discord, and Slack integrations for OpenClaw inside the sandbox VM.
	// Only supported when backend is openclaw or nemoclaw.
	// +optional
	Channels []SandboxChannel `json:"channels,omitempty"`
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
