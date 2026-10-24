package translator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/kagent-dev/kagent/go/adk/pkg/declarative"
)

// maxWorkflowBytes bounds a declarative workflow document, before and after
// canonicalization. The runtime config carries it in a bounded protobuf field.
const maxWorkflowBytes = 64 << 10

// ResolvedWorkflow is a declarative workflow document that passed structural
// validation, in the canonical form delivered to the runtime. Source is the
// ConfigMap key it was read from.
type ResolvedWorkflow struct {
	Document []byte
	SHA256   string
	Source   *ResolvedConfigMapKeyReference
}

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
	canonical, err := validateWorkflow(raw)
	if err != nil {
		return nil, NewValidationError("invalid workflow: %v", err)
	}
	digest := sha256.Sum256(canonical)
	return &ResolvedWorkflow{Document: canonical, SHA256: hex.EncodeToString(digest[:]), Source: source}, nil
}

// validateWorkflow checks a document with the same structural rules the Go ADK
// applies before building it, and returns its canonical JSON. Names that only
// the runtime image can resolve (functions, tools, models) are not checked
// here; the runtime reports them at startup.
func validateWorkflow(raw []byte) ([]byte, error) {
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
	canonical, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("canonicalize document: %w", err)
	}
	if len(canonical) > maxWorkflowBytes {
		return nil, fmt.Errorf("canonical document exceeds %d bytes", maxWorkflowBytes)
	}
	return canonical, nil
}
