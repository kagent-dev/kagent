package agent_test

import (
	"context"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type testSandboxBackend struct{}

func (testSandboxBackend) BuildSandbox(context.Context, sandboxbackend.BuildInput) ([]client.Object, error) {
	return nil, nil
}

func (testSandboxBackend) GetOwnedResourceTypes() []client.Object { return nil }

func (testSandboxBackend) OwnedResourceTypesFor(*v1alpha2.SandboxAgent) ([]client.Object, error) {
	return nil, nil
}

func (testSandboxBackend) SessionDBURL(*v1alpha2.SandboxAgent) string {
	return "sqlite+aiosqlite:////durable/session.db"
}

func (testSandboxBackend) ComputeReady(context.Context, client.Client, types.NamespacedName) (metav1.ConditionStatus, string, string) {
	return metav1.ConditionTrue, "WorkloadReady", "ready"
}
