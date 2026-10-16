package auth

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kagent-dev/kagent/go/api/authorization"
	"github.com/stretchr/testify/require"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// fakeReviewer answers each SubjectAccessReview from decide and records what
// it was asked.
type fakeReviewer struct {
	mu     sync.Mutex
	decide func(authorizationv1.SubjectAccessReviewSpec) (bool, error)
	asked  []authorizationv1.SubjectAccessReviewSpec
	// stalled answers no review: each waits until its context ends, as a
	// request to an API server that stopped answering does.
	stalled bool
}

func (f *fakeReviewer) Create(ctx context.Context, review *authorizationv1.SubjectAccessReview, _ metav1.CreateOptions) (*authorizationv1.SubjectAccessReview, error) {
	f.mu.Lock()
	f.asked = append(f.asked, review.Spec)
	f.mu.Unlock()
	if f.stalled {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	allowed, err := f.decide(review.Spec)
	if err != nil {
		return nil, err
	}
	result := review.DeepCopy()
	result.Status = authorizationv1.SubjectAccessReviewStatus{Allowed: allowed}
	if !allowed {
		result.Status.Reason = "no RoleBinding"
	}
	return result, nil
}

func (f *fakeReviewer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.asked)
}

type fakeNamespaces struct {
	names    []string
	selector string
	lists    int
}

func (f *fakeNamespaces) List(_ context.Context, options metav1.ListOptions) (*corev1.NamespaceList, error) {
	f.lists++
	f.selector = options.LabelSelector
	list := &corev1.NamespaceList{}
	for _, name := range f.names {
		list.Items = append(list.Items, corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})
	}
	return list, nil
}

func allowAll(authorizationv1.SubjectAccessReviewSpec) (bool, error) { return true, nil }

func allowIn(namespaces ...string) func(authorizationv1.SubjectAccessReviewSpec) (bool, error) {
	return func(spec authorizationv1.SubjectAccessReviewSpec) (bool, error) {
		return slices.Contains(namespaces, spec.ResourceAttributes.Namespace), nil
	}
}

func newTestAuthorizer(t *testing.T, reviewer *fakeReviewer, namespaces *fakeNamespaces, config KubernetesConfig) *KubernetesAuthorizer {
	t.Helper()
	authorizer, err := NewKubernetesAuthorizer(reviewer, namespaces, config)
	require.NoError(t, err)
	return authorizer
}

var alice = Principal{User: User{ID: "alice"}}

func TestKubernetesAuthorizerConstruction(t *testing.T) {
	reviewer := &fakeReviewer{decide: allowIn()}
	for _, test := range []struct {
		name       string
		reviewer   SubjectAccessReviewer
		namespaces NamespaceLister
		config     KubernetesConfig
		wantErr    string
	}{
		{name: "no reviewer", namespaces: &fakeNamespaces{}, wantErr: "SubjectAccessReview client"},
		{name: "no namespaces", reviewer: reviewer, wantErr: "Namespace client"},
		{name: "named namespaces need no lister", reviewer: reviewer, config: KubernetesConfig{Namespaces: []string{"team-a"}}},
		{name: "negative TTL", reviewer: reviewer, namespaces: &fakeNamespaces{}, config: KubernetesConfig{DecisionTTL: -time.Second}, wantErr: "TTL"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewKubernetesAuthorizer(test.reviewer, test.namespaces, test.config)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

func TestKubernetesAuthorizerCheckReviewsTheObject(t *testing.T) {
	for _, test := range []struct {
		name     string
		verb     Verb
		resource Resource
		want     authorizationv1.ResourceAttributes
	}{
		{name: "agent template", verb: VerbGet, resource: Resource{Type: "AgentTemplate", Namespace: "team-a", Name: "assistant"},
			want: authorizationv1.ResourceAttributes{Group: "api.kagent.dev", Resource: "agenttemplates", Verb: "get", Namespace: "team-a", Name: "assistant"}},
		{name: "agent", verb: VerbGet, resource: Resource{Type: "Agent", Namespace: "team-a", Name: "assistant"},
			want: authorizationv1.ResourceAttributes{Group: "api.kagent.dev", Resource: "agents", Verb: "get", Namespace: "team-a", Name: "assistant"}},
		{name: "harness update", verb: VerbUpdate, resource: Resource{Type: "Harness", Namespace: "kagent", Name: "claude"},
			want: authorizationv1.ResourceAttributes{Group: "api.kagent.dev", Resource: "harnesses", Verb: "update", Namespace: "kagent", Name: "claude"}},
		{name: "prompt template", verb: VerbGet, resource: Resource{Type: "PromptTemplate", Namespace: "kagent", Name: "greeting"},
			want: authorizationv1.ResourceAttributes{Resource: "configmaps", Verb: "get", Namespace: "kagent", Name: "greeting"}},
		{name: "new session", verb: VerbCreate, resource: Resource{Type: "Session", Namespace: "team-a"},
			want: authorizationv1.ResourceAttributes{Group: "api.kagent.dev", Resource: "sessions", Verb: "create", Namespace: "team-a"}},
		{name: "cluster-wide list", verb: VerbList, resource: Resource{Type: "ModelConfig"},
			want: authorizationv1.ResourceAttributes{Group: "api.kagent.dev", Resource: "modelconfigs", Verb: "list"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			reviewer := &fakeReviewer{decide: allowAll}
			authorizer := newTestAuthorizer(t, reviewer, &fakeNamespaces{}, KubernetesConfig{})
			require.NoError(t, authorizer.Check(t.Context(), alice, test.verb, test.resource))
			require.Len(t, reviewer.asked, 1)
			require.Equal(t, "alice", reviewer.asked[0].User)
			require.Equal(t, []string{AuthenticatedGroup}, reviewer.asked[0].Groups)
			require.Equal(t, test.want, *reviewer.asked[0].ResourceAttributes)
		})
	}
}

func TestKubernetesAuthorizerCheckDecisions(t *testing.T) {
	for _, test := range []struct {
		name        string
		decide      func(authorizationv1.SubjectAccessReviewSpec) (bool, error)
		wantErr     string
		unavailable bool
	}{
		{name: "allowed", decide: allowAll},
		{name: "denied", decide: allowIn(), wantErr: "alice may not get agenttemplates.api.kagent.dev assistant in namespace team-a: no RoleBinding"},
		{name: "review failed", decide: func(authorizationv1.SubjectAccessReviewSpec) (bool, error) {
			return false, errors.New("the API server is unreachable")
		}, wantErr: "the authorization review is unavailable: failed to review get agenttemplates.api.kagent.dev assistant in namespace team-a for alice: the API server is unreachable", unavailable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reviewer := &fakeReviewer{decide: test.decide}
			authorizer := newTestAuthorizer(t, reviewer, &fakeNamespaces{}, KubernetesConfig{})
			err := authorizer.Check(t.Context(), alice, VerbGet, Resource{Type: "AgentTemplate", Namespace: "team-a", Name: "assistant"})
			if test.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, test.wantErr)
			}
			// A refusal is final; an answer the API server did not give is not.
			require.Equal(t, test.unavailable, errors.Is(err, ErrReviewUnavailable))
			require.Equal(t, 1, reviewer.count())
		})
	}
}

func TestKubernetesAuthorizerCheckOutsideKubernetes(t *testing.T) {
	for _, test := range []struct {
		name      string
		principal Principal
		verb      Verb
		resource  Resource
		wantErr   string
	}{
		{name: "the control plane", principal: ControlPlaneSession{}.Principal(), verb: VerbCreate, resource: Resource{Type: "Session", Namespace: "team-a"}},
		{name: "an existing session's turn", principal: alice, verb: VerbCreate, resource: Resource{Type: "Session", Name: "b1c2"}},
		{name: "a session read", principal: alice, verb: VerbGet, resource: Resource{Type: "Session", Name: "b1c2"}},
		{name: "a session delete", principal: alice, verb: VerbDelete, resource: Resource{Type: "Session", Name: "b1c2"}},
		{name: "a checkpoint", principal: alice, verb: VerbGet, resource: Resource{Type: "Checkpoint", Name: "c3"}},
		{name: "a checkpoint fork", principal: alice, verb: VerbCreate, resource: Resource{Type: "Checkpoint", Name: "c3"}},
		{name: "a scheduled run", principal: alice, verb: VerbCreate, resource: Resource{Type: "ScheduledRun"}},
		{name: "a sandbox", principal: alice, verb: VerbCreate, resource: Resource{Type: "Sandbox"}},
		{name: "the tool server types", principal: alice, verb: VerbGet, resource: Resource{Type: "ToolServerType"}},
		{name: "the Substrate inventory", principal: alice, verb: VerbGet, resource: Resource{Type: "Substrate"}},
		{name: "an unknown type", principal: alice, verb: VerbGet, resource: Resource{Type: "Widget"}, wantErr: `unknown resource type "Widget"`},
		{name: "a runtime actor", principal: Principal{Agent: Agent{ID: "b1c2"}}, verb: VerbGet, resource: Resource{Type: "Agent", Namespace: "team-a", Name: "assistant"}, wantErr: "no user identity"},
		{name: "claims without a user", principal: Principal{Claims: map[string]any{"email": "alice@example.com"}}, verb: VerbGet, resource: Resource{Type: "Agent", Namespace: "team-a", Name: "assistant"}, wantErr: "no user identity"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reviewer := &fakeReviewer{decide: allowIn()}
			authorizer := newTestAuthorizer(t, reviewer, &fakeNamespaces{}, KubernetesConfig{})
			err := authorizer.Check(t.Context(), test.principal, test.verb, test.resource)
			if test.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.wantErr)
			}
			require.Zero(t, reviewer.count(), "no review is sent for a resource Kubernetes does not hold or a caller it cannot name")
		})
	}
}

func TestKubernetesAuthorizerSubjectMapping(t *testing.T) {
	for _, test := range []struct {
		name       string
		config     KubernetesConfig
		principal  Principal
		wantUser   string
		wantGroups []string
		wantErr    string
	}{
		{name: "no groups", principal: alice, wantUser: "alice", wantGroups: []string{AuthenticatedGroup}},
		{name: "no groups with prefix", config: KubernetesConfig{UsernamePrefix: "oidc:"}, principal: alice, wantUser: "oidc:alice", wantGroups: []string{AuthenticatedGroup}},
		{name: "the user id and groups, not the claims", principal: Principal{User: User{ID: "alice@example.com"}, Claims: map[string]any{"sub": "sub-1", "groups": []any{"team-b"}}},
			wantUser: "alice@example.com", wantGroups: []string{AuthenticatedGroup}},
		{name: "groups and prefixes", config: KubernetesConfig{UsernamePrefix: "oidc:", GroupsPrefix: "oidc:"},
			principal: Principal{User: User{ID: "alice@example.com", Groups: []string{"team-a", "admins"}}, Claims: map[string]any{"email": "alice@example.com"}},
			wantUser:  "oidc:alice@example.com", wantGroups: []string{"oidc:admins", "oidc:team-a", AuthenticatedGroup}},
		{name: "an agent's call for its user", config: KubernetesConfig{UsernamePrefix: "oidc:", GroupsPrefix: "oidc:"},
			principal: Principal{User: User{ID: "alice@example.com", Groups: []string{"team-a"}}, Agent: Agent{ID: "assistant"}},
			wantUser:  "oidc:alice@example.com", wantGroups: []string{"oidc:team-a", AuthenticatedGroup}},
		{name: "a ServiceAccount's name", principal: Principal{User: User{ID: "system:serviceaccount:kagent:kagent-controller"}, Agent: Agent{ID: "assistant"}},
			wantErr: "reserved for Kubernetes"},
		{name: "a ServiceAccount's name under a prefix", config: KubernetesConfig{UsernamePrefix: "oidc:"},
			principal: Principal{User: User{ID: "system:serviceaccount:kagent:kagent-controller"}},
			wantUser:  "oidc:system:serviceaccount:kagent:kagent-controller", wantGroups: []string{AuthenticatedGroup}},
		{name: "a reserved group", principal: Principal{User: User{ID: "alice", Groups: []string{"team-a", "system:masters"}}},
			wantErr: "reserved for Kubernetes"},
		{name: "a ServiceAccount the API server authenticated", config: KubernetesConfig{UsernamePrefix: "oidc:", GroupsPrefix: "oidc:"},
			principal: Principal{User: User{ID: "system:serviceaccount:workspace-manager:workspace-manager", Kubernetes: true,
				Groups: []string{"system:serviceaccounts", "system:serviceaccounts:workspace-manager", AuthenticatedGroup}}},
			wantUser:   "system:serviceaccount:workspace-manager:workspace-manager",
			wantGroups: []string{AuthenticatedGroup, "system:serviceaccounts", "system:serviceaccounts:workspace-manager"}},
		{name: "a reserved group under a prefix", config: KubernetesConfig{GroupsPrefix: "oidc:"},
			principal: Principal{User: User{ID: "alice", Groups: []string{"system:masters"}}},
			wantUser:  "alice", wantGroups: []string{"oidc:system:masters", AuthenticatedGroup}},
	} {
		t.Run(test.name, func(t *testing.T) {
			reviewer := &fakeReviewer{decide: allowAll}
			authorizer := newTestAuthorizer(t, reviewer, &fakeNamespaces{}, test.config)
			err := authorizer.Check(t.Context(), test.principal, VerbGet, Resource{Type: "Agent", Namespace: "team-a", Name: "assistant"})
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				require.Zero(t, reviewer.count(), "an identity that does not map is not reviewed")
				return
			}
			require.NoError(t, err)
			require.Len(t, reviewer.asked, 1)
			require.Equal(t, test.wantUser, reviewer.asked[0].User)
			require.Equal(t, test.wantGroups, reviewer.asked[0].Groups)
		})
	}
}

func TestKubernetesAuthorizerSessionsOfEveryCreator(t *testing.T) {
	workspaceManager := Principal{User: User{ID: "system:serviceaccount:workspace-manager:workspace-manager", Kubernetes: true,
		Groups: []string{AuthenticatedGroup, "system:serviceaccounts"}}}
	// Only the ServiceAccount is granted list on sessions, cluster-wide.
	grantedServiceAccount := func(spec authorizationv1.SubjectAccessReviewSpec) (bool, error) {
		return spec.User == workspaceManager.User.ID, nil
	}
	for _, test := range []struct {
		name      string
		principal Principal
		wantErr   string
	}{
		{name: "a caller granted list on sessions cluster-wide", principal: workspaceManager},
		{name: "a person without that grant", principal: alice, wantErr: "alice may not list sessions.api.kagent.dev: no RoleBinding"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reviewer := &fakeReviewer{decide: grantedServiceAccount}
			authorizer := newTestAuthorizer(t, reviewer, &fakeNamespaces{}, KubernetesConfig{})
			err := authorizer.Check(t.Context(), test.principal, VerbGet, Resource{Type: "SessionAllCreators"})
			if test.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, test.wantErr)
			}
			require.Len(t, reviewer.asked, 1)
			require.Equal(t, authorizationv1.ResourceAttributes{Group: "api.kagent.dev", Resource: "sessions", Verb: "list"}, *reviewer.asked[0].ResourceAttributes)
		})
	}
}

func TestKubernetesAuthorizerCachesDecisions(t *testing.T) {
	reviewer := &fakeReviewer{decide: allowIn("team-a")}
	authorizer := newTestAuthorizer(t, reviewer, &fakeNamespaces{}, KubernetesConfig{DecisionTTL: 5 * time.Second})
	now := time.Now()
	authorizer.now = func() time.Time { return now }
	allowed := Resource{Type: "Agent", Namespace: "team-a", Name: "assistant"}
	denied := Resource{Type: "Agent", Namespace: "team-b", Name: "assistant"}

	require.NoError(t, authorizer.Check(t.Context(), alice, VerbGet, allowed))
	require.NoError(t, authorizer.Check(t.Context(), alice, VerbGet, allowed))
	require.Error(t, authorizer.Check(t.Context(), alice, VerbGet, denied))
	require.Error(t, authorizer.Check(t.Context(), alice, VerbGet, denied))
	require.Equal(t, 2, reviewer.count(), "a fresh decision is reused, allowed and denied alike")

	// Another subject, verb or resource is its own decision.
	bob := Principal{User: User{ID: "bob"}}
	require.NoError(t, authorizer.Check(t.Context(), bob, VerbGet, allowed))
	require.NoError(t, authorizer.Check(t.Context(), alice, VerbUpdate, allowed))
	require.Equal(t, 4, reviewer.count())
	require.Len(t, authorizer.decisions, 4)

	now = now.Add(5 * time.Second)
	require.NoError(t, authorizer.Check(t.Context(), alice, VerbGet, allowed))
	require.Equal(t, 5, reviewer.count(), "an expired decision is reviewed again")
	require.Len(t, authorizer.decisions, 1, "the expired decisions are swept once the TTL has passed")
}

func TestKubernetesAuthorizerScope(t *testing.T) {
	for _, test := range []struct {
		name         string
		decide       func(authorizationv1.SubjectAccessReviewSpec) (bool, error)
		resourceType string
		want         authorization.AuthorizationScope
		wantErr      string
		wantReviews  int
	}{
		{name: "cluster-wide grant", decide: allowAll,
			resourceType: "AgentTemplate", want: authorization.AuthorizationScope{Kind: authorization.ScopeAll}, wantReviews: 1},
		{name: "some namespaces", decide: allowIn("team-a", "team-c"), resourceType: "AgentTemplate",
			want: authorization.AuthorizationScope{Kind: authorization.ScopeAnyOf, AnyOf: []authorization.ScopeClause{{All: []authorization.ScopePredicate{
				{Attribute: authorization.AttributeNamespace, Operator: authorization.ScopeIn, Values: []string{"team-a", "team-c"}},
			}}}}, wantReviews: 4},
		{name: "no namespace", decide: allowIn(), resourceType: "AgentTemplate", want: authorization.AuthorizationScope{Kind: authorization.ScopeNone}, wantReviews: 4},
		{name: "the sessions of every creator", decide: allowIn(), resourceType: "SessionAllCreators", want: authorization.AuthorizationScope{Kind: authorization.ScopeNone}},
		{name: "an owner-scoped type", decide: allowIn(), resourceType: "Session", want: authorization.AuthorizationScope{Kind: authorization.ScopeAll}},
		{name: "an unknown type", decide: allowIn(), resourceType: "Widget", wantErr: `unknown resource type "Widget"`},
		{name: "a failed review", decide: func(spec authorizationv1.SubjectAccessReviewSpec) (bool, error) {
			if spec.ResourceAttributes.Namespace == "team-b" {
				return false, errors.New("the API server is unreachable")
			}
			return false, nil
		}, resourceType: "AgentTemplate", wantErr: "the API server is unreachable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reviewer := &fakeReviewer{decide: test.decide}
			namespaces := &fakeNamespaces{names: []string{"team-c", "team-a", "team-b"}}
			authorizer := newTestAuthorizer(t, reviewer, namespaces, KubernetesConfig{})
			scope, err := authorizer.Scope(t.Context(), alice, VerbList, test.resourceType)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, scope)
			require.Equal(t, test.wantReviews, reviewer.count())
			if test.wantReviews > 0 {
				require.Equal(t, "list", reviewer.asked[0].ResourceAttributes.Verb)
				require.Equal(t, "agenttemplates", reviewer.asked[0].ResourceAttributes.Resource)
				require.Empty(t, reviewer.asked[0].ResourceAttributes.Namespace, "the cluster-wide review comes first")
			}
		})
	}
}

func TestKubernetesAuthorizerScopeNamespaces(t *testing.T) {
	t.Run("the control plane sees everything without a review", func(t *testing.T) {
		reviewer := &fakeReviewer{decide: allowIn()}
		authorizer := newTestAuthorizer(t, reviewer, &fakeNamespaces{names: []string{"team-a"}}, KubernetesConfig{})
		scope, err := authorizer.Scope(t.Context(), ControlPlaneSession{}.Principal(), VerbList, "AgentTemplate")
		require.NoError(t, err)
		require.Equal(t, authorization.AuthorizationScope{Kind: authorization.ScopeAll}, scope)
		require.Zero(t, reviewer.count())
	})
	t.Run("the named namespaces are reviewed, none listed", func(t *testing.T) {
		reviewer := &fakeReviewer{decide: allowIn("team-b")}
		namespaces := &fakeNamespaces{names: []string{"team-z"}}
		authorizer := newTestAuthorizer(t, reviewer, namespaces, KubernetesConfig{Namespaces: []string{"team-a", "team-b"}})
		scope, err := authorizer.Scope(t.Context(), alice, VerbList, "Agent")
		require.NoError(t, err)
		require.Equal(t, []string{"team-b"}, scope.AnyOf[0].All[0].Values)
		require.Zero(t, namespaces.lists)
	})
	t.Run("the listed namespaces follow the selector and are reused", func(t *testing.T) {
		reviewer := &fakeReviewer{decide: allowIn("team-a")}
		namespaces := &fakeNamespaces{names: []string{"team-a"}}
		selector, err := labels.Parse("tenant=true")
		require.NoError(t, err)
		authorizer := newTestAuthorizer(t, reviewer, namespaces, KubernetesConfig{NamespaceSelector: selector, DecisionTTL: 5 * time.Second})
		now := time.Now()
		authorizer.now = func() time.Time { return now }
		for range 2 {
			scope, err := authorizer.Scope(t.Context(), alice, VerbList, "Agent")
			require.NoError(t, err)
			require.Equal(t, []string{"team-a"}, scope.AnyOf[0].All[0].Values)
		}
		require.Equal(t, "tenant=true", namespaces.selector)
		require.Equal(t, 1, namespaces.lists, "the roster is listed once per TTL")
		require.Equal(t, 2, reviewer.count(), "the reviews are cached with it")
		now = now.Add(5 * time.Second)
		_, err = authorizer.Scope(t.Context(), alice, VerbList, "Agent")
		require.NoError(t, err)
		require.Equal(t, 2, namespaces.lists)
	})
}

func TestKubernetesAuthorizerSharesAReviewInFlight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		reviewer := &fakeReviewer{decide: func(authorizationv1.SubjectAccessReviewSpec) (bool, error) {
			<-release
			return true, nil
		}}
		authorizer := newTestAuthorizer(t, reviewer, &fakeNamespaces{}, KubernetesConfig{})
		resource := Resource{Type: "Agent", Namespace: "team-a", Name: "assistant"}
		errs := make(chan error, 4)
		check := func() { errs <- authorizer.Check(t.Context(), alice, VerbGet, resource) }
		go check()
		synctest.Wait()
		for range 3 {
			go check()
		}
		synctest.Wait()
		close(release)
		for range 4 {
			require.NoError(t, <-errs)
		}
		require.Equal(t, 1, reviewer.count(), "concurrent misses of one decision send one review")
	})
}

func TestKubernetesAuthorizerStalledAPIServer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authorizer := newTestAuthorizer(t, &fakeReviewer{stalled: true}, &fakeNamespaces{}, KubernetesConfig{})
		resource := Resource{Type: "Agent", Namespace: "team-a", Name: "assistant"}

		// A caller with a deadline leaves at it, before the shared review ends.
		start := time.Now()
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		err := authorizer.Check(ctx, alice, VerbGet, resource)
		require.ErrorIs(t, err, ErrReviewUnavailable)
		require.Equal(t, time.Second, time.Since(start))

		// A caller without one is answered unavailable when the review's own
		// bound ends it.
		start = time.Now()
		err = authorizer.Check(t.Context(), alice, VerbGet, resource)
		require.ErrorIs(t, err, ErrReviewUnavailable)
		require.Equal(t, reviewTimeout-time.Second, time.Since(start), "the second caller shares the review the first one started")
	})
}

func TestKubernetesAuthorizerScopeOfARefusedIdentity(t *testing.T) {
	for _, principal := range []Principal{
		{User: User{ID: "alice", Groups: []string{"system:masters"}}},
		{User: User{ID: "system:serviceaccount:kagent:kagent-controller"}},
		{Agent: Agent{ID: "b1c2"}},
	} {
		reviewer := &fakeReviewer{decide: allowAll}
		authorizer := newTestAuthorizer(t, reviewer, &fakeNamespaces{names: []string{"team-a"}}, KubernetesConfig{})
		scope, err := authorizer.Scope(t.Context(), principal, VerbList, "Agent")
		require.NoError(t, err)
		require.Equal(t, authorization.AuthorizationScope{Kind: authorization.ScopeNone}, scope, "a refused identity is allowed nowhere")
		require.Zero(t, reviewer.count(), "an identity that does not map is not reviewed")
	}
}

func TestKubernetesAuthorizerScopeIncludedNamespaces(t *testing.T) {
	reviewer := &fakeReviewer{decide: allowIn("kagent", "org-a")}
	namespaces := &fakeNamespaces{names: []string{"org-a", "org-b"}}
	selector, err := labels.Parse("giantswarm.io/organization")
	require.NoError(t, err)
	authorizer := newTestAuthorizer(t, reviewer, namespaces, KubernetesConfig{NamespaceSelector: selector, IncludedNamespaces: []string{"kagent", "org-a"}})
	scope, err := authorizer.Scope(t.Context(), alice, VerbList, "Agent")
	require.NoError(t, err)
	require.Equal(t, []string{"kagent", "org-a"}, scope.AnyOf[0].All[0].Values, "an included namespace is ranged over beside the selected ones, once")
	require.Equal(t, 4, reviewer.count(), "one cluster-wide review and one per namespace")
}
