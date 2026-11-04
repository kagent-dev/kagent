package grpcserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/jwttest"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/agentidentity"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

// Exercise the generated client, shared interceptors, real signature verification
// and lifecycle store. The lookup credential is accepted only by this RPC.
func TestAgentIdentityGeneratedClient(t *testing.T) {
	dsn := dbtest.StartT(context.WithoutCancel(t.Context()), t)
	dbtest.MigrateT(t, dsn, false)
	db, err := database.Connect(t.Context(), &database.PostgresConfig{Pool: dbtest.PoolConfig(dsn)})
	require.NoError(t, err)
	t.Cleanup(db.Close)
	store := database.NewClient(db, "public")
	session := createTaskStoreSession(t, store)
	agent := &v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "assistant", UID: "template-uid",
		Labels: map[string]string{"team": "payments"}, Annotations: map[string]string{"description": "helper"}},
		Spec: v1alpha3.AgentSpec{Template: v1alpha3.AgentTemplateSource{Inline: &v1alpha3.AgentTemplateSpec{SystemPrompt: "private runtime instructions"}}}}
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha3.AddToScheme(scheme))
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(agent).Build()
	issuer := jwttest.New(t)
	verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile, substrate.RuntimeJWTAudience)
	require.NoError(t, err)
	connection := newConfigurationConnection(t, kube, func(config *Config) {
		config.Authenticator = runtimeauth.NewProvider(&authimpl.InsecureAuthenticator{}, verifier, store, issuer.URL)
		config.AgentIdentityService = agentidentity.NewService(verifier, store, kube)
	})
	client := apiv1.NewAgentIdentityServiceClient(connection)
	claims := issuer.Claims("team-a", substrate.ActorName(session.Id), "actor-uid")
	claims["aud"] = []string{"downstream-api", agentidentity.Audience}
	token := issuer.Token(t, claims)
	lookupContext := func(raw string) context.Context {
		return metadata.AppendToOutgoingContext(t.Context(), "authorization", "Bearer "+raw)
	}
	response, err := client.ResolveAgentIdentity(lookupContext(token), &apiv1.ResolveAgentIdentityRequest{})
	require.NoError(t, err)
	require.True(t, proto.Equal(&apiv1.AgentIdentity{Namespace: agent.Namespace, Name: agent.Name, Uid: string(agent.UID), Labels: agent.Labels, Annotations: agent.Annotations}, response.Agent))
	require.Equal(t, session.Id, response.SessionId)
	require.Equal(t, session.PreparedRevision, response.RuntimeRevision)
	require.NotEmpty(t, response.RuntimeRevision)
	encoded, err := protojson.Marshal(response)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "spec")
	require.NotContains(t, string(encoded), "status")
	require.NotContains(t, string(encoded), "private runtime instructions")

	// Substrate allows up to 16 audiences of 512 Unicode characters. The
	// encoded JWT can exceed 16 KiB without exceeding those source limits.
	largeClaims := issuer.Claims("team-a", substrate.ActorName(session.Id), "actor-uid")
	largeAudiences := []string{agentidentity.Audience}
	for index := range 15 {
		largeAudiences = append(largeAudiences, strings.Repeat("界", 511)+string(rune('a'+index)))
	}
	largeClaims["aud"] = largeAudiences
	largeToken := issuer.Token(t, largeClaims)
	require.Greater(t, len(largeToken), 16384)
	_, err = client.ResolveAgentIdentity(lookupContext(largeToken), &apiv1.ResolveAgentIdentityRequest{})
	require.NoError(t, err)

	for _, test := range []struct {
		name     string
		metadata metadata.MD
	}{
		{name: "missing"},
		{name: "empty", metadata: metadata.Pairs("authorization", "")},
		{name: "empty bearer", metadata: metadata.Pairs("authorization", "Bearer ")},
		{name: "invalid", metadata: metadata.Pairs("authorization", "Bearer invalid-token")},
		{name: "oversized", metadata: metadata.Pairs("authorization", "Bearer "+strings.Repeat("x", 131073))},
		{name: "duplicate", metadata: metadata.Pairs("authorization", "Bearer "+token, "authorization", "Bearer "+token)},
		{name: "non-bearer", metadata: metadata.Pairs("authorization", "Basic "+token)},
		{name: "combined", metadata: metadata.Pairs("authorization", "Bearer "+token+", Bearer "+token)},
		{name: "user header", metadata: metadata.Pairs("x-user-id", "forged-owner")},
		{name: "share header", metadata: metadata.Pairs("x-share-token", "share")},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := client.ResolveAgentIdentity(metadata.NewOutgoingContext(t.Context(), test.metadata), &apiv1.ResolveAgentIdentityRequest{})
			require.Equal(t, codes.Unauthenticated, status.Code(err))
			require.NotContains(t, err.Error(), token)
			require.NotContains(t, err.Error(), "invalid-token")
		})
	}
	_, err = client.ResolveAgentIdentity(metadata.NewOutgoingContext(t.Context(), metadata.Pairs("authorization", "bearer "+token)), &apiv1.ResolveAgentIdentityRequest{})
	require.NoError(t, err)

	// Even with the insecure user fallback, an actor-shaped token for this
	// audience must fail shared authentication before any handler can run.
	bearer := metadata.AppendToOutgoingContext(t.Context(), "authorization", "Bearer "+token, "x-user-id", "forged-owner")
	for _, method := range []string{
		apiv1.AgentService_GetAgent_FullMethodName,
		apiv1.SessionService_GetSession_FullMethodName,
		apiv1.SessionService_CreateSession_FullMethodName,
		apiv1.SessionService_DeleteSession_FullMethodName,
		apiv1.SystemService_GetVersion_FullMethodName,
		apiv1.ScheduledRunService_TriggerScheduledRun_FullMethodName,
		runtimev1.TaskStoreService_CreateTask_FullMethodName,
		runtimev1.MemoryService_AddSession_FullMethodName,
	} {
		err := connection.Invoke(bearer, method, &emptypb.Empty{}, &emptypb.Empty{})
		require.Equal(t, codes.Unauthenticated, status.Code(err), method)
	}
	_, err = client.ResolveAgentIdentity(bearer, &apiv1.ResolveAgentIdentityRequest{})
	require.NoError(t, err, "the lookup uses the Bearer credential and ignores the claimed user")

	for _, test := range []struct {
		name   string
		change func(jwt.MapClaims)
		want   codes.Code
	}{
		{name: "wrong audience", change: func(c jwt.MapClaims) { c["aud"] = []string{"downstream-api"} }, want: codes.Unauthenticated},
		{name: "expired", change: func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Minute).Unix() }, want: codes.Unauthenticated},
		{name: "wrong atespace", change: func(c jwt.MapClaims) {
			c["actor.ate.dev"].(map[string]string)["atespace"] = "other"
			c["sub"] = "actor/other/" + substrate.ActorName(session.Id)
		}, want: codes.PermissionDenied},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := issuer.Claims("team-a", substrate.ActorName(session.Id), "actor-uid")
			c["aud"] = []string{agentidentity.Audience}
			test.change(c)
			_, err := client.ResolveAgentIdentity(lookupContext(issuer.Token(t, c)), &apiv1.ResolveAgentIdentityRequest{})
			require.Equal(t, test.want, status.Code(err))
		})
	}
	_, err = client.ResolveAgentIdentity(lookupContext(issuer.Token(t, jwt.MapClaims{"iss": "https://users.example", "sub": "alice"})), &apiv1.ResolveAgentIdentityRequest{})
	require.Equal(t, codes.Unauthenticated, status.Code(err), "ordinary user credentials cannot resolve Agent identity")
	untrustedIssuer := jwttest.New(t)
	_, err = client.ResolveAgentIdentity(lookupContext(untrustedIssuer.Token(t, claims)), &apiv1.ResolveAgentIdentityRequest{})
	require.Equal(t, codes.Unauthenticated, status.Code(err), "an untrusted signature cannot resolve Agent identity")

	claims["aud"] = []string{substrate.RuntimeJWTAudience}
	_, err = client.ResolveAgentIdentity(lookupContext(issuer.Token(t, claims)), &apiv1.ResolveAgentIdentityRequest{})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	claims["aud"] = []string{agentidentity.Audience, substrate.RuntimeJWTAudience}
	_, err = client.ResolveAgentIdentity(lookupContext(issuer.Token(t, claims)), &apiv1.ResolveAgentIdentityRequest{})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	claims["aud"] = []string{agentidentity.Audience}
	claims["actor.ate.dev"].(map[string]string)["actorUID"] = "obsolete-actor"
	_, err = client.ResolveAgentIdentity(lookupContext(issuer.Token(t, claims)), &apiv1.ResolveAgentIdentityRequest{})
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	current := &v1alpha3.Agent{}
	require.NoError(t, kube.Get(t.Context(), types.NamespacedName{Namespace: agent.Namespace, Name: agent.Name}, current))
	current.Labels["team"] = "support"
	require.NoError(t, kube.Update(t.Context(), current))
	response, err = client.ResolveAgentIdentity(lookupContext(token), &apiv1.ResolveAgentIdentityRequest{})
	require.NoError(t, err)
	require.Equal(t, "support", response.Agent.Labels["team"], "metadata reads the current Agent, not a revision snapshot")
	require.Equal(t, session.Id, response.SessionId)
	require.Equal(t, session.PreparedRevision, response.RuntimeRevision)
	require.NoError(t, kube.Delete(t.Context(), current))
	_, err = client.ResolveAgentIdentity(lookupContext(token), &apiv1.ResolveAgentIdentityRequest{})
	require.Equal(t, codes.NotFound, status.Code(err), "deleted Agents cannot resolve metadata")
	replacement := agent.DeepCopy()
	replacement.UID = "replacement-agent"
	replacement.ResourceVersion = ""
	require.NoError(t, kube.Create(t.Context(), replacement))
	_, err = client.ResolveAgentIdentity(lookupContext(token), &apiv1.ResolveAgentIdentityRequest{})
	require.Equal(t, codes.NotFound, status.Code(err), "a recreated Agent cannot inherit the old Actor's binding")

	operation, err := store.BeginSessionOperation(t.Context(), session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
	require.NoError(t, err)
	executor := uuid.New()
	claimed, err := store.ClaimSessionOperation(t.Context(), session.Id, operation.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = store.FinishSessionOperation(t.Context(), session.Id, operation.ID, executor, "", "", "")
	require.NoError(t, err)
	_, err = client.ResolveAgentIdentity(lookupContext(token), &apiv1.ResolveAgentIdentityRequest{})
	require.Equal(t, codes.PermissionDenied, status.Code(err), "deleted Sessions cannot resolve metadata")
}
