package translator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

const shortRevisionBytes = 6

// RevisionID is the SHA-256 identity of a compiled runtime revision. Keeping
// the digest as a fixed-size value makes invalid lengths unrepresentable.
type RevisionID [sha256.Size]byte

// String returns the full database identity.
func (id RevisionID) String() string { return hex.EncodeToString(id[:]) }

// Short returns the readable prefix used in Kubernetes names and labels.
func (id RevisionID) Short() string { return hex.EncodeToString(id[:shortRevisionBytes]) }

// IsZero reports whether compilation has not produced an identity.
func (id RevisionID) IsZero() bool { return id == RevisionID{} }

// Compilation contains one immutable runtime revision and the non-blocking
// diagnostics produced while compiling it. Diagnostics are deliberately kept
// outside Revision because they do not describe runtime behavior.
type Compilation struct {
	Revision
	Warnings []string
}

// Revision is the resolved runtime configuration for one immutable revision.
type Revision struct {
	// These fields identify the public attachment that produced the revision.
	Namespace         string
	AgentTemplateName string
	HarnessName       string

	// Image and Environment describe the runtime container.
	Image       string
	Command     []string
	Args        []string
	Environment []corev1.EnvVar
	// ConfigJSON and AgentCardJSON are injected into that container verbatim.
	ConfigJSON    []byte
	AgentCardJSON []byte

	// WorkerPoolName and SnapshotLocation control Substrate placement and state.
	WorkerPoolName   string
	SnapshotLocation string

	// Provenance identifies every Kubernetes input to this revision. Secret
	// values are represented only by hashes.
	Provenance json.RawMessage
	// EgressDestinations is the hostname allowlist required by this revision.
	EgressDestinations []string
}

// Digest returns the immutable identity of every input that affects runtime
// behavior. The full digest is the database key; Kubernetes names use a short
// prefix only for readability.
func (r *Revision) Digest() (RevisionID, error) {
	raw, err := json.Marshal(struct {
		Namespace          string          `json:"namespace"`
		AgentTemplateName  string          `json:"agentTemplateName"`
		HarnessName        string          `json:"harnessName"`
		Image              string          `json:"image"`
		Command            []string        `json:"command,omitempty"`
		Args               []string        `json:"args,omitempty"`
		Environment        []corev1.EnvVar `json:"environment"`
		ConfigJSON         json.RawMessage `json:"config"`
		AgentCardJSON      json.RawMessage `json:"agentCard"`
		WorkerPoolName     string          `json:"workerPoolName"`
		SnapshotLocation   string          `json:"snapshotLocation"`
		Provenance         json.RawMessage `json:"provenance"`
		EgressDestinations []string        `json:"egressDestinations"`
	}{
		Namespace: r.Namespace, AgentTemplateName: r.AgentTemplateName, HarnessName: r.HarnessName,
		Image: r.Image, Command: r.Command, Args: r.Args, Environment: r.Environment, ConfigJSON: r.ConfigJSON, AgentCardJSON: r.AgentCardJSON,
		WorkerPoolName: r.WorkerPoolName, SnapshotLocation: r.SnapshotLocation, Provenance: r.Provenance,
		EgressDestinations: r.EgressDestinations,
	})
	if err != nil {
		return RevisionID{}, fmt.Errorf("marshal runtime revision inputs: %w", err)
	}
	return RevisionID(sha256.Sum256(raw)), nil
}
