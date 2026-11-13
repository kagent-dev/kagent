package declarative

import (
	"fmt"

	"sigs.k8s.io/yaml"
)

// Parse decodes and validates an AgentSystem document in YAML or JSON.
// Unknown fields are errors so that a misspelled key cannot silently change
// behavior. Use Registry.Parse to also check that every name resolves.
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
