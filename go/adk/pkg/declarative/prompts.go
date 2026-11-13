package declarative

import (
	"fmt"
	"os"
)

// PromptResolver resolves one prompt source of a document. It returns either
// the prompt text, which is inlined, or a replacement source that the runtime
// resolves later, for example an Env source backed by a Secret.
type PromptResolver func(path string, src TextSource) (text string, replacement *TextSource, err error)

// ResolvePrompts replaces every instruction_from and global_instruction_from
// of spec using resolve. The kagent controller uses it to turn ConfigMap and
// Secret references into text or Secret-backed environment variables before
// it delivers the document. Errors carry the field path of the source.
func ResolvePrompts(spec *Spec, resolve PromptResolver) error {
	if spec == nil {
		return nil
	}
	for i := range spec.Agents {
		a := &spec.Agents[i]
		path := fmt.Sprintf("agents[%d]", i)
		if err := resolveLLMPrompts(path, &a.LLMSpec, resolve); err != nil {
			return err
		}
		if err := resolveNodePrompts(path, a.Nodes, resolve); err != nil {
			return err
		}
	}
	return nil
}

func resolveNodePrompts(path string, nodes []NodeSpec, resolve PromptResolver) error {
	for i := range nodes {
		n := &nodes[i]
		p := fmt.Sprintf("%s.nodes[%d]", path, i)
		if err := resolveLLMPrompts(p, &n.LLMSpec, resolve); err != nil {
			return err
		}
		if err := resolveNodePrompts(p, n.Nodes, resolve); err != nil {
			return err
		}
	}
	return nil
}

func resolveLLMPrompts(path string, l *LLMSpec, resolve PromptResolver) error {
	for _, f := range []struct {
		name string
		text *string
		src  **TextSource
	}{
		{"instruction_from", &l.Instruction, &l.InstructionFrom},
		{"global_instruction_from", &l.GlobalInstruction, &l.GlobalInstructionFrom},
	} {
		if *f.src == nil {
			continue
		}
		p := path + "." + f.name
		text, replacement, err := resolve(p, **f.src)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if replacement != nil {
			*f.src = replacement
			continue
		}
		*f.text, *f.src = text, nil
	}
	return nil
}

// sourcedText returns the effective text of an inline or sourced prompt. The
// runtime only resolves Env sources; Kubernetes references must have been
// resolved by the controller, which Registry.Validate enforces.
func sourcedText(inline string, src *TextSource) (string, error) {
	if src == nil {
		return inline, nil
	}
	if src.Env == "" {
		return "", fmt.Errorf("prompt source must be resolved by the kagent controller before the runtime builds the document")
	}
	text, ok := os.LookupEnv(src.Env) //nolint:forbidigo // Controller-generated Secret-backed prompt variables, not user configuration.
	if !ok {
		return "", fmt.Errorf("prompt environment variable %q is not set", src.Env)
	}
	return text, nil
}
