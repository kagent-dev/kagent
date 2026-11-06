package declarative

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"sigs.k8s.io/yaml"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

// Parse decodes and validates an AgentSystem document. Unknown fields are
// errors so that a misspelled key cannot silently change behavior.
//
// Documents that use node types added with Registry.RegisterNodeType must be
// parsed with Registry.Parse instead.
func Parse(data []byte) (*Spec, error) {
	spec, err := decode(data)
	if err != nil {
		return nil, err
	}
	if err := Validate(spec); err != nil {
		return nil, err
	}
	return spec, nil
}

func decode(data []byte) (*Spec, error) {
	var spec Spec
	if err := yaml.UnmarshalStrict(data, &spec); err != nil {
		return nil, fmt.Errorf("failed to decode %s document: %w", KindAgentSystem, err)
	}
	return &spec, nil
}

// ParseConfig decodes the document that the controller validated and delivered
// in the runtime config, and validates it against r. It refuses a document
// whose digest differs from the one the controller recorded, so a corrupted or
// swapped config fails loudly instead of running different behavior.
func (r *Registry) ParseConfig(c *configv1.ADKWorkflowConfig) (*Spec, error) {
	if c == nil {
		return nil, fmt.Errorf("workflow config is required")
	}
	sum := sha256.Sum256(c.Document)
	if got := hex.EncodeToString(sum[:]); got != c.Sha256 {
		return nil, fmt.Errorf("workflow document digest %s does not match the recorded digest %s", got, c.Sha256)
	}
	return r.Parse(c.Document)
}
