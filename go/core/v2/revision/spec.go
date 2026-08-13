package revision

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// Spec is the resolved runtime configuration for one immutable revision.
type Spec struct {
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
func (s *Spec) Digest() (string, error) {
	if s == nil {
		return "", fmt.Errorf("runtime revision spec is required")
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
		Namespace: s.Namespace, AgentTemplateName: s.AgentTemplateName, HarnessName: s.HarnessName,
		Image: s.Image, Environment: s.Environment, ConfigJSON: s.ConfigJSON, AgentCardJSON: s.AgentCardJSON,
		WorkerPoolName: s.WorkerPoolName, SnapshotLocation: s.SnapshotLocation, SourceSnapshot: s.SourceSnapshot,
		SecretHashes: hex.EncodeToString(s.SecretHashes), EgressDestinations: s.EgressDestinations,
	})
	if err != nil {
		return "", fmt.Errorf("marshal runtime revision inputs: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// ActorTemplateRef identifies the Kubernetes ActorTemplate provisioned for a revision.
type ActorTemplateRef struct {
	Namespace      string
	Name           string
	UID            string
	Phase          string
	GoldenSnapshot string
}
