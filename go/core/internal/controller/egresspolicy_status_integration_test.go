package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	kagentclient "github.com/kagent-dev/kagent/go/api/clientset/versioned"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/controller/apiclient"
)

func TestEgressPolicyStatusWithWriteOnlySecretAccess(t *testing.T) {
	environment := &envtest.Environment{CRDDirectoryPaths: []string{"../../../../helm/kagent-crds/templates"}, ErrorIfCRDPathMissing: true}
	config, err := environment.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, environment.Stop()) })
	client, err := apiclient.New(config)
	require.NoError(t, err)
	_, err = client.Kube().CoreV1().Namespaces().Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "references"}}, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = client.Kube().RbacV1().Roles("references").Create(t.Context(), &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "writer"}, Rules: []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"create", "update"}},
		{APIGroups: []string{v1alpha3.GroupVersion.Group}, Resources: []string{"egresspolicies"}, Verbs: []string{"get"}},
	}}, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = client.Kube().RbacV1().RoleBindings("references").Create(t.Context(), &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "writer"}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "writer"}, Subjects: []rbacv1.Subject{{Kind: "User", Name: "writer"}}}, metav1.CreateOptions{})
	require.NoError(t, err)
	user, err := environment.AddUser(envtest.User{Name: "writer"}, config)
	require.NoError(t, err)
	writer, err := kubernetes.NewForConfig(user.Config())
	require.NoError(t, err)
	reader, err := kagentclient.NewForConfig(user.Config())
	require.NoError(t, err)
	policy, err := client.Kagent().ApiV1alpha3().EgressPolicies("references").Create(t.Context(), &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{Name: "egress"}, Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: "https://example.com", Authentication: &v1alpha3.EgressAuthentication{Header: "Authorization", SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "credentials"}, Key: "key"}}}}}}, metav1.CreateOptions{})
	require.NoError(t, err)
	opts := krt.NewOptionsBuilder(t.Context().Done(), "references", nil)
	mock := krttest.NewMock(t, nil)
	policies := typedCollection[*v1alpha3.EgressPolicy](client, []string{"references"}, "Policies", opts)
	secrets := typedCollection[*corev1.Secret](client, []string{"references"}, "Secrets", opts)
	collections := Collections{
		EgressPolicies: policies, EgressPolicyStatuses: newEgressPolicyStatuses(policies, secrets, opts),
		Agents:              krttest.GetMockCollection[*v1alpha3.Agent](mock),
		Reconciliations:     krttest.GetMockCollection[AgentReconciliation](mock),
		AgentStatuses:       krttest.GetMockCollection[krt.ObjectWithStatus[*v1alpha3.Agent, v1alpha3.AgentStatus]](mock),
		ModelConfigStatuses: krttest.GetMockCollection[krt.ObjectWithStatus[*v1alpha3.ModelConfig, v1alpha3.ModelConfigStatus]](mock),
	}
	reconciler := newReconciler(collections, &fakeActorTemplates{}, &fakeRuntimeRevisionStore{}, client.Kagent().ApiV1alpha3())
	require.True(t, client.RunAndWait(t.Context().Done()))
	t.Cleanup(client.Shutdown)
	go func() { require.NoError(t, reconciler.Start(t.Context())) }()
	expect := func(reason string) {
		t.Helper()
		require.Eventually(t, func() bool {
			current, readError := reader.ApiV1alpha3().EgressPolicies("references").Get(t.Context(), "egress", metav1.GetOptions{})
			if readError != nil {
				return false
			}
			condition := apimeta.FindStatusCondition(current.Status.Conditions, v1alpha3.EgressPolicyConditionTypeResolvedRefs)
			return condition != nil && condition.Reason == reason && condition.ObservedGeneration == policy.Generation
		}, 10*time.Second, 10*time.Millisecond)
	}
	expect(v1alpha3.EgressPolicyReasonSecretNotFound)
	secret, err := writer.CoreV1().Secrets("references").Create(t.Context(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "credentials"}}, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = writer.CoreV1().Secrets("references").Get(t.Context(), "credentials", metav1.GetOptions{})
	require.True(t, apierrors.IsForbidden(err))
	expect(v1alpha3.EgressPolicyReasonSecretKeyNotFound)
	secret.Data = map[string][]byte{"key": []byte("example")}
	_, err = writer.CoreV1().Secrets("references").Update(t.Context(), secret, metav1.UpdateOptions{})
	require.NoError(t, err)
	expect("Resolved")
	require.NoError(t, client.Kube().CoreV1().Secrets("references").Delete(t.Context(), "credentials", metav1.DeleteOptions{}))
	expect(v1alpha3.EgressPolicyReasonSecretNotFound)
}
