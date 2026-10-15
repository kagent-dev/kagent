package controller

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
)

type egressTestStore struct {
	pinned  database.RuntimeEgress
	targets []database.RuntimeEgressTarget
}

var _ runtimeEgressStore = (*egressTestStore)(nil)

func (e *egressTestStore) GetRuntimeEgress(context.Context, string) (*database.RuntimeEgress, error) {
	return &e.pinned, nil
}
func (e *egressTestStore) ListRuntimeEgressTargets(_ context.Context, after string, limit int) ([]database.RuntimeEgressTarget, error) {
	if after != "" {
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
			setSource := func(uid string, refs []corev1.LocalObjectReference) {
				metadata := metav1.ObjectMeta{Namespace: "team", Name: "source"}
				metadata.SetUID(types.UID(uid))
				if kind == "agent" {
					agents.UpdateObject(&v1alpha3.Agent{ObjectMeta: metadata, Spec: v1alpha3.AgentSpec{EgressPolicyRefs: refs}})
				} else {
					sandboxes.UpdateObject(&v1alpha3.SandboxTemplate{ObjectMeta: metadata, Spec: v1alpha3.SandboxTemplateSpec{EgressPolicyRefs: refs}})
				}
			}
			refs := []corev1.LocalObjectReference{{Name: "access"}}
			setSource("original", refs)
			base := egress.Access{Destinations: []string{"https://model.example.com:443"}}
			store := &egressTestStore{pinned: database.RuntimeEgress{Kind: kind, Namespace: "team", SourceName: "source", SourceUID: "original", EgressDestinations: base.Destinations}}
			resolver := NewEgressResolver(&Runtime{Options: opts, Collections: Collections{Agents: agents, SandboxTemplates: sandboxes, EgressPolicies: policies, Secrets: secrets}}, store, nil)
			expect := func(want egress.Access) {
				t.Helper()
				require.Eventually(t, func() bool {
					got, err := resolver.ResolveEgress(ctx, "pinned")
					return err == nil && reflect.DeepEqual(want, got)
				}, time.Second, 10*time.Millisecond)
			}
			expect(base) // Missing reference; unrelated runtime configuration is also invalid.
			policy := &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "access"}, Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: "https://tickets.example.com"}}}}
			policies.UpdateObject(policy.DeepCopy())
			allowed := egress.Access{Destinations: []string{"https://model.example.com:443", "https://tickets.example.com:443"}}
			expect(allowed)
			setSource("original", nil)
			expect(base)
			setSource("original", refs)
			expect(allowed)
			policies.DeleteObject("team/access")
			expect(base)
			policies.UpdateObject(policy.DeepCopy())
			expect(allowed)
			setSource("replacement", refs)
			expect(base)
			setSource("original", refs)
			expect(allowed)
			policy.Spec.Destinations[0].Authentication = &v1alpha3.EgressAuthentication{Header: "authorization", SecretKeyRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "auth"}, Key: "token"}}
			policies.UpdateObject(policy.DeepCopy())
			expect(base)
			secrets.UpdateObject(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "auth"}, Data: map[string][]byte{"token": []byte("secret")}})
			allowed.Credentials = []egress.Credential{{Origin: "https://tickets.example.com:443", Header: "authorization", URI: "ate-secret://k8s.io/default/team/auth/token"}}
			expect(allowed)
			secrets.DeleteObject("team/auth")
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
				pinned.PassthroughOrigins = base.Destinations
			} else {
				base.Credentials = []egress.Credential{credential}
				pinned.Credentials = base.Credentials
			}
			credential.URI = "ate-secret://k8s.io/default/team/other/token"
			opts := krt.NewOptionsBuilder(t.Context().Done(), "test", nil)
			grants := krt.NewStaticCollection(nil, []policyGrants{{Key: "agent/team/source", UID: "uid", Access: egress.Access{Destinations: base.Destinations, Credentials: []egress.Credential{credential}}}}, opts.WithName("Grants")...)
			resolver := &EgressResolver{grants: grants, store: &egressTestStore{pinned: pinned}}
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
	grant := policyGrants{Key: "agent/team/source", UID: "uid", Access: egress.Access{Destinations: []string{"https://tickets.example.com:443"}}}
	grants := krt.NewStaticCollection(nil, []policyGrants{grant}, opts.WithName("Grants")...)
	store := &egressTestStore{pinned: database.RuntimeEgress{Kind: "agent", Namespace: "team", SourceName: "source", SourceUID: "uid"}, targets: []database.RuntimeEgressTarget{{ID: "instance", Revision: "old-revision", Atespace: "team", TemplateName: "runtime"}}}
	resolver := &EgressResolver{grants: grants, store: store}
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
