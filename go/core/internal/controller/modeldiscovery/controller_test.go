package modeldiscovery

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	api "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

func testController(t *testing.T, endpoint string) (*Controller, krt.StaticCollection[*api.ModelProviderConfig], krt.StaticCollection[*corev1.Secret]) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	opts := krt.NewOptionsBuilder(ctx.Done(), "discovery-test", nil)
	providers := krt.NewStaticCollection[*api.ModelProviderConfig](nil, nil, opts.WithName("Providers")...)
	secrets := krt.NewStaticCollection[*corev1.Secret](nil, nil, opts.WithName("Secrets")...)
	c := New(providers, secrets, opts)
	done := make(chan error, 1)
	go func() { done <- c.Start(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	providers.UpdateObject(&api.ModelProviderConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "gateway", UID: "source", Generation: 1}, Spec: api.ModelProviderConfigSpec{Type: api.ModelProviderOpenAI, Endpoint: endpoint}})
	require.Eventually(t, c.Synced, 5*time.Second, 10*time.Millisecond)
	return c, providers, secrets
}

var testRef = types.NamespacedName{Namespace: "team", Name: "gateway"}

func waitState(t *testing.T, c *Controller, state apiv1.DiscoveryState) {
	t.Helper()
	require.Eventually(t, func() bool { s := c.Get(testRef); return s != nil && s.State == state }, 5*time.Second, 10*time.Millisecond)
}
func scheduleNow(c *Controller) {
	c.mu.Lock()
	c.entries[testRef.String()].due = time.Time{}
	c.mu.Unlock()
	c.signal()
}

func TestPeriodicRefreshReplacesAndRetainsOnFailure(t *testing.T) {
	var mode atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode.Load() {
		case 0:
			_, _ = fmt.Fprint(w, `{"object":"list","data":[{"id":"old","object":"model"}]}`)
		case 1:
			_, _ = fmt.Fprint(w, `{"object":"list","data":[{"id":"new","object":"model","capabilities":{"tools":true}}]}`)
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			_, _ = fmt.Fprint(w, `{"object":"list","data":[]}`)
		}
	}))
	defer server.Close()
	c, _, _ := testController(t, server.URL)
	waitState(t, c, apiv1.DiscoveryState_DISCOVERY_STATE_CURRENT)
	require.Equal(t, "old", c.Get(testRef).Models[0].Id)
	mode.Store(1)
	scheduleNow(c)
	require.Eventually(t, func() bool { return c.Get(testRef).Models[0].Id == "new" }, 5*time.Second, 10*time.Millisecond)
	require.True(t, *c.Get(testRef).Models[0].Capabilities.Tools)
	changed, cancel := c.Subscribe()
	defer cancel()
	<-changed
	previous := c.Get(testRef).LastSuccess.AsTime()
	scheduleNow(c)
	require.Eventually(t, func() bool { return c.Get(testRef).LastSuccess.AsTime().After(previous) }, 5*time.Second, 10*time.Millisecond)
	select {
	case <-changed:
		t.Fatal("unchanged model set notified observers")
	default:
	}
	mode.Store(2)
	scheduleNow(c)
	waitState(t, c, apiv1.DiscoveryState_DISCOVERY_STATE_STALE)
	require.Equal(t, "new", c.Get(testRef).Models[0].Id)
	mode.Store(3)
	scheduleNow(c)
	require.Eventually(t, func() bool {
		s := c.Get(testRef)
		return s.State == apiv1.DiscoveryState_DISCOVERY_STATE_CURRENT && len(s.Models) == 0
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, int64(1), c.Get(testRef).Generation)
}
func TestEndpointChangeDiscardsLateResults(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
		_, _ = fmt.Fprint(w, `{"object":"list","data":[{"id":"obsolete","object":"model"}]}`)
	}))
	defer old.Close()
	next := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer next.Close()
	c, providers, _ := testController(t, old.URL)
	<-started
	providers.UpdateObject(&api.ModelProviderConfig{ObjectMeta: metav1.ObjectMeta{Namespace: testRef.Namespace, Name: testRef.Name, UID: "source", Generation: 2}, Spec: api.ModelProviderConfigSpec{Type: api.ModelProviderOpenAI, Endpoint: next.URL}})
	require.Eventually(t, func() bool { return c.Get(testRef).Generation == 2 }, 5*time.Second, 10*time.Millisecond)
	close(release)
	waitState(t, c, apiv1.DiscoveryState_DISCOVERY_STATE_FAILED)
	require.Empty(t, c.Get(testRef).Models)
	require.Nil(t, c.Get(testRef).LastSuccess)
	providers.DeleteObject(testRef.String())
	require.Eventually(t, func() bool { return c.Get(testRef) == nil }, 5*time.Second, 10*time.Millisecond)
}
func TestSecretRotationInvalidatesCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer first" {
			w.WriteHeader(401)
			return
		}
		_, _ = fmt.Fprint(w, `{"object":"list","data":[{"id":"private","object":"model"}]}`)
	}))
	defer server.Close()
	c, providers, secrets := testController(t, server.URL)
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "discovery", UID: "secret", ResourceVersion: "1"}, Data: map[string][]byte{"key": []byte("first")}}
	secrets.UpdateObject(secret)
	providers.UpdateObject(&api.ModelProviderConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "gateway", UID: "source", Generation: 2}, Spec: api.ModelProviderConfigSpec{Type: api.ModelProviderOpenAI, Endpoint: server.URL, SecretRef: &api.SecretReference{Name: "discovery"}}})
	waitState(t, c, apiv1.DiscoveryState_DISCOVERY_STATE_CURRENT)
	require.Len(t, c.Get(testRef).Models, 1)
	rotated := secret.DeepCopy()
	rotated.ResourceVersion = "2"
	rotated.Data["key"] = []byte("second")
	secrets.UpdateObject(rotated)
	waitState(t, c, apiv1.DiscoveryState_DISCOVERY_STATE_FAILED)
	require.Empty(t, c.Get(testRef).Models)
	require.Nil(t, c.Get(testRef).LastSuccess)
	secrets.DeleteObject("team/discovery")
	require.Eventually(t, func() bool { return c.Get(testRef).Error == errSecret.Error() }, 5*time.Second, 10*time.Millisecond)
}
func TestExplicitRefreshRateLimitAndImmutableReads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"object":"list","data":[{"id":"immutable","object":"model"}]}`)
	}))
	defer server.Close()
	c, _, _ := testController(t, server.URL)
	waitState(t, c, apiv1.DiscoveryState_DISCOVERY_STATE_CURRENT)
	s := c.Get(testRef)
	s.Models[0].Id = "modified"
	require.Equal(t, "immutable", c.Get(testRef).Models[0].Id)
	_, err := c.Refresh(t.Context(), testRef)
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeResourceExhausted))
}

func TestRefreshCoalescesAndCallerCancellationDoesNotCancelDiscovery(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) > 1 {
			started <- struct{}{}
			<-release
		}
		_, _ = fmt.Fprint(w, `{"object":"list","data":[{"id":"shared","object":"model"}]}`)
	}))
	defer server.Close()
	c, _, _ := testController(t, server.URL)
	waitState(t, c, apiv1.DiscoveryState_DISCOVERY_STATE_CURRENT)
	c.mu.Lock()
	c.entries[testRef.String()].lastAttempt = time.Time{}
	c.mu.Unlock()
	firstCtx, cancel := context.WithCancel(t.Context())
	first := make(chan error, 1)
	go func() { _, err := c.Refresh(firstCtx, testRef); first <- err }()
	<-started
	second := make(chan error, 1)
	go func() { _, err := c.Refresh(t.Context(), testRef); second <- err }()
	cancel()
	require.ErrorIs(t, <-first, context.Canceled)
	// Wait until the second caller is waiting on the active attempt before release.
	require.Eventually(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.entries[testRef.String()].cancel != nil }, time.Second, time.Millisecond)
	close(release)
	err := <-second
	// The caller can arrive after completion; in that case admission rejects a new
	// attempt instead of creating duplicate upstream work.
	require.True(t, err == nil || serviceerrors.IsCode(err, serviceerrors.CodeResourceExhausted))
	require.Eventually(t, func() bool { return c.Get(testRef).State == apiv1.DiscoveryState_DISCOVERY_STATE_CURRENT }, time.Second, time.Millisecond)
	require.Equal(t, int32(2), calls.Load())
}

func TestUpstreamConcurrencyIsBounded(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		<-release
		_, _ = fmt.Fprint(w, `{"object":"list","data":[]}`)
	}))
	defer server.Close()
	c, providers, _ := testController(t, server.URL)
	for i := range 5 {
		providers.UpdateObject(&api.ModelProviderConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: fmt.Sprintf("gateway-%d", i), UID: types.UID(fmt.Sprintf("uid-%d", i)), Generation: 1}, Spec: api.ModelProviderConfigSpec{Type: api.ModelProviderOpenAI, Endpoint: server.URL}})
	}
	require.Eventually(t, func() bool { return calls.Load() == 4 }, 5*time.Second, time.Millisecond)
	require.Never(t, func() bool { return calls.Load() > 4 }, 50*time.Millisecond, time.Millisecond)
	close(release)
	require.Eventually(t, func() bool {
		snapshots := c.List()
		if len(snapshots) != 6 {
			return false
		}
		for _, snapshot := range snapshots {
			if snapshot.State != apiv1.DiscoveryState_DISCOVERY_STATE_CURRENT {
				return false
			}
		}
		return true
	}, 5*time.Second, time.Millisecond)
}
