package translator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// Revision is the resolved runtime configuration for one immutable revision.
type Revision struct {
	// These fields identify the public attachment that produced the revision.
	Namespace         string
	AgentTemplateName string
	HarnessName       string

	// Image and Environment describe the runtime container.
	Image       string
	Environment []corev1.EnvVar
	// ConfigJSON and AgentCardJSON are injected into that container verbatim.
	ConfigJSON    []byte
	AgentCardJSON []byte

	// WorkerPoolName and SnapshotLocation control Substrate placement and state.
	WorkerPoolName   string
	SnapshotLocation string

	// SourceSnapshot is provenance safe to persist for debugging. Secret values
	// are represented only by hashes.
	SourceSnapshot json.RawMessage
	// SecretHashes makes credential rotation produce a new revision without
	// embedding credential values in the revision.
	SecretHashes []byte
	// EgressDestinations is the hostname allowlist required by this revision.
	EgressDestinations []string
}

// Digest returns the immutable identity of every input that affects runtime
// behavior. The full digest is the database key; Kubernetes names use a short
// prefix only for readability.
func (r *Revision) Digest() (string, error) {
	if r == nil {
		return "", fmt.Errorf("runtime revision is required")
	}
	raw, err := json.Marshal(struct {
		Namespace          string          `json:"namespace"`
		AgentTemplateName  string          `json:"agentTemplateName"`
		HarnessName        string          `json:"harnessName"`
		Image              string          `json:"image"`
		Environment        []corev1.EnvVar `json:"environment"`
		ConfigJSON         json.RawMessage `json:"config"`
		AgentCardJSON      json.RawMessage `json:"agentCard"`
		WorkerPoolName     string          `json:"workerPoolName"`
		SnapshotLocation   string          `json:"snapshotLocation"`
		SourceSnapshot     json.RawMessage `json:"sourceSnapshot"`
		SecretHashes       string          `json:"secretHashes"`
		EgressDestinations []string        `json:"egressDestinations"`
	}{
		Namespace: r.Namespace, AgentTemplateName: r.AgentTemplateName, HarnessName: r.HarnessName,
		Image: r.Image, Environment: r.Environment, ConfigJSON: r.ConfigJSON, AgentCardJSON: r.AgentCardJSON,
		WorkerPoolName: r.WorkerPoolName, SnapshotLocation: r.SnapshotLocation, SourceSnapshot: r.SourceSnapshot,
		SecretHashes: hex.EncodeToString(r.SecretHashes), EgressDestinations: r.EgressDestinations,
	})
	if err != nil {
		return "", fmt.Errorf("marshal runtime revision inputs: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
