package modeldiscovery

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ktesting "k8s.io/client-go/testing"

	fakeclient "github.com/kagent-dev/kagent/go/api/clientset/versioned/fake"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	api "github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestStatusWritesOnlyReady(t *testing.T) {
	for _, tt := range []struct {
		name   string
		state  apiv1.DiscoveryState
		reason string
		ready  metav1.ConditionStatus
	}{
		{"pending", apiv1.DiscoveryState_DISCOVERY_STATE_PENDING, api.ModelProviderConfigReasonPending, metav1.ConditionFalse},
		{"success", apiv1.DiscoveryState_DISCOVERY_STATE_CURRENT, api.ModelProviderConfigReasonDiscovered, metav1.ConditionTrue},
		{"secret unresolved", apiv1.DiscoveryState_DISCOVERY_STATE_FAILED, api.ModelProviderConfigReasonSecretUnresolved, metav1.ConditionFalse},
		{"unauthorized", apiv1.DiscoveryState_DISCOVERY_STATE_FAILED, errUnauthorized.Error(), metav1.ConditionFalse},
		{"stale", apiv1.DiscoveryState_DISCOVERY_STATE_STALE, errRequest.Error(), metav1.ConditionFalse},
	} {
		t.Run(tt.name, func(t *testing.T) {
			transition := metav1.NewTime(time.Unix(1700000000, 0))
			object := &api.ModelProviderConfig{ObjectMeta: metav1.ObjectMeta{Namespace: testRef.Namespace, Name: testRef.Name, UID: "source", Generation: 1}}
			for _, kind := range []string{api.ModelProviderConfigConditionTypeReady, "ModelsDiscovered", "SecretResolved"} {
				object.Status.Conditions = append(object.Status.Conditions, metav1.Condition{Type: kind, Status: metav1.ConditionTrue, Reason: api.ModelProviderConfigReasonDiscovered, Message: api.ModelProviderConfigReasonDiscovered, ObservedGeneration: 1, LastTransitionTime: transition})
			}
			input := source{Ref: testRef, UID: object.UID, Generation: 1}
			observation := statusObservation{Input: input, State: tt.state}
			if tt.state == apiv1.DiscoveryState_DISCOVERY_STATE_FAILED || tt.state == apiv1.DiscoveryState_DISCOVERY_STATE_STALE {
				observation.Error = tt.reason
			}
			if tt.state == apiv1.DiscoveryState_DISCOVERY_STATE_CURRENT || tt.state == apiv1.DiscoveryState_DISCOVERY_STATE_STALE {
				observation.LastSuccess = &transition
			}
			got := object.DeepCopy()
			got.Status = statusWithTransitionTimes(statusForObservation(observation), object.Status)
			require.Len(t, got.Status.Conditions, 1)
			condition := got.Status.Conditions[0]
			require.Equal(t, api.ModelProviderConfigConditionTypeReady, condition.Type)
			require.Equal(t, tt.ready, condition.Status)
			require.Equal(t, tt.reason, condition.Reason)
			require.Equal(t, int64(1), condition.ObservedGeneration)
			require.Zero(t, got.Status.ModelCount)
			if tt.ready == metav1.ConditionTrue {
				require.Equal(t, transition, condition.LastTransitionTime)
			}
			require.True(t, apiequality.Semantic.DeepEqual(got.Status, statusWithTransitionTimes(statusForObservation(observation), got.Status)))
		})
	}
}

func statusFixture(t *testing.T) (*Controller, krt.StaticCollection[*api.ModelProviderConfig], krt.StaticCollection[*corev1.Secret]) {
	t.Helper()
	opts := krt.NewOptionsBuilder(t.Context().Done(), "status-test", nil)
	p := &api.ModelProviderConfig{ObjectMeta: metav1.ObjectMeta{Namespace: testRef.Namespace, Name: testRef.Name, UID: "source", Generation: 1}, Spec: api.ModelProviderConfigSpec{Type: api.ModelProviderOpenAI, Endpoint: "https://gateway.example/v1", SecretRef: &api.SecretReference{Name: "credential"}}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: testRef.Namespace, Name: "credential", UID: "secret", ResourceVersion: "1"}, Data: map[string][]byte{"key": []byte("first")}}
	providers := krt.NewStaticCollection[*api.ModelProviderConfig](nil, []*api.ModelProviderConfig{p}, opts.WithName("Providers")...)
	secrets := krt.NewStaticCollection[*corev1.Secret](nil, []*corev1.Secret{secret}, opts.WithName("Secrets")...)
	c := New(providers, secrets, opts)
	require.True(t, c.statuses.WaitUntilSynced(t.Context().Done()))
	require.Eventually(t, func() bool {
		return c.inputs.GetKey(testRef.String()) != nil && c.statuses.GetKey(testRef.String()) != nil
	}, time.Second, time.Millisecond)
	return c, providers, secrets
}

func TestKRTStatusObservations(t *testing.T) {
	c, _, secrets := statusFixture(t)
	key := testRef.String()
	require.Equal(t, api.ModelProviderConfigReasonPending, c.statuses.GetKey(key).Status.Conditions[0].Reason)
	now := metav1.NewTime(time.Unix(1700000000, 0))
	observation := statusObservation{Input: *c.inputs.GetKey(key), State: apiv1.DiscoveryState_DISCOVERY_STATE_CURRENT, Count: 2, LastSuccess: &now}
	c.observations.UpdateObject(observation)
	require.Eventually(t, func() bool { return c.statuses.GetKey(key).Status.ModelCount == 2 }, time.Second, time.Millisecond)
	next := metav1.NewTime(now.Add(time.Minute))
	observation.LastSuccess = &next
	c.observations.UpdateObject(observation)
	require.Eventually(t, func() bool { return c.statuses.GetKey(key).Status.LastDiscoveryTime.Equal(&next) }, time.Second, time.Millisecond)
	// Secret rotation invalidates status even though UID/generation are unchanged.
	secret := (*secrets.GetKey("team/credential")).DeepCopy()
	secret.ResourceVersion = "2"
	secrets.UpdateObject(secret)
	require.Eventually(t, func() bool {
		status := c.statuses.GetKey(key).Status
		return status.ModelCount == 0 && status.LastDiscoveryTime == nil && status.Conditions[0].Reason == api.ModelProviderConfigReasonPending
	}, time.Second, time.Millisecond)
	// Late old-credential observations cannot turn the new input epoch Ready.
	observation.Count = 3
	c.observations.UpdateObject(observation)
	require.Eventually(t, func() bool { return c.observations.GetKey(key).Count == 3 }, time.Second, time.Millisecond)
	require.Never(t, func() bool { return c.statuses.GetKey(key).Status.ModelCount != 0 }, 50*time.Millisecond, time.Millisecond)
}

func TestStatusQueueRetriesLatestObservation(t *testing.T) {
	c, providers, _ := statusFixture(t)
	key := testRef.String()
	object := (*providers.GetKey(key)).DeepCopy()
	client := fakeclient.NewSimpleClientset(object)
	var attempts atomic.Int32
	failed := make(chan struct{})
	client.PrependReactor("update", "modelproviderconfigs", func(action ktesting.Action) (bool, runtime.Object, error) {
		require.Equal(t, "status", action.GetSubresource())
		if attempts.Add(1) == 1 {
			close(failed)
			return true, nil, apierrors.NewConflict(schema.GroupResource{Group: api.GroupVersion.Group, Resource: "modelproviderconfigs"}, object.Name, fmt.Errorf("test conflict"))
		}
		updated := action.(ktesting.UpdateAction).GetObject().(*api.ModelProviderConfig).DeepCopy()
		providers.UpdateObject(updated)
		return false, nil, nil
	})
	writer := &StatusWriter{discovery: c, client: client.ApiV1alpha3()}
	require.True(t, writer.NeedLeaderElection())
	require.False(t, c.NeedLeaderElection())
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- writer.Start(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	select {
	case <-failed:
	case <-time.After(5 * time.Second):
		t.Fatal("status queue did not write")
	}
	c.observations.UpdateObject(statusObservation{Input: *c.inputs.GetKey(key), State: apiv1.DiscoveryState_DISCOVERY_STATE_FAILED, Error: api.ModelProviderConfigReasonSecretUnresolved})
	require.Eventually(t, func() bool {
		got, err := client.ApiV1alpha3().ModelProviderConfigs(testRef.Namespace).Get(t.Context(), testRef.Name, metav1.GetOptions{})
		return err == nil && len(got.Status.Conditions) == 1 && got.Status.Conditions[0].Reason == api.ModelProviderConfigReasonSecretUnresolved
	}, 5*time.Second, 10*time.Millisecond)
	require.GreaterOrEqual(t, attempts.Load(), int32(2))
	before := attempts.Load()
	require.Never(t, func() bool { return attempts.Load() != before }, 100*time.Millisecond, 10*time.Millisecond)
}
