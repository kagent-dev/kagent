package translator

import (
	"context"
	"maps"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// Compiler resolves public API objects into a complete, immutable runtime
// revision. It owns the v2 translation boundary rather than delegating to an
// earlier API translator.
type Compiler struct {
	kube             Reader
	harnessCompilers map[HarnessType]HarnessCompiler
}

// HarnessType identifies the runtime selected by a Harness.
type HarnessType string

// Supported Harness runtime types.
const (
	HarnessTypeKagent HarnessType = "kagent"
	HarnessTypeCodex  HarnessType = "codex"
	HarnessTypeClaude HarnessType = "claude"
)

// HarnessCompiler converts resolved, harness-neutral inputs into one runtime revision.
type HarnessCompiler interface {
	Compile(context.Context, *HarnessInput) (*Revision, error)
}

// NewCompiler constructs the v2 runtime compiler.
func NewCompiler(kube Reader, harnessCompilers map[HarnessType]HarnessCompiler) *Compiler {
	return &Compiler{kube: kube, harnessCompilers: maps.Clone(harnessCompilers)}
}

// CompileAgentTemplate resolves an API v2 attachment into an immutable runtime
// revision. Nothing below this boundary needs to read the public API objects.
func (c *Compiler) CompileAgentTemplate(ctx context.Context, harness *v1alpha3.Harness, template *v1alpha3.AgentTemplate) (*Revision, error) {
	harnessCompiler := c.harnessCompilers[harnessType(harness)]
	if harnessCompiler == nil {
		return nil, NewValidationError("Harness runtime is not supported by any compiler")
	}
	tree, err := c.resolveTree(ctx, harness, template)
	if err != nil {
		return nil, err
	}
	input, err := c.buildInputs(ctx, tree)
	if err != nil {
		return nil, err
	}
	return harnessCompiler.Compile(ctx, input)
}

func harnessType(harness *v1alpha3.Harness) HarnessType {
	switch {
	case harness.Spec.Kagent != nil:
		return HarnessTypeKagent
	case harness.Spec.Codex != nil:
		return HarnessTypeCodex
	case harness.Spec.Claude != nil:
		return HarnessTypeClaude
	default:
		return ""
	}
}
