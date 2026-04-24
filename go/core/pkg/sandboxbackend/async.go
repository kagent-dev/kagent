package sandboxbackend

import (
	"context"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Handle is the opaque identifier an AsyncBackend uses to address a sandbox
// it owns on an external control plane. Persisted in Sandbox.Status.BackendRef.
type Handle struct {
	ID string
}

// EnsureResult is returned by EnsureSandbox. Endpoint (if set) is surfaced
// to users via Sandbox.Status.Connection.
type EnsureResult struct {
	Handle   Handle
	Endpoint string
}

// AsyncBackend is the minimal surface a gRPC/HTTP-driven sandbox control
// plane must implement to back the kagent.dev/v1alpha2 Sandbox CRD. It is
// deliberately separate from Backend (which serves SandboxAgent's in-cluster
// agent-runtime flow).
type AsyncBackend interface {
	// Name identifies the backend for Sandbox.Status.BackendRef.Backend
	// and logging.
	Name() v1alpha2.SandboxBackendType

	// EnsureSandbox creates the sandbox on the backend if it does not
	// already exist. Implementations must be idempotent — if a sandbox
	// matching sbx.Name is already present, return its current handle.
	EnsureSandbox(ctx context.Context, sbx *v1alpha2.Sandbox) (EnsureResult, error)

	// GetStatus returns a Ready condition (status, reason, message) for
	// the sandbox identified by h. Used to refresh Sandbox.Status after
	// each reconcile.
	GetStatus(ctx context.Context, h Handle) (metav1.ConditionStatus, string, string)

	// DeleteSandbox releases the sandbox. NotFound must be treated as
	// success so the finalizer can be removed idempotently.
	DeleteSandbox(ctx context.Context, h Handle) error
}
