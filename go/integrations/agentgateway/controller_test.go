package agentgateway

import (
	"testing"

	agw "github.com/agentgateway/agentgateway/controller/api/v1alpha1/agentgateway"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestControllersConvergeThroughKubernetesAPI(t *testing.T) {
	api, scheme, start := testAPI(t)
	config := testConfig()
	config.MCPPolicy = &agw.Traffic{Authorization: &agw.Authorization{
		Action: agw.AuthorizationPolicyActionRequire, Policy: agw.AuthorizationPolicy{MatchExpressions: []agw.CELExpression{"true"}},
	}}
	start(config)
	ctx := t.Context()

	source := testModel("model")
	source.Spec.Policies = &agw.ModelPolicies{Auth: &agw.ModelBackendAuth{
		SecretRef: &agw.LocalSecretKeyRef{Name: "credential", Key: new("token")},
	}}
	require.NoError(t, api.Create(ctx, source))
	modelKey := client.ObjectKeyFromObject(source)
	modelReady := func(want bool) bool {
		current := &agw.AgentgatewayModel{}
		if api.Get(ctx, modelKey, current) != nil {
			return false
		}
		for _, parent := range current.Status.Parents {
			if parent.ControllerName == "kagent.dev/agentgateway-connections" {
				condition := apimeta.FindStatusCondition(parent.Conditions, ReadyCondition)
				return condition != nil && (condition.Status == metav1.ConditionTrue) == want && condition.ObservedGeneration == current.Generation
			}
		}
		return false
	}
	eventually(t, func() bool { return modelReady(false) })
	require.True(t, apierrors.IsNotFound(api.Get(ctx, modelKey, &kagentv1alpha3.ModelConfig{})))

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "credential", Namespace: "team"}}
	require.NoError(t, api.Create(ctx, secret))
	eventually(t, func() bool {
		current := &agw.AgentgatewayModel{}
		if api.Get(ctx, modelKey, current) != nil {
			return false
		}
		for _, parent := range current.Status.Parents {
			condition := apimeta.FindStatusCondition(parent.Conditions, ReadyCondition)
			if condition != nil && condition.Message == "connection credential credential has no value for key token" {
				return condition.Status == metav1.ConditionFalse
			}
		}
		return false
	})
	secret.Data = map[string][]byte{"token": []byte("credential-value")}
	require.NoError(t, api.Update(ctx, secret))
	eventually(t, func() bool { return modelReady(true) })

	model := &kagentv1alpha3.ModelConfig{}
	require.NoError(t, api.Get(ctx, modelKey, model))
	require.True(t, metav1.IsControlledBy(model, source))
	rv := model.ResourceVersion
	reconciler := &reconciler{client: api, scheme: scheme, config: config, model: true}
	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: modelKey})
	require.NoError(t, err)
	require.NoError(t, api.Get(ctx, modelKey, model))
	require.Equal(t, rv, model.ResourceVersion)

	require.NoError(t, api.Get(ctx, modelKey, source))
	source.Spec.Match.Model = new("updated-model")
	require.NoError(t, api.Update(ctx, source))
	eventually(t, func() bool {
		return api.Get(ctx, modelKey, model) == nil && model.Spec.Model == "updated-model" && modelReady(true)
	})
	require.NoError(t, api.Get(ctx, modelKey, source))
	source.Status.Parents = append(source.Status.Parents, gatewayv1.RouteParentStatus{
		ParentRef: config.Gateway, ControllerName: "example.com/gateway",
		Conditions: []metav1.Condition{{Type: "Accepted", Status: metav1.ConditionTrue, Reason: "Accepted", Message: "Gateway accepted the model", LastTransitionTime: metav1.Now()}},
	})
	require.NoError(t, api.Status().Update(ctx, source))
	delete(source.Annotations, ConnectionAnnotation)
	require.NoError(t, api.Update(ctx, source))
	eventually(t, func() bool {
		return apierrors.IsNotFound(api.Get(ctx, modelKey, model)) && api.Get(ctx, modelKey, source) == nil &&
			len(source.Status.Parents) == 1 && source.Status.Parents[0].ControllerName == "example.com/gateway"
	})

	backend := testBackend("tools")
	require.NoError(t, api.Create(ctx, backend))
	backendKey := client.ObjectKeyFromObject(backend)
	remote := &kagentv1alpha3.RemoteMCPServer{}
	eventually(t, func() bool {
		current := &agw.AgentgatewayBackend{}
		return api.Get(ctx, backendKey, current) == nil &&
			apimeta.IsStatusConditionTrue(current.Status.Conditions, ReadyCondition) &&
			api.Get(ctx, backendKey, remote) == nil
	})
	require.Equal(t, config.MCPURL+"/mcp/tools", remote.Spec.URL)
	routeKey := client.ObjectKey{Namespace: "team", Name: "mcp-tools"}
	policyKey := client.ObjectKey{Namespace: "team", Name: "tools-runtime-auth"}
	route := &gatewayv1.HTTPRoute{}
	require.NoError(t, api.Get(ctx, routeKey, route))
	require.True(t, metav1.IsControlledBy(route, backend))
	require.NoError(t, api.Get(ctx, policyKey, &agw.AgentgatewayPolicy{}))

	require.NoError(t, api.Get(ctx, backendKey, backend))
	delete(backend.Annotations, ConnectionAnnotation)
	require.NoError(t, api.Update(ctx, backend))
	eventually(t, func() bool {
		return apierrors.IsNotFound(api.Get(ctx, backendKey, &kagentv1alpha3.RemoteMCPServer{})) &&
			apierrors.IsNotFound(api.Get(ctx, routeKey, &gatewayv1.HTTPRoute{})) &&
			apierrors.IsNotFound(api.Get(ctx, policyKey, &agw.AgentgatewayPolicy{})) &&
			api.Get(ctx, backendKey, backend) == nil && apimeta.FindStatusCondition(backend.Status.Conditions, ReadyCondition) == nil
	})

	conflict := testModel("user-owned")
	existing := &kagentv1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Name: conflict.Name, Namespace: "team"},
		Spec: kagentv1alpha3.ModelConfigSpec{Provider: kagentv1alpha3.ModelProviderOpenAI, Model: "user-model"}}
	require.NoError(t, api.Create(ctx, existing))
	require.NoError(t, api.Create(ctx, conflict))
	_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(conflict)})
	require.ErrorContains(t, err, "another owner")
	require.NoError(t, api.Get(ctx, client.ObjectKeyFromObject(existing), existing))
	require.Equal(t, "user-model", existing.Spec.Model)
	require.Empty(t, existing.OwnerReferences)
}
