package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/stretchr/testify/require"
)

// TestE2EAgentKindNameCollision verifies that an Agent and a SandboxAgent
// sharing a namespace/name are independently addressable: the merged list
// returns both with distinct kind-qualified ids, the per-kind routes resolve
// the right resource, and a per-kind delete does not touch the other kind.
func TestE2EAgentKindNameCollision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	kubeClient := setupK8sClient(t, false)

	const ns = "kagent"
	const name = "e2e-kind-collision"
	ref := ns + "/" + name

	spec := v1alpha2.AgentSpec{
		Type:        v1alpha2.AgentType_Declarative,
		Description: "e2e kind collision test agent",
		Declarative: &v1alpha2.DeclarativeAgentSpec{
			SystemMessage: "You are a test agent.",
			ModelConfig:   "default-model-config",
		},
	}

	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       spec,
	}
	sandboxAgent := &v1alpha2.SandboxAgent{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       v1alpha2.SandboxAgentSpec{AgentSpec: spec},
	}

	require.NoError(t, kubeClient.Create(ctx, agent))
	t.Cleanup(func() { _ = kubeClient.Delete(context.Background(), agent) })
	require.NoError(t, kubeClient.Create(ctx, sandboxAgent))
	t.Cleanup(func() { _ = kubeClient.Delete(context.Background(), sandboxAgent) })

	doJSON := func(method, path string, out any) int {
		req, err := http.NewRequestWithContext(ctx, method, kagentURL()+path, bytes.NewReader(nil))
		require.NoError(t, err)
		req.Header.Set("X-User-Id", "e2e-collision@test.local")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		if out != nil && resp.StatusCode == http.StatusOK {
			require.NoError(t, json.Unmarshal(body, out), "body: %s", body)
		}
		return resp.StatusCode
	}

	t.Run("merged list returns both kinds with distinct ids", func(t *testing.T) {
		var list api.StandardResponse[[]api.AgentResponse]
		code := doJSON(http.MethodGet, fmt.Sprintf("/api/agents?namespace=%s", ns), &list)
		require.Equal(t, http.StatusOK, code)

		ids := map[string]string{} // id -> kind
		for _, a := range list.Data {
			if a.Agent != nil && a.Agent.Metadata.Name == name {
				ids[a.ID] = a.Agent.Kind
			}
		}
		require.Len(t, ids, 2, "expected both kinds in the merged list, got %v", ids)
		require.Equal(t, "Agent", ids[utils.AgentDBID(utils.AgentKind, ref)])
		require.Equal(t, "SandboxAgent", ids[utils.AgentDBID(utils.SandboxAgentKind, ref)])
	})

	t.Run("per-kind get resolves the right resource", func(t *testing.T) {
		var got api.StandardResponse[api.AgentResponse]
		code := doJSON(http.MethodGet, fmt.Sprintf("/api/agents/%s/%s", ns, name), &got)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "Agent", got.Data.Agent.Kind)

		code = doJSON(http.MethodGet, fmt.Sprintf("/api/sandboxagents/%s/%s", ns, name), &got)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "SandboxAgent", got.Data.Agent.Kind)
	})

	t.Run("per-kind delete leaves the other kind intact", func(t *testing.T) {
		code := doJSON(http.MethodDelete, fmt.Sprintf("/api/sandboxagents/%s/%s", ns, name), nil)
		require.Equal(t, http.StatusOK, code)

		// Deletion completes asynchronously (the substrate controller clears its
		// finalizer), so poll for the CR to disappear.
		require.Eventually(t, func() bool {
			var sa v1alpha2.SandboxAgent
			err := kubeClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &sa)
			return apierrors.IsNotFound(err)
		}, 30*time.Second, 500*time.Millisecond, "SandboxAgent should be deleted")

		var still v1alpha2.Agent
		require.NoError(t, kubeClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &still),
			"the Agent sharing the name must survive a SandboxAgent-targeted delete")
	})
}
