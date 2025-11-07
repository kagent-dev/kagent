package frameworks

import (
	"fmt"

	adk_python "github.com/kagent-dev/kagent/go/cli/agent/frameworks/adk/python"
	"github.com/kagent-dev/kagent/go/cli/agent/frameworks/common"
)

// Generator interface for project generation
type Generator interface {
	Generate(agentConfig *common.AgentConfig) error
}

// NewGenerator creates a new generator for the specified framework and language
func NewGenerator(framework, language string) (Generator, error) {
	switch framework {
	case "adk":
		switch language {
		case "python":
			return adk_python.NewPythonGenerator(), nil
		default:
			return nil, fmt.Errorf("unsupported language '%s' for adk", language)
		}
	default:
		return nil, fmt.Errorf("unsupported framework: %s", framework)
	}
}
