package translator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	atev1 "go.ate.dev/substrate/api/infra/k8s/ate/v1"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/api/bootstrap"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
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

// CompileResult contains immutable runtime inputs and non-blocking diagnostics.
// Diagnostics do not enter the revision's identity.
type CompileResult struct {
	Revision
	Warnings []string
}

// Revision is the resolved runtime configuration for one immutable revision.
type Revision struct {
	ControlPlaneURL string

	// These fields identify the Agent that produced the revision.
	AgentName string
	AgentUID  string
	Namespace string

	// Image and Environment describe the runtime container.
	Image       string
	Command     []string
	Args        []string
	Environment []corev1.EnvVar
	CPU         string
	Memory      string
	// ConfigJSON is delivered in the immutable bootstrap bundle.
	// AgentCard stays typed until a runtime or public protocol boundary renders it.
	ConfigJSON  []byte
	AgentCard   *a2apb.AgentCard
	SkillImages []SkillImage

	// WorkerPoolName, SandboxClass and SnapshotLocation control Substrate placement and state.
	WorkerPoolName   string
	SandboxClass     atev1.SandboxClass
	SnapshotLocation string
	// WorkerPoolNamespace is the pool's namespace; empty means Namespace.
	WorkerPoolNamespace string

	// Provenance records non-secret source configuration for diagnostics. Revision
	// identity comes from compiled runtime inputs, not this source snapshot.
	Provenance json.RawMessage
	// Credentials contains references resolved by the egress gateway.
	Credentials []egress.Credential
	// TokenPassthroughTargets reserve their origins against Secret credentials.
	TokenPassthroughTargets []egresspolicy.GatewayTarget
	// EgressDestinations is the HTTP(S) origin allowlist required by this revision.
	EgressDestinations []string
}

// PoolNamespace returns the namespace of the WorkerPool that places this revision.
func (r Revision) PoolNamespace() string {
	if r.WorkerPoolNamespace != "" {
		return r.WorkerPoolNamespace
	}
	return r.Namespace
}

// Equals compares the Agent Card's contents without inspecting protobuf caches.
func (r Revision) Equals(other Revision) bool {
	if !proto.Equal(r.AgentCard, other.AgentCard) {
		return false
	}
	r.AgentCard, other.AgentCard = nil, nil
	return reflect.DeepEqual(r, other)
}

// Digest returns the immutable identity of every input that affects runtime
// behavior. The full digest is the database key; Kubernetes names use a short
// prefix only for readability.
func (r *Revision) Digest() (RevisionID, error) {
	sandboxClass := r.SandboxClass
	switch sandboxClass {
	case "", atev1.SandboxClassGvisor:
		sandboxClass = atev1.SandboxClassGvisor
	case atev1.SandboxClassMicroVM:
	default:
		return RevisionID{}, fmt.Errorf("unsupported sandbox class %q", sandboxClass)
	}
	raw, err := json.Marshal(struct {
		BootstrapVersion        uint32                       `json:"bootstrapVersion"`
		ControlPlaneURL         string                       `json:"controlPlaneURL"`
		AgentName               string                       `json:"agentName"`
		AgentUID                string                       `json:"agentUID"`
		Namespace               string                       `json:"namespace"`
		CPU                     string                       `json:"cpu"`
		Memory                  string                       `json:"memory"`
		Image                   string                       `json:"image"`
		Command                 []string                     `json:"command,omitempty"`
		Args                    []string                     `json:"args,omitempty"`
		Environment             []corev1.EnvVar              `json:"environment"`
		ConfigJSON              json.RawMessage              `json:"config"`
		SkillImages             []SkillImage                 `json:"skillImages,omitempty"`
		WorkerPoolName          string                       `json:"workerPoolName"`
		SnapshotLocation        string                       `json:"snapshotLocation"`
		Credentials             []egress.Credential          `json:"credentials,omitempty"`
		TokenPassthroughTargets []egresspolicy.GatewayTarget `json:"tokenPassthroughTargets,omitempty"`
		EgressDestinations      []string                     `json:"egressDestinations"`
		SandboxClass            atev1.SandboxClass           `json:"sandboxClass"`

		// Placement now matches the pool's namespace as well as its name.
		// Hash it explicitly so existing immutable templates do not conflict after upgrades.
		WorkerPoolNamespace string `json:"workerPoolNamespace"`
	}{
		BootstrapVersion: bootstrap.FormatVersion, ControlPlaneURL: r.ControlPlaneURL,
		AgentName: r.AgentName, AgentUID: r.AgentUID, Namespace: r.Namespace,
		CPU: r.CPU, Memory: r.Memory,
		Image: r.Image, Command: r.Command, Args: r.Args, Environment: r.Environment, ConfigJSON: r.ConfigJSON,
		SkillImages:    r.SkillImages,
		WorkerPoolName: r.WorkerPoolName, SnapshotLocation: r.SnapshotLocation,
		TokenPassthroughTargets: r.TokenPassthroughTargets, Credentials: r.Credentials, EgressDestinations: r.EgressDestinations,
		SandboxClass: sandboxClass, WorkerPoolNamespace: r.PoolNamespace(),
	})
	if err != nil {
		return RevisionID{}, fmt.Errorf("marshal runtime revision inputs: %w", err)
	}
	card, err := proto.MarshalOptions{Deterministic: true}.Marshal(r.AgentCard)
	if err != nil {
		return RevisionID{}, fmt.Errorf("marshal runtime revision Agent Card: %w", err)
	}
	return RevisionID(sha256.Sum256(append(raw, card...))), nil
}

// BootstrapBundle renders the immutable payload served to a revision's actors.
func (r *Revision) BootstrapBundle(id RevisionID) (*runtimev1.BootstrapBundle, error) {
	card, err := apia2a.FromProtoAgentCard(r.AgentCard)
	if err != nil {
		return nil, fmt.Errorf("convert bootstrap Agent Card: %w", err)
	}
	// Keep the rendered bytes: re-rendering the structured card after an upgrade
	// could change JSON encoding and invalidate the descriptor digest.
	raw, err := json.Marshal(card)
	if err != nil {
		return nil, fmt.Errorf("render bootstrap Agent Card: %w", err)
	}
	return bootstrap.NewBundle(id.String(), r.ConfigJSON, raw)
}
