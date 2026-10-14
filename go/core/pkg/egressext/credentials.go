// Package egressext defines outbound credential extensions.
package egressext

import "k8s.io/apimachinery/pkg/types"

// CredentialTarget is an eligible HTTPS origin.
// A nonempty Header requires the supplied header and prefix.
type CredentialTarget struct {
	Origin string
	Header string
	Prefix string
}

// Credential binds a provider reference to an outbound origin and header.
type Credential struct {
	Origin string
	Header string
	Prefix string
	URI    string
}

// CredentialBinder adds bindings during compilation for eligible targets without replacing existing bindings.
// Implementations must be deterministic and safe for concurrent calls, with no I/O or input mutation.
type CredentialBinder interface {
	// agent identifies the runnable Agent, not its referenced AgentTemplate.
	Bindings(agent types.NamespacedName, targets []CredentialTarget, existing []Credential) ([]Credential, error)
}
