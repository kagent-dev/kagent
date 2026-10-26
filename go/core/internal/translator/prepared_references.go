package translator

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"istio.io/istio/pkg/kube/krt"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// PreparedAgentReferences tracks inputs of the revision eligible for new sessions.
// It is restored from durable provenance, independently of the current Agent spec.
type PreparedAgentReferences struct {
	Namespace, AgentName, AgentUID string
	Revision                       string
	References                     []PreparedReference
	Error                          string
}

func (p PreparedAgentReferences) ResourceName() string { return p.Namespace + "/" + p.AgentName }

// PreparedReference is a resolved ModelConfig or RemoteMCPServer identity.
type PreparedReference struct {
	Kind string
	Name types.NamespacedName
}

// PreparedReferencesFromProvenance reads the resource identities recorded by all
// harness compilers. Reject malformed snapshots rather than losing policy checks.
func PreparedReferencesFromProvenance(data json.RawMessage, ownerNamespace string) ([]PreparedReference, error) {
	var snapshot struct {
		Inputs *[]struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			Name       string `json:"name"`
		} `json:"inputs"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("decode prepared revision provenance: %w", err)
	}
	if snapshot.Inputs == nil {
		return nil, fmt.Errorf("prepared revision provenance has no inputs")
	}
	var refs []PreparedReference
	seen := map[PreparedReference]bool{}
	for _, input := range *snapshot.Inputs {
		if input.Kind != "ModelConfig" && input.Kind != "RemoteMCPServer" {
			continue
		}
		if input.APIVersion != v1alpha3.GroupVersion.String() {
			return nil, fmt.Errorf("unexpected API version in prepared %s reference", input.Kind)
		}
		namespace, name, qualified := strings.Cut(input.Name, "/")
		if !qualified {
			// Local-only revisions recorded before namespace support used bare names.
			namespace, name = ownerNamespace, input.Name
		}
		if len(validation.IsDNS1123Label(namespace)) != 0 || len(validation.IsDNS1123Subdomain(name)) != 0 {
			return nil, fmt.Errorf("invalid prepared %s reference %q", input.Kind, input.Name)
		}
		ref := PreparedReference{Kind: input.Kind, Name: types.NamespacedName{Namespace: namespace, Name: name}}
		if !seen[ref] {
			refs = append(refs, ref)
			seen[ref] = true
		}
	}
	return refs, nil
}

func preparedReferencePolicies(ctx krt.HandlerContext, inputs Collections, prepared *PreparedAgentReferences) []referencePolicy {
	var policies []referencePolicy
	for _, ref := range prepared.References {
		switch ref.Kind {
		case "ModelConfig":
			model := krt.FetchOne(ctx, inputs.ResolvedModelConfigs, krt.FilterObjectName(ref.Name))
			if model != nil {
				policies = append(policies, referencePolicy{ref.Kind, ref.Name, model.Config.Spec.AllowedNamespaces})
			}
		case "RemoteMCPServer":
			server := krt.FetchOne(ctx, inputs.RemoteMCPServers, krt.FilterObjectName(ref.Name))
			if server != nil {
				policies = append(policies, referencePolicy{ref.Kind, ref.Name, (*server).Spec.AllowedNamespaces})
			}
		}
	}
	return policies
}

// AuthorizePreparedReferences evaluates the retained revision even when its
// references no longer appear in the desired spec. Missing targets retain normal
// last-good behavior, but their recreation remains a tracked dependency.
func AuthorizePreparedReferences(ctx krt.HandlerContext, inputs Collections, prepared *PreparedAgentReferences) error {
	if prepared.Error != "" {
		return &ReferencePolicyError{Err: fmt.Errorf("prepared revision references: %s", prepared.Error)}
	}
	var failures []error
	for _, ref := range preparedReferencePolicies(ctx, inputs, prepared) {
		if err := authorizeReference(ctx, inputs, prepared.Namespace, ref.target, ref.policy); err != nil {
			failures = append(failures, fmt.Errorf("prepared revision %s references %s %q: %w", prepared.Revision, ref.kind, ref.target, err))
		}
	}
	return errors.Join(failures...)
}
