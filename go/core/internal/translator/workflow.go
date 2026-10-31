package translator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kagent-dev/kagent/go/adk/pkg/declarative"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// maxWorkflowBytes bounds a declarative workflow document, before and after
// canonicalization. The runtime config carries it in a bounded protobuf field.
const maxWorkflowBytes = 64 << 10

// ResolvedWorkflow is a declarative workflow document that passed structural
// validation, in the canonical form delivered to the runtime. Source is the
// ConfigMap key it was read from; Prompts are the ConfigMap keys whose text
// was inlined for instruction_from and global_instruction_from.
type ResolvedWorkflow struct {
	Document []byte
	SHA256   string
	Source   *ResolvedConfigMapKeyReference
	Prompts  []*ResolvedConfigMapKeyReference
}

// ConfigMapNames returns the ConfigMaps the workflow was compiled from.
func (w *ResolvedWorkflow) ConfigMapNames() []string {
	if w == nil {
		return nil
	}
	var names []string
	if w.Source != nil {
		names = append(names, w.Source.NamespacedName.Name)
	}
	for _, p := range w.Prompts {
		names = append(names, p.NamespacedName.Name)
	}
	return names
}

// promptReferenceError is a prompt ConfigMap lookup failure. The ConfigMap
// may appear later, so it is retried rather than reported as incompatible.
type promptReferenceError struct{ err error }

func (e *promptReferenceError) Error() string { return e.err.Error() }
func (e *promptReferenceError) Unwrap() error { return e.err }

// resolveWorkflow reads and validates the template's workflow document. It
// returns nil when the template declares none.
func (c *Compiler) resolveWorkflow(_ context.Context, template *TemplateConfiguration) (*ResolvedWorkflow, error) {
	ref := template.Spec.WorkflowFrom
	if ref == nil {
		return nil, nil
	}
	raw, source, err := c.resolveConfigMapKey(template.Namespace, "workflow", ref)
	if err != nil {
		return nil, err
	}
	var prompts []*ResolvedConfigMapKeyReference
	resolve := func(_ string, src declarative.TextSource) (string, *declarative.TextSource, error) {
		if src.ConfigMap == nil {
			return "", nil, unsupportedPromptSource(src)
		}
		text, prompt, err := c.resolveConfigMapKey(template.Namespace, "workflow prompt", &v1alpha3.ConfigMapKeyReference{Name: src.ConfigMap.Name, Key: src.ConfigMap.Key})
		if err != nil {
			return "", nil, &promptReferenceError{err: err}
		}
		prompts = append(prompts, prompt)
		return string(text), nil, nil
	}
	canonical, err := validateWorkflow(raw, resolve)
	if err != nil {
		if _, ok := errors.AsType[*promptReferenceError](err); ok {
			return nil, fmt.Errorf("workflow: %w", err)
		}
		return nil, NewValidationError("invalid workflow: %v", err)
	}
	digest := sha256.Sum256(canonical)
	return &ResolvedWorkflow{Document: canonical, SHA256: hex.EncodeToString(digest[:]), Source: source, Prompts: prompts}, nil
}

// unsupportedPromptSource explains why the controller does not deliver a
// Secret or environment prompt source. Secrets never enter a runtime: kagent
// injects credentials only at the egress gateway, so prompt text that must be
// visible to the runtime belongs in a ConfigMap. Env sources are reserved for
// what the runtime image itself sets and are not resolvable here.
func unsupportedPromptSource(src declarative.TextSource) error {
	if src.Secret != nil {
		return fmt.Errorf("secret %q: prompts cannot be read from Secrets because kagent never delivers Secret values to a runtime; use a ConfigMap", src.Secret.Name)
	}
	return fmt.Errorf("env %q: environment prompt sources are not supported in a delivered workflow; use a ConfigMap", src.Env)
}

// validateWorkflow checks a document with the same structural rules the Go ADK
// applies before building it, resolves its prompt sources with resolve, and
// returns its canonical JSON. Names that only
// the runtime image can resolve (functions, tools, models) are not checked
// here; the runtime reports them at startup.
func validateWorkflow(raw []byte, resolve declarative.PromptResolver) ([]byte, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("document is empty")
	}
	if len(raw) > maxWorkflowBytes {
		return nil, fmt.Errorf("document exceeds %d bytes", maxWorkflowBytes)
	}
	spec, err := declarative.Parse(raw)
	if err != nil {
		return nil, err
	}
	if err := declarative.ResolvePrompts(spec, resolve); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("canonicalize document: %w", err)
	}
	if len(canonical) > maxWorkflowBytes {
		return nil, fmt.Errorf("canonical document exceeds %d bytes", maxWorkflowBytes)
	}
	return canonical, nil
}
