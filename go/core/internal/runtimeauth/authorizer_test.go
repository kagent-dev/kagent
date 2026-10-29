package runtimeauth_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/authorization"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/jwttest"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type denyUsers struct{ auth.NoopAuthorizer }

func (denyUsers) Check(context.Context, auth.Principal, auth.Verb, auth.Resource) error {
	return fmt.Errorf("user denied")
}

func (denyUsers) Scope(context.Context, auth.Principal, auth.Verb, string) (authorization.AuthorizationScope, error) {
	return authorization.AuthorizationScope{Kind: authorization.ScopeNone}, nil
}

func TestActorPolicyDoesNotInheritOwnerPermissions(t *testing.T) {
	issuer := jwttest.New(t)
	verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile, substrate.RuntimeJWTAudience)
	require.NoError(t, err)
	store := newBinding()
	provider := runtimeauth.NewProvider(&authimpl.InsecureAuthenticator{}, verifier, store, issuer.URL)
	identity, err := provider.Authenticate(t.Context(), http.Header{"Authorization": {"Bearer " + issuer.Token(t, issuer.Claims("team-a", substrate.ActorName(store.session.Id), store.actorUID))}}, nil)
	require.NoError(t, err)
	ctx := auth.AuthSessionTo(t.Context(), identity)
	for _, users := range []auth.CollectionAuthorizer{auth.NoopAuthorizer{}, denyUsers{}} {
		policy := runtimeauth.NewAuthorizer(users, store)
		for _, test := range []struct {
			name     string
			verb     auth.Verb
			resource auth.Resource
			allowed  bool
		}{
			{"own agent", auth.VerbGet, auth.Resource{Type: "Agent", Namespace: "team-a", Name: "agent"}, true},
			{"other agent", auth.VerbGet, auth.Resource{Type: "Agent", Namespace: "team-a", Name: "other"}, false},
			{"other namespace", auth.VerbGet, auth.Resource{Type: "Agent", Namespace: "other", Name: "agent"}, false},
			{"mutate agent", auth.VerbUpdate, auth.Resource{Type: "Agent", Namespace: "team-a", Name: "agent"}, false},
			{"own session", auth.VerbGet, auth.Resource{Type: "Session", Name: store.session.Id}, true},
			{"own session send", auth.VerbCreate, auth.Resource{Type: "Session", Name: store.session.Id}, true},
			{"own session continue", auth.VerbUpdate, auth.Resource{Type: "Session", Name: store.session.Id}, true},
			{"create checkpoint", auth.VerbCreate, auth.Resource{Type: "Checkpoint"}, false},
			{"session collection", auth.VerbGet, auth.Resource{Type: "Session"}, true},
			{"session list category", auth.VerbList, auth.Resource{Type: "Session"}, true},
			{"session share administration", auth.VerbList, auth.Resource{Type: "Session", Name: store.session.Id + "/shares"}, false},
			{"other session", auth.VerbGet, auth.Resource{Type: "Session", Name: uuid.NewString()}, false},
			{"new session", auth.VerbCreate, auth.Resource{Type: "Session"}, false},
			{"all creators", auth.VerbGet, auth.Resource{Type: "SessionAllCreators"}, false},
			{"all creators list", auth.VerbList, auth.Resource{Type: "SessionAllCreators"}, false},
			{"delete session", auth.VerbDelete, auth.Resource{Type: "Session", Name: store.session.Id}, false},
			{"share", auth.VerbCreate, auth.Resource{Type: "SessionShare", Name: store.session.Id}, false},
			{"own memory", auth.VerbList, auth.Resource{Type: "Memory", Name: "team-a/agent"}, true},
			{"other memory", auth.VerbDelete, auth.Resource{Type: "Memory", Name: "team-a/other"}, false},
			{"sandbox", auth.VerbGet, auth.Resource{Type: "Sandbox", Name: store.session.Id}, false},
			{"namespaces", auth.VerbGet, auth.Resource{Type: "SystemNamespaces"}, false},
			{"unknown", auth.VerbGet, auth.Resource{Type: "Extension"}, false},
		} {
			t.Run(test.name, func(t *testing.T) {
				err := policy.Check(ctx, identity.Principal(), test.verb, test.resource)
				if test.allowed {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			})
		}
		scope, err := policy.Scope(ctx, identity.Principal(), auth.VerbList, "Agent")
		require.NoError(t, err)
		require.Equal(t, authorization.AuthorizationScope{Kind: authorization.ScopeAnyOf, AnyOf: []authorization.ScopeClause{{All: []authorization.ScopePredicate{
			{Attribute: authorization.AttributeNamespace, Operator: authorization.ScopeIn, Values: []string{"team-a"}},
			{Attribute: authorization.AttributeName, Operator: authorization.ScopeIn, Values: []string{"agent"}},
		}}}}, scope)
		scope, err = policy.Scope(ctx, identity.Principal(), auth.VerbList, "SandboxTemplate")
		require.NoError(t, err)
		require.Equal(t, authorization.ScopeNone, scope.Kind)
	}

	policy := runtimeauth.NewAuthorizer(denyUsers{}, store)
	user, err := provider.Authenticate(t.Context(), http.Header{"X-User-Id": {"owner"}}, nil)
	require.NoError(t, err)
	require.EqualError(t, policy.Check(auth.AuthSessionTo(t.Context(), user), user.Principal(), auth.VerbGet, auth.Resource{Type: "Session", Name: store.session.Id}), "user denied")
	store.actorUID = "replacement"
	require.Error(t, policy.Check(ctx, identity.Principal(), auth.VerbGet, auth.Resource{Type: "Session", Name: store.session.Id}))
	_, err = policy.Scope(ctx, identity.Principal(), auth.VerbList, "Agent")
	require.Error(t, err)
}

type actorSessionListStore struct {
	*database.Client
	*bindingStore
	sessions  []*apiv1.Session
	listCalls int
}

func (s *actorSessionListStore) ListSessions(_ context.Context, query database.SessionQuery) ([]*apiv1.Session, error) {
	s.listCalls++
	var result []*apiv1.Session
	for _, session := range s.sessions {
		if session.Creator == query.UserID && session.Id > query.AfterID {
			result = append(result, session)
		}
	}
	return result[:min(len(result), query.Limit)], nil
}

func (s *actorSessionListStore) GetSession(_ context.Context, id, owner string) (*apiv1.Session, error) {
	if id == s.session.Id && owner == s.session.Creator {
		return s.session, nil
	}
	return nil, database.ErrNotFound
}

func TestActorCanListOnlyItsScheduledSession(t *testing.T) {
	issuer := jwttest.New(t)
	verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile, substrate.RuntimeJWTAudience)
	require.NoError(t, err)
	binding := newBinding()
	binding.session.ScheduledRun = &apiv1.ScheduledRunProvenance{Ref: &apiv1.ResourceReference{Namespace: "team-a", Name: "daily"}, Uid: uuid.NewString(), ExecutionId: uuid.NewString()}
	other := &apiv1.Session{Id: uuid.NewString(), Creator: binding.session.Creator, ScheduledRun: binding.session.GetScheduledRun()}
	store := &actorSessionListStore{bindingStore: binding, sessions: []*apiv1.Session{binding.session, other}}
	slices.SortFunc(store.sessions, func(a, b *apiv1.Session) int { return strings.Compare(a.Id, b.Id) })
	provider := runtimeauth.NewProvider(&authimpl.InsecureAuthenticator{}, verifier, binding, issuer.URL)
	identity, err := provider.Authenticate(t.Context(), http.Header{"Authorization": {"Bearer " + issuer.Token(t, issuer.Claims("team-a", substrate.ActorName(binding.session.Id), binding.actorUID))}}, nil)
	require.NoError(t, err)
	service := sessionsvc.NewService(store, runtimeauth.NewAuthorizer(denyUsers{}, binding), nil)
	ctx := auth.AuthSessionTo(t.Context(), identity)
	result, err := service.List(ctx, sessionsvc.ListRequest{PageSize: 1})
	require.NoError(t, err)
	require.Equal(t, []*apiv1.Session{binding.session}, result.Sessions)
	require.Empty(t, result.NextPageToken)
	require.Zero(t, store.listCalls, "an Actor must not scan scheduler history")
	_, err = service.List(ctx, sessionsvc.ListRequest{AllCreators: true})
	require.Error(t, err)
}

func TestSchedulerBypassesUserPolicy(t *testing.T) {
	policy := runtimeauth.NewAuthorizer(denyUsers{}, nil)
	scheduler := auth.NewSchedulerSession("kagent", nil)
	resource := auth.Resource{Type: "ScheduledRunInvocation", Namespace: "team", Name: "daily"}

	ctx := auth.AuthSessionTo(t.Context(), scheduler)
	require.NoError(t, policy.Check(ctx, scheduler.Principal(), auth.VerbCreate, resource))
	scope, err := policy.Scope(ctx, scheduler.Principal(), auth.VerbList, "Session")
	require.NoError(t, err)
	require.Equal(t, authorization.ScopeAll, scope.Kind)

	// Only the in-process session type confers the bypass, and only for itself.
	require.Error(t, policy.Check(ctx, auth.Principal{ID: "other"}, auth.VerbCreate, resource))
	forged := auth.AuthSessionTo(t.Context(), auth.NewCredentialSession(scheduler.Principal(), nil))
	require.Error(t, policy.Check(forged, scheduler.Principal(), auth.VerbCreate, resource))
	other := auth.NewSchedulerSession("other-installation", nil)
	require.Error(t, policy.Check(auth.AuthSessionTo(t.Context(), other), scheduler.Principal(), auth.VerbCreate, resource))
}
