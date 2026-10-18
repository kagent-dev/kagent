package remotemcpserver

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/pkg/consts"
)

func TestPassthroughDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name     string
		headers  []v1alpha3.ValueRef
		disabled bool
		reason   string
		calls    int
		failure  bool
	}{
		{name: "no credential", reason: discoveryCredentialRequired},
		{name: "empty credential", headers: []v1alpha3.ValueRef{{Name: "Authorization"}}, reason: discoveryCredentialRequired},
		{name: "disabled takes precedence", disabled: true, reason: "DiscoveryDisabled"},
		{name: "literal discovery auth", headers: []v1alpha3.ValueRef{{Name: "aUtHoRiZaTiOn", Value: "discovery-token"}}, reason: "DiscoverySucceeded", calls: 1},
		{name: "secret discovery auth", headers: []v1alpha3.ValueRef{{Name: "Authorization", ValueFrom: &v1alpha3.ValueSource{
			Type: v1alpha3.SecretValueSource, Name: "discovery", Key: "token"}}}, reason: "DiscoverySucceeded", calls: 1},
		{name: "failure redacts upstream", headers: []v1alpha3.ValueRef{{Name: "Authorization", Value: "discovery-token"}}, reason: "DiscoveryFailed", calls: 1, failure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := testServer()
			server.Spec.TokenPassthrough = new(true)
			server.Spec.HeadersFrom = tc.headers
			server.Status.DiscoveredTools = []*v1alpha3.MCPTool{{Name: "stale"}}
			if tc.disabled {
				server.Labels = map[string]string{consts.DiscoveryLabel: consts.DiscoveryDisabled}
			}
			kube := testClient(t, server)
			discoverer, catalog := &fakeDiscoverer{}, &fakeCatalog{}
			if tc.failure {
				discoverer.err = errors.New("upstream echoed discovery-token")
			}
			result, err := New(kube, discoverer, catalog).Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(server)})
			if tc.failure {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "discovery-token")
			} else {
				require.NoError(t, err)
				require.Equal(t, refreshInterval, result.RequeueAfter)
			}
			require.Equal(t, tc.calls, discoverer.calls)
			status := getServer(t, kube, server).Status
			require.Empty(t, status.DiscoveredTools)
			condition := apiMeta.FindStatusCondition(status.Conditions, conditionAccepted)
			require.NotNil(t, condition)
			require.Equal(t, tc.reason, condition.Reason)
			require.NotContains(t, condition.Message, "discovery-token")
			if tc.reason == discoveryCredentialRequired || tc.failure {
				require.Equal(t, metav1.ConditionFalse, condition.Status)
			}
			require.Empty(t, catalog.tools)
			if tc.calls == 0 || tc.failure {
				require.Nil(t, catalog.server.LastConnected)
			}
		})
	}
}
