// Package egressext defines outbound policy extensions.
package egressext

import "k8s.io/apimachinery/pkg/types"

// AuthenticationRequirement describes an application's authentication at an origin.
// A nonempty Header requires that header and prefix for new bindings.
type AuthenticationRequirement struct {
	Origin string
	Header string
	Prefix string
	// Preserve prevents additional bindings at this origin.
	Preserve bool
	// Required requires a gateway binding before the client can be rendered.
	Required bool
}

// Credential binds a provider reference to an outbound origin and header.
type Credential struct {
	Origin string
	Header string
	Prefix string
	URI    string
}

// PolicyDraft contains application destinations, authentication requirements and credential references.
// It contains no credential values. The extension can only add Credentials.
type PolicyDraft struct {
	Destinations []string
	Requirements []AuthenticationRequirement
	Credentials  []Credential
}

// PolicyTranslator resolves policy before harness configuration is rendered.
// Implementations must be deterministic and safe for concurrent calls, with no I/O.
// Core rejects changed destinations, requirements, and existing bindings.
type PolicyTranslator interface {
	// agent identifies the runnable Agent, not its referenced AgentTemplate.
	Translate(agent types.NamespacedName, draft PolicyDraft) (PolicyDraft, error)
}
