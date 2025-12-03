package common

import (
	"fmt"
	"strings"
)

// Generator defines the interface that all framework generators must implement
type Generator interface {
	Generate(projectDir, agentName, instruction, modelProvider, modelName, description string, verbose bool, kagentVersion string) error
	GetFrameworkName() string
	GetLanguage() string
}

// GeneratorRegistry manages available framework generators
type GeneratorRegistry struct {
	generators map[string]Generator
	validator  *FrameworkValidator
}

// NewGeneratorRegistry creates a new registry
func NewGeneratorRegistry() *GeneratorRegistry {
	return &GeneratorRegistry{
		generators: make(map[string]Generator),
		validator:  NewFrameworkValidator(),
	}
}

// Register adds a generator to the registry
func (r *GeneratorRegistry) Register(gen Generator) error {
	framework := strings.ToLower(gen.GetFrameworkName())

	// Validate framework is supported
	if err := r.validator.Validate(framework); err != nil {
		return fmt.Errorf("cannot register unsupported framework: %w", err)
	}

	r.generators[framework] = gen
	return nil
}

// Get retrieves a generator by framework name
func (r *GeneratorRegistry) Get(framework string) (Generator, error) {
	framework = strings.ToLower(framework)

	// Validate framework
	if err := r.validator.Validate(framework); err != nil {
		return nil, err
	}

	gen, exists := r.generators[framework]
	if !exists {
		return nil, fmt.Errorf("no generator registered for framework '%s'", framework)
	}

	return gen, nil
}

// List returns all registered framework names
func (r *GeneratorRegistry) List() []string {
	frameworks := make([]string, 0, len(r.generators))
	for framework := range r.generators {
		frameworks = append(frameworks, framework)
	}
	return frameworks
}
