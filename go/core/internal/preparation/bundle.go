package preparation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// Bundle is the resolved, in-memory runtime input for one prepared revision.
// Provisioning-specific code translates it to the current Substrate resource.
type Bundle struct {
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

// Revision returns the immutable identity of every input that affects runtime behavior.
func (b *Bundle) Revision() (string, error) {
	if b == nil {
		return "", fmt.Errorf("preparation bundle is required")
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
		Namespace:          b.Namespace,
		AgentTemplateName:  b.AgentTemplateName,
		HarnessName:        b.HarnessName,
		Image:              b.Image,
		Environment:        b.Environment,
		ConfigJSON:         b.ConfigJSON,
		AgentCardJSON:      b.AgentCardJSON,
		WorkerPoolName:     b.WorkerPoolName,
		SnapshotLocation:   b.SnapshotLocation,
		SourceSnapshot:     b.SourceSnapshot,
		SecretHashes:       hex.EncodeToString(b.SecretHashes),
		EgressDestinations: b.EgressDestinations,
	})
	if err != nil {
		return "", fmt.Errorf("marshal prepared revision inputs: %w", err)
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
