package controller

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/controller/apiclient"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

type referenceResult struct{ Name, Error string }

func (r referenceResult) ResourceName() string { return r.Name }

// Real admission, informers, Namespace watches and resourceNames-scoped RBAC.
func TestCrossNamespaceReferenceWatches(t *testing.T) {
	environment := &envtest.Environment{CRDDirectoryPaths: []string{"../../../../helm/kagent-crds/templates"}, ErrorIfCRDPathMissing: true}
	config, err := environment.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, environment.Stop()) })
	client, err := apiclient.New(config)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	for _, name := range []string{"team-a", "shared", "unwatched"} {
		_, err = client.Kube().CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"shared": "yes"}}}, metav1.CreateOptions{})
		require.NoError(t, err)
	}
	model := &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Name: "model", Namespace: "shared"}, Spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "example", AllowedNamespaces: &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromAll}}}
	model, err = client.Kagent().ApiV1alpha3().ModelConfigs("shared").Create(ctx, model, metav1.CreateOptions{})
	require.NoError(t, err)
	agent := &v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Name: "assistant", Namespace: "team-a"}, Spec: v1alpha3.AgentSpec{HarnessRef: corev1.LocalObjectReference{Name: "runner"}, Template: v1alpha3.AgentTemplateSource{Inline: &v1alpha3.AgentTemplateSpec{ModelConfig: &v1alpha3.ObjectReference{Name: "model", Namespace: "shared"}}}}}
	_, err = client.Kagent().ApiV1alpha3().Agents("team-a").Create(ctx, agent, metav1.CreateOptions{})
	require.NoError(t, err)
	opts := krt.NewOptionsBuilder(ctx.Done(), "references", nil)
	agents := typedCollection[*v1alpha3.Agent](client, []string{"team-a"}, "Agents", opts)
	models := typedCollection[*v1alpha3.ModelConfig](client, []string{"team-a", "shared"}, "Models", opts)
	inputs := translator.Collections{
		Secrets:          typedCollection[*corev1.Secret](client, []string{"team-a", "shared"}, "Secrets", opts),
		ConfigMaps:       typedCollection[*corev1.ConfigMap](client, []string{"team-a", "shared"}, "ConfigMaps", opts),
		Harnesses:        typedCollection[*v1alpha3.Harness](client, []string{"team-a"}, "Harnesses", opts),
		AgentTemplates:   typedCollection[*v1alpha3.AgentTemplate](client, []string{"team-a"}, "Templates", opts),
		RemoteMCPServers: typedCollection[*v1alpha3.RemoteMCPServer](client, []string{"team-a", "shared"}, "Servers", opts),
	}
	_, inputs.ResolvedModelConfigs = newModelConfigReconciliations(models, inputs.ConfigMaps, inputs.Secrets, opts)
	inputs.Namespaces = newReferenceNamespaces(client, agents, inputs, opts)
	results := krt.NewCollection(agents, func(hctx krt.HandlerContext, agent *v1alpha3.Agent) *referenceResult {
		_, err := translator.ResolveModelReference(hctx, inputs, agent.Namespace, *agent.Spec.Template.Inline.ModelConfig)
		result := &referenceResult{Name: agent.Name}
		if err != nil {
			result.Error = err.Error()
		}
		return result
	}, opts.WithName("Resolved")...)
	require.True(t, client.RunAndWait(ctx.Done()))
	t.Cleanup(client.Shutdown)
	expect := func(message string) {
		t.Helper()
		require.Eventually(t, func() bool {
			r := results.GetKey("assistant")
			return r != nil && (message == "" && r.Error == "" || message != "" && strings.Contains(r.Error, message))
		}, 10*time.Second, 10*time.Millisecond)
	}
	expect("")
	model.Spec.AllowedNamespaces = &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromSame}
	model, err = client.Kagent().ApiV1alpha3().ModelConfigs("shared").Update(ctx, model, metav1.UpdateOptions{})
	require.NoError(t, err)
	expect("denied")
	model.Spec.AllowedNamespaces = &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromSelector, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"shared": "yes"}}}
	model, err = client.Kagent().ApiV1alpha3().ModelConfigs("shared").Update(ctx, model, metav1.UpdateOptions{})
	require.NoError(t, err)
	expect("")
	namespace, err := client.Kube().CoreV1().Namespaces().Get(ctx, "team-a", metav1.GetOptions{})
	require.NoError(t, err)
	namespace.Labels = nil
	_, err = client.Kube().CoreV1().Namespaces().Update(ctx, namespace, metav1.UpdateOptions{})
	require.NoError(t, err)
	expect("denied")
	require.NoError(t, client.Kagent().ApiV1alpha3().ModelConfigs("shared").Delete(ctx, "model", metav1.DeleteOptions{}))
	expect("not found")
	model.ResourceVersion = ""
	model.UID = ""
	model.Spec.AllowedNamespaces = &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromAll}
	_, err = client.Kagent().ApiV1alpha3().ModelConfigs("shared").Create(ctx, model, metav1.CreateOptions{})
	require.NoError(t, err)
	expect("")

	// A persisted target outside watchNamespaces must remain unresolved.
	hidden := model.DeepCopy()
	hidden.Namespace = "unwatched"
	hidden.ResourceVersion, hidden.UID = "", ""
	_, err = client.Kagent().ApiV1alpha3().ModelConfigs("unwatched").Create(ctx, hidden, metav1.CreateOptions{})
	require.NoError(t, err)
	agent, err = client.Kagent().ApiV1alpha3().Agents("team-a").Get(ctx, "assistant", metav1.GetOptions{})
	require.NoError(t, err)
	agent.Spec.Template.Inline.ModelConfig.Namespace = "unwatched"
	_, err = client.Kagent().ApiV1alpha3().Agents("team-a").Update(ctx, agent, metav1.UpdateOptions{})
	require.NoError(t, err)
	expect("not found")

	t.Run("scoped namespace permission", func(t *testing.T) {
		userConfig := rest.CopyConfig(config)
		userConfig.Impersonate = rest.ImpersonationConfig{UserName: "namespace-observer"}
		restricted, err := apiclient.New(userConfig)
		require.NoError(t, err)
		var mu sync.Mutex
		var observed translator.NamespaceObservation
		publish := func(n translator.NamespaceObservation) { mu.Lock(); defer mu.Unlock(); observed = n }
		// An actual Kubernetes RBAC denial must not yield usable namespace labels.
		err = watchReferenceNamespace(ctx, restricted, "team-a", publish)
		require.ErrorContains(t, err, "forbidden")
		_, err = client.Kube().RbacV1().ClusterRoles().Create(ctx, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "namespace-observer"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"namespaces"}, ResourceNames: []string{"team-a"}, Verbs: []string{"list", "watch"}}}}, metav1.CreateOptions{})
		require.NoError(t, err)
		_, err = client.Kube().RbacV1().ClusterRoleBindings().Create(ctx, &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "namespace-observer"}, RoleRef: rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: "namespace-observer"}, Subjects: []rbacv1.Subject{{Kind: "User", Name: "namespace-observer"}}}, metav1.CreateOptions{})
		require.NoError(t, err)
		scopedCtx, stop := context.WithCancel(ctx)
		defer stop()
		go observeReferenceNamespace(scopedCtx, restricted, "team-a", publish)
		require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return observed.Namespace != nil && observed.Error == "" }, 10*time.Second, 10*time.Millisecond)
		_, err = restricted.Kube().CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		require.ErrorContains(t, err, "forbidden", "scoped observation must not need blanket namespace listing")
	})
}
