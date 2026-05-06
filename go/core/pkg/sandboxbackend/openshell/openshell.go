// Package openshell implements sandboxbackend.AsyncBackend against an external
// OpenShell gateway over gRPC.
//
// Use Dial to obtain OpenShellClients (shared connection for openshell.v1.OpenShell
// and openshell.inference.v1.Inference).
//
// • NewOpenshellBackend — generic Sandbox resources with spec.backend=openshell:
// user image/env mapping per translate.go; spec.modelConfigRef is ignored for
// gateway registration and post-ready bootstrap.
//
// • NewOpenClawBackend — equivalent backend that pins the
// sandbox image to NemoclawSandboxBaseImage, register gateway providers when
// modelConfigRef is set, and run OpenClaw bootstrap after Ready.
//
// Unlike agentsxk8s, these backends do not emit Kubernetes workload objects —
// sandbox lifecycle goes through the gateway over gRPC.
package openshell

import (
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// OpenshellBackend implements AsyncBackend for spec.backend=openshell (generic
// OpenShell sandbox provisioning without OpenClaw/Nemo bootstrap behavior).
type OpenshellBackend struct {
	*grpcBackend
}

var _ sandboxbackend.AsyncBackend = (*OpenshellBackend)(nil)

// NewOpenshellBackend returns a backend for Sandbox.spec.backend=openshell.
func NewOpenshellBackend(kubeClient client.Client, clients *OpenShellClients, cfg Config, recorder record.EventRecorder) *OpenshellBackend {
	return &OpenshellBackend{
		grpcBackend: newGRPCBackend(kubeClient, clients, cfg, recorder, v1alpha2.AgentHarnessBackendOpenshell, buildOpenshellCreateRequest, false),
	}
}
