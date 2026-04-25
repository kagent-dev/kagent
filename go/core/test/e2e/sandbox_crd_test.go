package e2e_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestE2ESandboxCRDWithOpenShellBackend(t *testing.T) {
	cli := setupK8sClient(t, false)

	sandbox := &v1alpha2.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-sandbox-",
			Namespace:    utils.GetResourceNamespace(),
		},
		Spec: v1alpha2.SandboxSpec{
			Backend: v1alpha2.SandboxBackendOpenshell,
			Env: []corev1.EnvVar{{
				Name:  "KAGENT_E2E_SANDBOX",
				Value: "true",
			}},
		},
	}
	require.NoError(t, cli.Create(t.Context(), sandbox))
	cleanup(t, cli, sandbox)

	waitForSandboxReady(t, cli, sandbox)

	require.NotNil(t, sandbox.Status.BackendRef)
	require.Equal(t, v1alpha2.SandboxBackendOpenshell, sandbox.Status.BackendRef.Backend)
	require.Equal(t, fmt.Sprintf("%s-%s", sandbox.Namespace, sandbox.Name), sandbox.Status.BackendRef.ID)
	require.NotNil(t, sandbox.Status.Connection)
	require.Contains(t, sandbox.Status.Connection.Endpoint, sandbox.Status.BackendRef.ID)

	require.NoError(t, cli.Delete(t.Context(), sandbox))
	waitForSandboxDeleted(t, cli, sandbox)
}

func waitForSandboxReady(t *testing.T, cli client.Client, sandbox *v1alpha2.Sandbox) {
	t.Helper()

	require.NoError(t, wait.PollUntilContextTimeout(t.Context(), 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := cli.Get(ctx, client.ObjectKeyFromObject(sandbox), sandbox); err != nil {
			return false, err
		}
		accepted := meta.FindStatusCondition(sandbox.Status.Conditions, v1alpha2.SandboxConditionTypeAccepted)
		ready := meta.FindStatusCondition(sandbox.Status.Conditions, v1alpha2.SandboxConditionTypeReady)
		if accepted == nil || accepted.Status != metav1.ConditionTrue {
			return false, nil
		}
		return sandbox.Status.BackendRef != nil &&
			sandbox.Status.Connection != nil &&
			ready != nil &&
			ready.Status == metav1.ConditionTrue, nil
	}))
}

func waitForSandboxDeleted(t *testing.T, cli client.Client, sandbox *v1alpha2.Sandbox) {
	t.Helper()

	require.NoError(t, wait.PollUntilContextTimeout(t.Context(), time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		err := cli.Get(ctx, client.ObjectKeyFromObject(sandbox), &v1alpha2.Sandbox{})
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}))
}
