package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	v1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

type egressTestStore struct {
	pinned  database.RuntimeEgress
	targets []database.RuntimeEgressTarget
	listed  chan database.RuntimeEgressSource
}

var _ runtimeEgressStore = (*egressTestStore)(nil)

func (e *egressTestStore) GetRuntimeEgress(context.Context, string) (*database.RuntimeEgress, error) {
	return &e.pinned, nil
}
func (e *egressTestStore) ListRuntimeEgressTargets(_ context.Context, source database.RuntimeEgressSource, after string, limit int) ([]database.RuntimeEgressTarget, error) {
	if e.listed != nil {
		e.listed <- source
	}
	if after != "" || (source.Kind != "" && source.Name != e.pinned.SourceName) {
		return nil, nil
	}
	return e.targets, nil
}

func TestLiveEgressResolvesCurrentSource(t *testing.T) {
	for _, kind := range []string{"agent", "sandbox"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			opts := krt.NewOptionsBuilder(ctx.Done(), "test", nil)
			agents := krt.NewStaticCollection[*v1alpha3.Agent](nil, nil, opts.WithName("Agents")...)
			sandboxes := krt.NewStaticCollection[*v1alpha3.SandboxTemplate](nil, nil, opts.WithName("Sandboxes")...)
			policies := krt.NewStaticCollection[*v1alpha3.EgressPolicy](nil, nil, opts.WithName("Policies")...)
			secrets := krt.NewStaticCollection[*corev1.Secret](nil, nil, opts.WithName("Secrets")...)
			reader := egressTestReader(t)
			setSource := func(uid string, refs []corev1.LocalObjectReference) {
				metadata := metav1.ObjectMeta{Namespace: "team", Name: "source"}
				metadata.SetUID(types.UID(uid))
				if kind == "agent" {
					source := &v1alpha3.Agent{ObjectMeta: metadata, Spec: v1alpha3.AgentSpec{EgressPolicyRefs: refs}}
					require.NoError(t, ctrlclient.IgnoreNotFound(reader.Delete(ctx, source)))
					require.NoError(t, reader.Create(ctx, source))
					agents.UpdateObject(source.DeepCopy())
				} else {
					source := &v1alpha3.SandboxTemplate{ObjectMeta: metadata, Spec: v1alpha3.SandboxTemplateSpec{EgressPolicyRefs: refs}}
					require.NoError(t, ctrlclient.IgnoreNotFound(reader.Delete(ctx, source)))
					require.NoError(t, reader.Create(ctx, source))
					sandboxes.UpdateObject(source.DeepCopy())
				}
			}
			refs := []corev1.LocalObjectReference{{Name: "access"}}
			setSource("original", refs)
			base := egress.Access{Destinations: []string{"https://model.example.com:443"}}
			store := &egressTestStore{pinned: database.RuntimeEgress{Kind: kind, Namespace: "team", SourceName: "source", SourceUID: "original", EgressDestinations: base.Destinations}}
			resolver := NewEgressResolver(&Runtime{Options: opts, Collections: Collections{Agents: agents, SandboxTemplates: sandboxes, EgressPolicies: policies, Secrets: secrets}}, store, reader, nil)
			expect := func(want egress.Access) {
				t.Helper()
				require.Eventually(t, func() bool {
					got, err := resolver.ResolveEgress(ctx, "pinned")
					return err == nil && reflect.DeepEqual(want, got)
				}, time.Second, 10*time.Millisecond)
			}
			expect(base) // Missing reference; unrelated runtime configuration is also invalid.
			policy := &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "access"}, Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: "https://tickets.example.com"}}}}
			require.NoError(t, reader.Create(ctx, policy.DeepCopy()))
			allowed := egress.Access{Destinations: []string{"https://model.example.com:443", "https://tickets.example.com:443"}}
			expect(allowed)
			setSource("original", nil)
			expect(base)
			setSource("original", refs)
			expect(allowed)
			require.NoError(t, reader.Delete(ctx, policy))
			policy.ResourceVersion = ""
			expect(base)
			require.NoError(t, reader.Create(ctx, policy))
			expect(allowed)
			setSource("replacement", refs)
			expect(base)
			setSource("original", refs)
			expect(allowed)
			policy.Spec.Destinations[0].Authentication = &v1alpha3.EgressAuthentication{Header: "authorization", SecretKeyRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "auth"}, Key: "token"}}
			require.NoError(t, reader.Update(ctx, policy))
			expect(base)
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "auth"}, Data: map[string][]byte{"token": []byte("secret")}}
			require.NoError(t, reader.Create(ctx, secret))
			allowed.Credentials = []egress.Credential{{Origin: "https://tickets.example.com:443", Header: "authorization", URI: "ate-secret://k8s.io/default/team/auth/token"}}
			expect(allowed)
			require.NoError(t, reader.Delete(ctx, secret))
			expect(base)
		})
	}
}

func TestLiveEgressRejectsConflictsWithPinnedRuntime(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprint("passthrough=", passthrough), func(t *testing.T) {
			base := egress.Access{Destinations: []string{"https://model.example.com:443"}}
			credential := egress.Credential{Origin: base.Destinations[0], Header: "authorization", URI: "ate-secret://k8s.io/default/team/auth/token"}
			pinned := database.RuntimeEgress{Kind: "agent", Namespace: "team", SourceName: "source", SourceUID: "uid", EgressDestinations: base.Destinations}
			if passthrough {
				pinned.TokenPassthroughOrigins = base.Destinations
			} else {
				base.Credentials = []egress.Credential{credential}
				pinned.Credentials = base.Credentials
			}
			reader := egressTestReader(t, egressTestAgent(), egressTestPolicy(base.Destinations[0], &v1alpha3.EgressAuthentication{Header: "authorization", SecretKeyRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "other"}, Key: "token"}}), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "other"}, Data: map[string][]byte{"token": []byte("secret")}})
			resolver := &EgressResolver{reader: reader, warnings: rate.NewLimiter(rate.Every(time.Minute), 1), store: &egressTestStore{pinned: pinned}}
			got, err := resolver.ResolveEgress(t.Context(), "old-revision")
			require.NoError(t, err)
			require.Equal(t, base, got)
		})
	}
}

type observingEgressActors struct {
	resolver *EgressResolver
	applied  chan egress.Access
}

var _ actorEgressClient = (*observingEgressActors)(nil)

func (a *observingEgressActors) EnsureActorEgressPolicy(ctx context.Context, _, _, revision string) error {
	access, err := a.resolver.ResolveEgress(ctx, revision)
	if err != nil {
		return err
	}
	select {
	case a.applied <- access:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func TestLiveEgressReconcilesWithoutInvocationAndAfterRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	opts := krt.NewOptionsBuilder(ctx.Done(), "test", nil)
	grant := policyGrants{Source: database.RuntimeEgressSource{Kind: "agent", Namespace: "team", Name: "source"}, UID: "uid", Access: egress.Access{Destinations: []string{"https://tickets.example.com:443"}}}
	grants := krt.NewStaticCollection(nil, []policyGrants{grant}, opts.WithName("Grants")...)
	store := &egressTestStore{pinned: database.RuntimeEgress{Kind: "agent", Namespace: "team", SourceName: "source", SourceUID: "uid"}, targets: []database.RuntimeEgressTarget{{ID: "instance", Revision: "old-revision", Atespace: "team", TemplateName: "runtime"}}}
	reader := egressTestReader(t, egressTestAgent(), egressTestPolicy("https://tickets.example.com", nil))
	resolver := &EgressResolver{grants: grants, store: store, reader: reader}
	actors := &observingEgressActors{resolver: resolver, applied: make(chan egress.Access, 10)}
	expect := func(want []string) {
		t.Helper()
		for {
			select {
			case access := <-actors.applied:
				if reflect.DeepEqual(want, access.Destinations) {
					return
				}
			case <-ctx.Done():
				t.Fatal("policy did not converge")
			}
		}
	}
	for i := range 2 {
		runCtx, stop := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- NewEgressReconciler(resolver, actors).Start(runCtx) }()
		expect(grant.Access.Destinations)
		if i == 0 {
			require.NoError(t, reader.Delete(ctx, egressTestPolicy("https://tickets.example.com", nil)))
			grant.Access = egress.Access{}
			grants.UpdateObject(grant)
			expect(nil)
		}
		stop()
		require.NoError(t, <-done)
	}
}

func (a *observingEgressActors) GetActor(_ context.Context, space, name string) (*ateapipb.Actor, error) {
	return &ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{Atespace: space, Name: name, Uid: "uid"}, ActorTemplate: &ateapipb.ObjectRef{Atespace: space, Name: "runtime"}}, nil
}

func egressTestReader(t *testing.T, objects ...ctrlclient.Object) ctrlclient.WithWatch {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha3.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func egressTestAgent() *v1alpha3.Agent {
	return &v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "source", UID: "uid"}, Spec: v1alpha3.AgentSpec{EgressPolicyRefs: []corev1.LocalObjectReference{{Name: "access"}}}}
}

func egressTestPolicy(origin string, auth *v1alpha3.EgressAuthentication) *v1alpha3.EgressPolicy {
	return &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "access"}, Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: origin, Authentication: auth}}}}
}

func TestLiveEgressIgnoresLaggingCache(t *testing.T) {
	ctx := t.Context()
	policy := egressTestPolicy("https://tickets.example.com", nil)
	reader := egressTestReader(t, egressTestAgent(), policy)
	opts := krt.NewOptionsBuilder(ctx.Done(), "test", nil)
	stale := policyGrants{Source: database.RuntimeEgressSource{Kind: "agent", Namespace: "team", Name: "source"}, UID: "uid", Access: egress.Access{Destinations: []string{"https://tickets.example.com:443"}}}
	grants := krt.NewStaticCollection(nil, []policyGrants{stale}, opts.WithName("StaleGrants")...)
	resolver := &EgressResolver{reader: reader, grants: grants, store: &egressTestStore{pinned: database.RuntimeEgress{Kind: "agent", Namespace: "team", SourceName: "source", SourceUID: "uid"}}}
	got, err := resolver.ResolveEgress(ctx, "revision")
	require.NoError(t, err)
	require.Equal(t, stale.Access, got)
	require.NoError(t, reader.Delete(ctx, policy))
	got, err = resolver.ResolveEgress(ctx, "revision")
	require.NoError(t, err)
	require.Empty(t, got, "a replica with stale grants must not undo revocation")
	require.Equal(t, stale.Access, grants.GetKey(stale.ResourceName()).Access)

	unavailable := errors.New("API unavailable")
	resolver.reader = interceptor.NewClient(reader, interceptor.Funcs{Get: func(context.Context, ctrlclient.WithWatch, ctrlclient.ObjectKey, ctrlclient.Object, ...ctrlclient.GetOption) error {
		return unavailable
	}})
	_, err = resolver.ResolveEgress(ctx, "revision")
	require.ErrorIs(t, err, unavailable, "transport failures must retry instead of becoming an empty grant")
}

type retryEgressActors struct {
	observingEgressActors
	failures atomic.Int32
	mu       sync.Mutex
	attempts map[string][]time.Time
}

func (a *retryEgressActors) EnsureActorEgressPolicy(_ context.Context, _, name, _ string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.attempts[name] = append(a.attempts[name], time.Now())
	if name == "session-failing" && a.failures.Add(1) <= 2 {
		return errors.New("backend unavailable")
	}
	return nil
}

func TestLiveEgressTargetsChangesAndRetriesFailedActors(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	opts := krt.NewOptionsBuilder(ctx.Done(), "test", nil)
	grant := policyGrants{Source: database.RuntimeEgressSource{Kind: "agent", Namespace: "team", Name: "source"}, UID: "uid"}
	grants := krt.NewStaticCollection(nil, []policyGrants{grant}, opts.WithName("Grants")...)
	store := &egressTestStore{pinned: database.RuntimeEgress{SourceName: "source"}, listed: make(chan database.RuntimeEgressSource, 10), targets: []database.RuntimeEgressTarget{
		{ID: "failing", Atespace: "team", TemplateName: "runtime"}, {ID: "healthy", Atespace: "team", TemplateName: "runtime"},
	}}
	actors := &retryEgressActors{attempts: map[string][]time.Time{}}
	done := make(chan error, 1)
	go func() { done <- NewEgressReconciler(&EgressResolver{store: store, grants: grants}, actors).Start(ctx) }()
	defer func() { cancel(); require.NoError(t, <-done) }()
	require.Equal(t, database.RuntimeEgressSource{}, <-store.listed, "startup recovers all actors")
	require.Eventually(t, func() bool {
		actors.mu.Lock()
		defer actors.mu.Unlock()
		return len(actors.attempts["session-failing"]) == 3
	}, 5*time.Second, 10*time.Millisecond)
	actors.mu.Lock()
	attempts := actors.attempts["session-failing"]
	healthy := len(actors.attempts["session-healthy"])
	actors.mu.Unlock()
	require.Len(t, attempts, 3)
	require.GreaterOrEqual(t, attempts[1].Sub(attempts[0]), time.Second)
	require.GreaterOrEqual(t, attempts[2].Sub(attempts[1]), 2*time.Second)
	require.Equal(t, 1, healthy, "a failure must not retry successful actors")
	grant.Source.Name = "unrelated"
	grants.UpdateObject(grant)
	require.Equal(t, grant.Source, <-store.listed, "events must select only their own source")
	grants.DeleteObject(grant.ResourceName())
	require.Equal(t, grant.Source, <-store.listed, "source deletion must also revoke its old actors")
}

func TestLiveEgressWarnsWhenMergedGrantsExceedLimit(t *testing.T) {
	base := egress.Access{}
	for i := range 256 {
		base.Destinations = append(base.Destinations, fmt.Sprintf("https://host-%03d.example.com:443", i))
	}
	var logs bytes.Buffer
	ctx := logging.IntoContext(t.Context(), slog.New(slog.NewJSONHandler(&logs, nil)))
	resolver := &EgressResolver{
		reader:   egressTestReader(t, egressTestAgent(), egressTestPolicy("https://extra.example.com", nil)),
		warnings: rate.NewLimiter(rate.Every(time.Hour), 1),
		store:    &egressTestStore{pinned: database.RuntimeEgress{Kind: "agent", Namespace: "team", SourceName: "source", SourceUID: "uid", EgressDestinations: base.Destinations}},
	}
	for range 3 {
		got, err := resolver.ResolveEgress(ctx, "full-revision")
		require.NoError(t, err)
		require.Equal(t, base, got)
	}
	require.Equal(t, 1, strings.Count(logs.String(), "explicit egress grants withheld"))
	require.Contains(t, logs.String(), `"level":"WARN"`)
	require.Contains(t, logs.String(), `"revision":"full-revision"`)
}
