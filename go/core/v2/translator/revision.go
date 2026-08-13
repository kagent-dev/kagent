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
	Namespace          string
	AgentTemplateName  string
	HarnessName        string
	Image              string
	Environment        []corev1.EnvVar
	ConfigJSON         []byte
	AgentCardJSON      []byte
	WorkerPoolName     string
	SnapshotLocation   string
	SourceSnapshot     json.RawMessage
	SecretHashes       []byte
	EgressDestinations []string
}

// Digest returns the immutable identity of every input that affects runtime behavior.
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
