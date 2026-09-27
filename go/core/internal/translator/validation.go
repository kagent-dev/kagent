package translator

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
)

// WorkerPoolNotFoundError identifies an unresolved Harness capacity reference.
type WorkerPoolNotFoundError struct {
	WorkerPool types.NamespacedName
}

func (e *WorkerPoolNotFoundError) Error() string {
	return fmt.Sprintf("WorkerPool %q not found", e.WorkerPool.String())
}

// ValidationError marks a resolved but unsupported public configuration. The
// controller reports these as a compatibility condition instead of retrying
// them like transient Kubernetes lookup failures.
type ValidationError struct {
	Err error
}

// Error implements error.
func (e *ValidationError) Error() string {
	return e.Err.Error()
}

// Unwrap preserves the formatted cause for errors.Is and errors.As callers.
func (e *ValidationError) Unwrap() error {
	return e.Err
}

// NewValidationError reports invalid public API configuration.
func NewValidationError(format string, args ...any) error {
	return &ValidationError{Err: fmt.Errorf(format, args...)}
}
