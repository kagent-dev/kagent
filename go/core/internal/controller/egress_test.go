package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

type revisionEgressFunc func(context.Context, string) error

func (f revisionEgressFunc) ReconcileRevision(ctx context.Context, revision string) error {
	return f(ctx, revision)
}

type egressTestStore struct {
	mu         sync.Mutex
	pinned     map[string]database.RuntimeEgress
	intended   map[string]egress.Access
	targets    map[string][]database.RuntimeEgressTarget
	listed     chan database.RuntimeEgressSource
	publishErr error
}

var _ runtimeEgressStore = (*egressTestStore)(nil)

func (e *egressTestStore) GetRuntimeEgress(_ context.Context, revision string) (*database.RuntimeEgress, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	row, ok := e.pinned[revision]
	if !ok {
		return nil, database.ErrNotFound
	}
	return &row, nil
}
func (e *egressTestStore) SetRuntimeEgressPolicy(_ context.Context, revision string, access egress.Access) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.publishErr != nil {
		return e.publishErr
	}
	if e.intended == nil {
		e.intended = map[string]egress.Access{}
	}
	e.intended[revision] = access
	return nil
}
func (e *egressTestStore) GetRuntimeEgressPolicy(_ context.Context, revision string) (egress.Access, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	policy, ok := e.intended[revision]
	if !ok {
		return egress.Access{}, database.ErrNotFound
	}
	return policy, nil
}
func (e *egressTestStore) ListRuntimeEgressRevisions(_ context.Context, after string, limit int) ([]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var revisions []string
	for key := range e.pinned {
		if key > after {
			revisions = append(revisions, key)
		}
	}
	slices.Sort(revisions)
	return revisions[:min(limit, len(revisions))], nil
}
func (e *egressTestStore) ListRuntimeEgressSourceRevisions(_ context.Context, source database.RuntimeEgressSource, after string, limit int) ([]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.listed != nil {
		e.listed <- source
	}
	var revisions []string
	for key, pinned := range e.pinned {
		if pinned.Kind == source.Kind && pinned.Namespace == source.Namespace && pinned.SourceName == source.Name && key > after {
			revisions = append(revisions, key)
		}
	}
	slices.Sort(revisions)
	return revisions[:min(limit, len(revisions))], nil
}
func (e *egressTestStore) ListRuntimeEgressTargets(_ context.Context, revision, after string, limit int) ([]database.RuntimeEgressTarget, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var targets []database.RuntimeEgressTarget
	for _, target := range e.targets[revision] {
		if target.ID > after {
			targets = append(targets, target)
		}
	}
	return targets[:min(limit, len(targets))], nil
}

type egressTestInputs struct {
	runtime   *Runtime
	agents    krt.StaticCollection[*v1alpha3.Agent]
	sandboxes krt.StaticCollection[*v1alpha3.SandboxTemplate]
	policies  krt.StaticCollection[*v1alpha3.EgressPolicy]
	secrets   krt.StaticCollection[*corev1.Secret]
}

func newEgressTestInputs(ctx context.Context) egressTestInputs {
	opts := krt.NewOptionsBuilder(ctx.Done(), "test", nil)
	inputs := egressTestInputs{
		agents:    krt.NewStaticCollection[*v1alpha3.Agent](nil, nil, opts.WithName("Agents")...),
		sandboxes: krt.NewStaticCollection[*v1alpha3.SandboxTemplate](nil, nil, opts.WithName("Sandboxes")...),
		policies:  krt.NewStaticCollection[*v1alpha3.EgressPolicy](nil, nil, opts.WithName("Policies")...),
		secrets:   krt.NewStaticCollection[*corev1.Secret](nil, nil, opts.WithName("Secrets")...),
	}
	inputs.runtime = &Runtime{Options: opts, Collections: Collections{Agents: inputs.agents, SandboxTemplates: inputs.sandboxes, EgressPolicies: inputs.policies, Secrets: inputs.secrets}}
	return inputs
}
func (i egressTestInputs) source(kind database.RuntimeEgressSourceKind, uid string, refs []corev1.LocalObjectReference) {
	metadata := metav1.ObjectMeta{Namespace: "team", Name: "source", UID: types.UID(uid)}
	if kind == database.RuntimeEgressAgent {
		i.agents.UpdateObject(&v1alpha3.Agent{ObjectMeta: metadata, Spec: v1alpha3.AgentSpec{EgressPolicyRefs: refs}})
	} else {
		i.sandboxes.UpdateObject(&v1alpha3.SandboxTemplate{ObjectMeta: metadata, Spec: v1alpha3.SandboxTemplateSpec{EgressPolicyRefs: refs}})
	}
}
func egressTestPolicy(origin string, auth *v1alpha3.EgressAuthentication) *v1alpha3.EgressPolicy {
	return &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "access"}, Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: origin, Authentication: auth}}}}
}

func TestEgressTranslationUsesCachedDependencies(t *testing.T) {
	for _, kind := range []database.RuntimeEgressSourceKind{database.RuntimeEgressAgent, database.RuntimeEgressSandbox} {
		t.Run(string(kind), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			inputs := newEgressTestInputs(ctx)
			refs := []corev1.LocalObjectReference{{Name: "access"}}
			inputs.source(kind, "original", refs)
			base := egress.Access{Destinations: []string{"https://model.example.com:443"}}
			store := &egressTestStore{pinned: map[string]database.RuntimeEgress{"pinned": {Kind: kind, Namespace: "team", SourceName: "source", SourceUID: "original", EgressDestinations: base.Destinations}}}
			controller := NewEgressReconciler(ctx, inputs.runtime, store, &observingEgressActors{store: store}, nil)
			expect := func(want egress.Access) {
				t.Helper()
				require.Eventually(t, func() bool {
					if err := controller.ReconcileRevision(ctx, "pinned"); err != nil {
						return false
					}
					got, err := store.GetRuntimeEgressPolicy(ctx, "pinned")
					return err == nil && reflect.DeepEqual(want, got)
				}, time.Second, 10*time.Millisecond)
			}
			expect(base) // Missing refs and broken unrelated runtime inputs cannot preserve stale grants.
			policy := egressTestPolicy("https://tickets.example.com", nil)
			inputs.policies.UpdateObject(policy.DeepCopy())
			allowed := egress.Access{Destinations: []string{"https://model.example.com:443", "https://tickets.example.com:443"}}
			expect(allowed)
			inputs.source(kind, "original", nil)
			expect(base)
			inputs.source(kind, "original", refs)
			expect(allowed)
			inputs.policies.DeleteObject("team/access")
			expect(base)
			inputs.policies.UpdateObject(policy.DeepCopy())
			expect(allowed)
			inputs.source(kind, "replacement", refs)
			expect(base)
			inputs.source(kind, "original", refs)
			expect(allowed)
			policy.Spec.Destinations[0].Authentication = &v1alpha3.EgressAuthentication{Header: "authorization", SecretKeyRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "auth"}, Key: "token"}}
			inputs.policies.UpdateObject(policy.DeepCopy())
			expect(base)
			inputs.secrets.UpdateObject(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "auth"}, Data: map[string][]byte{"token": []byte("secret")}})
			allowed.Credentials = []egress.Credential{{Origin: "https://tickets.example.com:443", Header: "authorization", URI: "ate-secret://k8s.io/default/team/auth/token"}}
			expect(allowed)
			inputs.secrets.DeleteObject("team/auth")
			expect(base)
		})
	}
}

type observingEgressActors struct {
	store    *egressTestStore
	mu       sync.Mutex
	applied  map[string]egress.Access
	attempts map[string][]time.Time
	failing  string
	failures int
}

func (a *observingEgressActors) EnsureActorEgressPolicy(ctx context.Context, _, name, revision string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.attempts == nil {
		a.attempts = map[string][]time.Time{}
	}
	a.attempts[name] = append(a.attempts[name], time.Now())
	if name == a.failing && len(a.attempts[name]) <= a.failures {
		return errors.New("backend unavailable")
	}
	access, err := a.store.GetRuntimeEgressPolicy(ctx, revision)
	if err != nil {
		return err
	}
	if a.applied == nil {
		a.applied = map[string]egress.Access{}
	}
	a.applied[name] = access
	return nil
}
func (a *observingEgressActors) GetActor(_ context.Context, space, name string) (*ateapipb.Actor, error) {
	return &ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{Atespace: space, Name: name, Uid: "uid"}, ActorTemplate: &ateapipb.ObjectRef{Atespace: space, Name: "runtime"}}, nil
}

func TestEgressUpdatesRetainedRevisionsAndRecoversAfterRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	inputs := newEgressTestInputs(ctx)
	inputs.source(database.RuntimeEgressAgent, "uid", []corev1.LocalObjectReference{{Name: "access"}})
	inputs.policies.UpdateObject(egressTestPolicy("https://tickets.example.com", nil))
	pinned := database.RuntimeEgress{Kind: database.RuntimeEgressAgent, Namespace: "team", SourceName: "source", SourceUID: "uid"}
	old := pinned
	old.EgressDestinations = []string{"https://old-model.example.com:443"}
	store := &egressTestStore{pinned: map[string]database.RuntimeEgress{"new": pinned, "old": old}, targets: map[string][]database.RuntimeEgressTarget{
		"new": {{ID: "new", Revision: "new", Atespace: "team", TemplateName: "runtime"}},
		"old": {{ID: "old", Revision: "old", Atespace: "team", TemplateName: "runtime"}},
	}}
	actors := &observingEgressActors{store: store}
	expect := func(granted bool) {
		t.Helper()
		require.Eventually(t, func() bool {
			actors.mu.Lock()
			defer actors.mu.Unlock()
			current, ok := actors.applied["session-new"]
			previous, oldOK := actors.applied["session-old"]
			return ok && oldOK && slices.Contains(current.Destinations, "https://tickets.example.com:443") == granted &&
				slices.Contains(previous.Destinations, "https://tickets.example.com:443") == granted && slices.Contains(previous.Destinations, "https://old-model.example.com:443")
		}, 3*time.Second, 10*time.Millisecond)
	}
	for i := range 2 {
		runCtx, stop := context.WithCancel(ctx)
		controller := NewEgressReconciler(runCtx, inputs.runtime, store, actors, nil)
		done := make(chan error, 1)
		go func() { done <- controller.Start(runCtx) }()
		if i == 0 {
			expect(true)
			inputs.policies.DeleteObject("team/access")
			expect(false)
		} else {
			expect(false)
		}
		stop()
		require.NoError(t, <-done)
		// Startup repairs existing actors without a new Kubernetes event/invocation.
		actors.mu.Lock()
		actors.applied = map[string]egress.Access{}
		actors.mu.Unlock()
	}
}

func TestEgressTargetsSourceChangesAndBacksOffActorFailures(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	inputs := newEgressTestInputs(ctx)
	inputs.source(database.RuntimeEgressAgent, "uid", nil)
	store := &egressTestStore{pinned: map[string]database.RuntimeEgress{"revision": {Kind: database.RuntimeEgressAgent, Namespace: "team", SourceName: "source", SourceUID: "uid"}},
		listed: make(chan database.RuntimeEgressSource, 20), targets: map[string][]database.RuntimeEgressTarget{"revision": {
			{ID: "failing", Revision: "revision", Atespace: "team", TemplateName: "runtime"},
			{ID: "healthy", Revision: "revision", Atespace: "team", TemplateName: "runtime"},
		}}}
	actors := &observingEgressActors{store: store, failing: "session-failing", failures: 2}
	controller := NewEgressReconciler(ctx, inputs.runtime, store, actors, nil)
	// Exercise recovery backoff independently of initial informer delivery.
	require.True(t, controller.grants.WaitUntilSynced(ctx.Done()))
	done := make(chan error, 1)
	go func() { done <- controller.Start(ctx) }()
	defer func() { cancel(); require.NoError(t, <-done) }()
	require.Eventually(t, func() bool {
		actors.mu.Lock()
		defer actors.mu.Unlock()
		return len(actors.attempts["session-failing"]) == 3
	}, 5*time.Second, 10*time.Millisecond)
	actors.mu.Lock()
	attempts := slices.Clone(actors.attempts["session-failing"])
	healthy := len(actors.attempts["session-healthy"])
	actors.mu.Unlock()
	require.GreaterOrEqual(t, attempts[1].Sub(attempts[0]), time.Second)
	require.GreaterOrEqual(t, attempts[2].Sub(attempts[1]), 2*time.Second)
	require.Equal(t, 1, healthy)
	inputs.agents.UpdateObject(&v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "unrelated", UID: "other"}})
	source := database.RuntimeEgressSource{Kind: database.RuntimeEgressAgent, Namespace: "team", Name: "unrelated"}
	select {
	case got := <-store.listed:
		require.Equal(t, source, got)
	case <-ctx.Done():
		t.Fatal("source change was not queued")
	}
	inputs.agents.DeleteObject("team/unrelated")
	select {
	case got := <-store.listed:
		require.Equal(t, source, got)
	case <-ctx.Done():
		t.Fatal("source deletion was not queued")
	}
}

func TestEgressWithholdsConflictingGrantsAndWarns(t *testing.T) {
	for _, reason := range []string{"credential", "passthrough", "rule limit"} {
		t.Run(reason, func(t *testing.T) {
			var logs bytes.Buffer
			ctx := logging.IntoContext(t.Context(), slog.New(slog.NewJSONHandler(&logs, nil)))
			inputs := newEgressTestInputs(ctx)
			inputs.source(database.RuntimeEgressAgent, "uid", []corev1.LocalObjectReference{{Name: "access"}})
			base := egress.Access{Destinations: []string{"https://model.example.com:443"}}
			pinned := database.RuntimeEgress{Kind: database.RuntimeEgressAgent, Namespace: "team", SourceName: "source", SourceUID: "uid"}
			authentication := &v1alpha3.EgressAuthentication{Header: "authorization", SecretKeyRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "other"}, Key: "token"}}
			switch reason {
			case "credential":
				base.Credentials = []egress.Credential{{Origin: base.Destinations[0], Header: "authorization", URI: "ate-secret://k8s.io/default/team/auth/token"}}
			case "passthrough":
				pinned.TokenPassthroughOrigins = base.Destinations
			case "rule limit":
				base.Destinations = nil
				for i := range 256 {
					base.Destinations = append(base.Destinations, fmt.Sprintf("https://host-%03d.example.com:443", i))
				}
				authentication = nil
			}
			pinned.EgressDestinations, pinned.Credentials = base.Destinations, base.Credentials
			inputs.policies.UpdateObject(egressTestPolicy("https://model.example.com", authentication))
			inputs.secrets.UpdateObject(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "other"}, Data: map[string][]byte{"token": []byte("private-value")}})
			store := &egressTestStore{pinned: map[string]database.RuntimeEgress{"revision": pinned}}
			controller := NewEgressReconciler(ctx, inputs.runtime, store, &observingEgressActors{store: store}, nil)
			controller.warnings = rate.NewLimiter(rate.Every(time.Hour), 1)
			for range 3 {
				require.NoError(t, controller.ReconcileRevision(ctx, "revision"))
				got, err := store.GetRuntimeEgressPolicy(ctx, "revision")
				require.NoError(t, err)
				require.Equal(t, base, got)
			}
			require.Equal(t, 1, strings.Count(logs.String(), "explicit egress grants withheld"))
			require.Contains(t, logs.String(), `"level":"WARN"`)
			require.Contains(t, logs.String(), `"revision":"revision"`)
			require.NotContains(t, logs.String(), "private-value")
		})
	}
}
