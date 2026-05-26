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
	"k8s.io/apimachinery/pkg/runtime"
)

// AgentHarnessBackendType selects which sandbox control plane provisions the
// environment. Additional backends may be added in the future.
// +kubebuilder:validation:Enum=openclaw;nemoclaw;hermes
type AgentHarnessBackendType string

const (
	AgentHarnessBackendOpenClaw AgentHarnessBackendType = "openclaw"
	AgentHarnessBackendNemoClaw AgentHarnessBackendType = "nemoclaw"
	AgentHarnessBackendHermes   AgentHarnessBackendType = "hermes"
)

// IsKnownAgentHarnessBackend reports backends the OpenShell harness controller and API expose.
func IsKnownAgentHarnessBackend(b AgentHarnessBackendType) bool {
	switch b {
	case AgentHarnessBackendOpenClaw, AgentHarnessBackendNemoClaw, AgentHarnessBackendHermes:
		return true
	default:
		return false
	}
}

// AgentHarnessChannelType selects a messenger integration for OpenClaw harness VMs.
// +kubebuilder:validation:Enum=telegram;slack
type AgentHarnessChannelType string

const (
	AgentHarnessChannelTypeTelegram AgentHarnessChannelType = "telegram"
	AgentHarnessChannelTypeSlack    AgentHarnessChannelType = "slack"
)

// AgentHarnessChannelAccess controls whether the bot listens broadly or only on an allowlist.
// +kubebuilder:validation:Enum=allowlist;open;disabled
type AgentHarnessChannelAccess string

const (
	AgentHarnessChannelAccessAllowlist AgentHarnessChannelAccess = "allowlist"
	AgentHarnessChannelAccessOpen      AgentHarnessChannelAccess = "open"
	AgentHarnessChannelAccessDisabled  AgentHarnessChannelAccess = "disabled"
)

// AgentHarnessChannelCredential supplies a token from an inline value or a Secret/ConfigMap key.
//
// +kubebuilder:validation:XValidation:rule="(has(self.value) && !has(self.valueFrom)) || (!has(self.value) && has(self.valueFrom))",message="Exactly one of value or valueFrom must be specified"
type AgentHarnessChannelCredential struct {
	// +kubebuilder:validation:MaxLength=8192
	// +optional
	Value string `json:"value,omitempty"`
	// +optional
	ValueFrom *ValueSource `json:"valueFrom,omitempty"`
}

// AgentHarnessTelegramChannelSpec configures Telegram when AgentHarnessChannel.type is Telegram.
//
// +kubebuilder:validation:XValidation:rule="!(size(self.allowedUserIDs) > 0 && has(self.allowedUserIDsFrom))",message="allowedUserIDs and allowedUserIDsFrom are mutually exclusive"
type AgentHarnessTelegramChannelSpec struct {
	// +required
	BotToken AgentHarnessChannelCredential `json:"botToken"`
	// +optional
	// +kubebuilder:validation:MaxItems=1024
	AllowedUserIDs []string `json:"allowedUserIDs,omitempty"`
	// +optional
	AllowedUserIDsFrom *ValueSource `json:"allowedUserIDsFrom,omitempty"`
}

// AgentHarnessOpenClawSlackOptions configures OpenClaw/NemoClaw-specific Slack routing.
//
// +kubebuilder:validation:XValidation:rule="!has(self.channelAccess) || self.channelAccess != 'allowlist' || (has(self.allowlistChannels) && size(self.allowlistChannels) > 0)",message="allowlistChannels is required when channelAccess is allowlist"
type AgentHarnessOpenClawSlackOptions struct {
	// +optional
	ChannelAccess AgentHarnessChannelAccess `json:"channelAccess,omitempty"`
	// AllowlistChannels is required when channelAccess is allowlist.
	// +optional
	// +kubebuilder:validation:MaxItems=1024
	AllowlistChannels []string `json:"allowlistChannels,omitempty"`
	// +optional
	// +kubebuilder:default=true
	InteractiveReplies *bool `json:"interactiveReplies,omitempty"`
}

// AgentHarnessHermesSlackOptions configures Hermes-specific Slack settings (env vars in the sandbox).
//
// +kubebuilder:validation:XValidation:rule="!(size(self.allowedUserIDs) > 0 && has(self.allowedUserIDsFrom))",message="allowedUserIDs and allowedUserIDsFrom are mutually exclusive"
type AgentHarnessHermesSlackOptions struct {
	// AllowedUserIDs restricts which Slack member IDs may interact with the bot (SLACK_ALLOWED_USERS).
	// +optional
	// +kubebuilder:validation:MaxItems=1024
	AllowedUserIDs []string `json:"allowedUserIDs,omitempty"`
	// +optional
	AllowedUserIDsFrom *ValueSource `json:"allowedUserIDsFrom,omitempty"`
	// HomeChannel is the default Slack channel ID for cron/scheduled messages (SLACK_HOME_CHANNEL).
	// +optional
	HomeChannel string `json:"homeChannel,omitempty"`
	// HomeChannelName is a human-readable label for HomeChannel (SLACK_HOME_CHANNEL_NAME).
	// +optional
	HomeChannelName string `json:"homeChannelName,omitempty"`
}

// AgentHarnessSlackChannelSpec configures Slack when AgentHarnessChannel.type is Slack.
// Backend-specific settings live under the matching backend key; AgentHarnessSpec validation
// requires the key to match spec.backend.
type AgentHarnessSlackChannelSpec struct {
	// +required
	BotToken AgentHarnessChannelCredential `json:"botToken"`
	// +required
	AppToken AgentHarnessChannelCredential `json:"appToken"`
	// OpenClaw configures OpenClaw/NemoClaw-specific Slack routing.
	// +optional
	OpenClaw *AgentHarnessOpenClawSlackOptions `json:"openclaw,omitempty"`
	// Hermes configures Hermes-specific Slack settings.
	// +optional
	Hermes *AgentHarnessHermesSlackOptions `json:"hermes,omitempty"`
}

// AgentHarnessChannel declares one messenger binding inside a harness VM.
//
// +kubebuilder:validation:XValidation:rule="(self.type == 'telegram' && has(self.telegram) && !has(self.slack)) || (self.type == 'slack' && has(self.slack) && !has(self.telegram))",message="exactly one of telegram or slack must be set and must match type"
type AgentHarnessChannel struct {
	// Name is a stable id for this binding (OpenClaw channels.*.accounts key).
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
	// +required
	Type AgentHarnessChannelType `json:"type"`
	// +optional
	Telegram *AgentHarnessTelegramChannelSpec `json:"telegram,omitempty"`
	// Slack configures Slack when type is Slack.
	// +optional
	Slack *AgentHarnessSlackChannelSpec `json:"slack,omitempty"`
}

// AgentHarnessSpec describes a generic remote execution environment that agents
// (or human operators) can attach to via exec or SSH.
//
// An AgentHarness is distinct from a SandboxAgent: it has no agent runtime baked
// in. The backend is responsible for provisioning an environment that stays
// ready to accept incoming commands.
// +kubebuilder:validation:XValidation:rule="!has(self.channels) || self.channels.all(c, c.type != 'slack' || (has(c.slack) && ((self.backend == 'hermes' && has(c.slack.hermes) && !has(c.slack.openclaw)) || ((self.backend == 'openclaw' || self.backend == 'nemoclaw') && has(c.slack.openclaw) && !has(c.slack.hermes)))))",message="slack backend-specific settings must match spec.backend"
type AgentHarnessSpec struct {
	// Backend selects the control plane to use. Required.
	// +required
	Backend AgentHarnessBackendType `json:"backend"`

	// Description is a short human-readable summary shown in the UI (e.g. agents list).
	// +optional
	Description string `json:"description,omitempty"`

	// Image is the container image to run in the harness VM, if the backend
	// supports per-resource images. Backends openclaw and nemoclaw pin the image
	// to the NemoClaw sandbox base when this field is empty; backend hermes pins
	// to the Hermes sandbox base image when empty.
	// +optional
	Image string `json:"image,omitempty"`

	// Env is a list of environment variables injected into the harness workload.
	// Values use the Kubernetes EnvVar shape; ValueFrom references are
	// resolved server-side where supported.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// Network controls outbound access from the harness. When unset,
	// backend defaults apply.
	// +optional
	Network *AgentHarnessNetwork `json:"network,omitempty"`

	// ModelConfigRef is the reference to the ModelConfig used to configure the harness.
	// The controller registers the gateway provider and, after the harness is Ready,
	// writes OpenClaw config inside the VM (~/.openclaw/openclaw.json) and starts the gateway.
	// +optional
	ModelConfigRef string `json:"modelConfigRef,omitempty"`

	// Channels configures Telegram and Slack integrations for OpenClaw inside the harness VM.
	// +optional
	// +kubebuilder:validation:MaxItems=1024
	Channels []AgentHarnessChannel `json:"channels,omitempty"`
}

// AgentHarnessNetwork captures the minimal network-policy knobs exposed to users.
type AgentHarnessNetwork struct {
	// AllowedDomains is a list of DNS names the harness may reach.
	// +optional
	AllowedDomains []string `json:"allowedDomains,omitempty"`
}

// AgentHarnessConnection describes how clients reach the provisioned harness VM.
type AgentHarnessConnection struct {
	// Endpoint is the backend-specific address (gRPC target, SSH host:port,
	// ...) clients should use to reach the harness.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`
}

// AgentHarnessStatusRef identifies a harness instance on an external control plane.
type AgentHarnessStatusRef struct {
	// +required
	Backend AgentHarnessBackendType `json:"backend"`
	// +required
	ID string `json:"id"`
}

// AgentHarnessStatus is the observed state of an AgentHarness.
type AgentHarnessStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// BackendRef points at the harness instance on the backend control
	// plane, once Ensure has succeeded at least once.
	// +optional
	BackendRef *AgentHarnessStatusRef `json:"backendRef,omitempty"`

	// Connection is populated by the controller when the harness is ready.
	// +optional
	Connection *AgentHarnessConnection `json:"connection,omitempty"`
}

// AgentHarnessConditionType enumerates the condition types an AgentHarness may report.
const (
	AgentHarnessConditionTypeReady    = "Ready"
	AgentHarnessConditionTypeAccepted = "Accepted"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=agentharnesses,singular=agentharness,shortName=ahr,categories=kagent
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Backend",type="string",JSONPath=".spec.backend"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="ID",type="string",JSONPath=".status.backendRef.id"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// AgentHarness is a generic remote execution environment provisioned by a backend
// (e.g. OpenShell) and addressable by exec/SSH.
type AgentHarness struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec AgentHarnessSpec `json:"spec,omitempty"`
	// +optional
	Status AgentHarnessStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AgentHarnessList is a list of AgentHarness resources.
type AgentHarnessList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentHarness `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, &AgentHarness{}, &AgentHarnessList{})
		return nil
	})
}
