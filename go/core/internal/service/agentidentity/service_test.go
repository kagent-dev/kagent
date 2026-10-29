package agentidentity_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/jwttest"
	"github.com/kagent-dev/kagent/go/core/internal/service/agentidentity"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

type identityStore struct {
	session  *apiv1.Session
	revision *database.RuntimeRevision
	calls    int
}

func (s *identityStore) GetSessionForRuntime(_ context.Context, id, actorUID string) (*apiv1.Session, error) {
	s.calls++
	if s.session == nil || s.session.Id != id || actorUID != "actor-uid" {
		return nil, database.ErrNotFound
	}
	return s.session, nil
}

func (s *identityStore) GetRuntimeRevision(_ context.Context, revision string) (*database.RuntimeRevision, error) {
	if s.revision.Revision != revision {
		return nil, database.ErrNotFound
	}
	return s.revision, nil
}

func TestResolveAgentIdentity(t *testing.T) {
	issuer := jwttest.New(t)
	// The service binds its own audience while sharing the runtime verifier's keys.
	verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile, substrate.RuntimeJWTAudience)
	require.NoError(t, err)
	for _, test := range []struct {
		name   string
		change func(jwt.MapClaims, *identityStore, *v1alpha3.Agent)
		want   serviceerrors.Code
	}{
		{name: "current metadata"},
		{name: "pinned revision despite Agent update", change: func(_ jwt.MapClaims, _ *identityStore, a *v1alpha3.Agent) {
			a.Status.DesiredRevision = "newer-revision"
			a.Status.LatestSuccessfulRevision = "newer-revision"
		}},
		{name: "runtime audience", change: func(c jwt.MapClaims, _ *identityStore, _ *v1alpha3.Agent) {
			c["aud"] = []string{substrate.RuntimeJWTAudience}
		}, want: serviceerrors.CodeUnauthenticated},
		{name: "mixed runtime authority", change: func(c jwt.MapClaims, _ *identityStore, _ *v1alpha3.Agent) {
			c["aud"] = []string{agentidentity.Audience, substrate.RuntimeJWTAudience}
		}, want: serviceerrors.CodeUnauthenticated},
		{name: "downstream audience only", change: func(c jwt.MapClaims, _ *identityStore, _ *v1alpha3.Agent) { c["aud"] = []string{"downstream-api"} }, want: serviceerrors.CodeUnauthenticated},
		{name: "expired", change: func(c jwt.MapClaims, _ *identityStore, _ *v1alpha3.Agent) {
			c["exp"] = time.Now().Add(-time.Minute).Unix()
		}, want: serviceerrors.CodeUnauthenticated},
		{name: "worker credential", change: func(c jwt.MapClaims, _ *identityStore, _ *v1alpha3.Agent) {
			c["worker-for-actor.ate.dev"] = c["actor.ate.dev"]
			delete(c, "actor.ate.dev")
		}, want: serviceerrors.CodeUnauthenticated},
		{name: "non-session actor", change: func(c jwt.MapClaims, _ *identityStore, _ *v1alpha3.Agent) {
			c["actor.ate.dev"].(map[string]string)["actorName"] = "sandbox-other"
			c["sub"] = "actor/team-a/sandbox-other"
		}, want: serviceerrors.CodePermissionDenied},
		{name: "obsolete actor UID", change: func(c jwt.MapClaims, _ *identityStore, _ *v1alpha3.Agent) {
			c["actor.ate.dev"].(map[string]string)["actorUID"] = "obsolete"
		}, want: serviceerrors.CodePermissionDenied},
		{name: "wrong atespace", change: func(c jwt.MapClaims, _ *identityStore, _ *v1alpha3.Agent) {
			actor := c["actor.ate.dev"].(map[string]string)
			actor["atespace"] = "other"
			c["sub"] = "actor/other/" + actor["actorName"]
		}, want: serviceerrors.CodePermissionDenied},
		{name: "deleted Session", change: func(_ jwt.MapClaims, s *identityStore, _ *v1alpha3.Agent) { s.session = nil }, want: serviceerrors.CodePermissionDenied},
		{name: "inconsistent revision", change: func(_ jwt.MapClaims, s *identityStore, _ *v1alpha3.Agent) { s.revision.AgentName = "other" }, want: serviceerrors.CodeInternal},
		{name: "missing Agent UID", change: func(_ jwt.MapClaims, s *identityStore, _ *v1alpha3.Agent) { s.revision.AgentUID = "" }, want: serviceerrors.CodeInternal},
		{name: "recreated Agent", change: func(_ jwt.MapClaims, _ *identityStore, a *v1alpha3.Agent) { a.UID = "replacement" }, want: serviceerrors.CodeNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			id := uuid.NewString()
			store := &identityStore{session: &apiv1.Session{Id: id, PreparedRevision: "revision", Agent: &apiv1.ResourceReference{Namespace: "team-a", Name: "assistant"}, A2AAuthority: substrate.ActorHost("team-a", substrate.ActorName(id), "")},
				revision: &database.RuntimeRevision{Revision: "revision", Namespace: "team-a", AgentName: "assistant", AgentUID: "agent-uid"}}
			agent := &v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "assistant", UID: "agent-uid",
				Labels: map[string]string{"team": "payments"}, Annotations: map[string]string{"description": "on-call helper", "kubectl.kubernetes.io/last-applied-configuration": `{"spec":{"example":"operator-supplied"}}`}}}
			claims := issuer.Claims("team-a", substrate.ActorName(id), "actor-uid")
			claims["aud"] = []string{"downstream-api", agentidentity.Audience}
			if test.change != nil {
				test.change(claims, store, agent)
			}
			scheme := runtime.NewScheme()
			require.NoError(t, v1alpha3.AddToScheme(scheme))
			kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(agent).Build()
			service := agentidentity.NewService(verifier, store, kube)
			ctx, err := service.Authenticate(t.Context(), http.Header{"Authorization": {"Bearer " + issuer.Token(t, claims)}})
			var result *apiv1.ResolveAgentIdentityResponse
			if err == nil {
				result, err = service.Resolve(ctx)
			}
			require.Equal(t, test.want, serviceerrors.CodeOf(err))
			if test.want != "" {
				require.Nil(t, result)
				if test.want == serviceerrors.CodeUnauthenticated {
					require.Zero(t, store.calls, "unverified credentials cannot inspect the Session store")
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, agent.Namespace, result.Agent.Namespace)
			require.Equal(t, agent.Name, result.Agent.Name)
			require.Equal(t, string(agent.UID), result.Agent.Uid)
			require.Equal(t, agent.Labels, result.Agent.Labels)
			require.Equal(t, agent.Annotations, result.Agent.Annotations)
			require.Equal(t, store.session.Id, result.SessionId)
			require.Equal(t, "revision", result.RuntimeRevision)
		})
	}
}

func TestResolveRequiresAuthenticatedIdentity(t *testing.T) {
	service := &agentidentity.Service{}
	result, err := service.Resolve(t.Context())
	require.Nil(t, result)
	require.Equal(t, serviceerrors.CodeUnauthenticated, serviceerrors.CodeOf(err))
}
