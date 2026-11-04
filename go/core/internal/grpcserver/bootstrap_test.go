package grpcserver

import (
	"context"
	"strings"
	"testing"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	contract "github.com/kagent-dev/kagent/go/api/bootstrap"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/jwttest"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/bootstrap"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

type bootstrapBackend struct {
	actor    *ateapipb.Actor
	template *ateapipb.ActorTemplate
}

func (b *bootstrapBackend) GetActor(context.Context, string, string) (*ateapipb.Actor, error) {
	return b.actor, nil
}
func (b *bootstrapBackend) GetActorTemplate(context.Context, string, string) (*ateapipb.ActorTemplate, error) {
	return b.template, nil
}

func TestBootstrapGeneratedClient(t *testing.T) {
	// Exercise actual persistence, shared method policies/interceptors and signed
	// actor identity before any Session has been created.
	dsn := dbtest.StartT(context.WithoutCancel(t.Context()), t)
	dbtest.MigrateT(t, dsn, false)
	pool, err := database.Connect(t.Context(), &database.PostgresConfig{Pool: dbtest.PoolConfig(dsn)})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store := database.NewClient(pool, "public")
	issuer := jwttest.New(t)
	verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile, substrate.RuntimeJWTAudience)
	require.NoError(t, err)
	id := strings.Repeat("a", 64)
	bundle, err := contract.NewBundle(id, []byte(`{"instruction":"`+strings.Repeat("x", 128<<10)+`"}`), []byte(`{"description":"`+strings.Repeat("y", 40000)+`"}`))
	require.NoError(t, err)
	template := &ateapipb.ActorTemplate{Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "template", Uid: "template-uid"}, Containers: []*ateapipb.Container{{Name: "runtime", Image: "agent:v1"}}}
	digest, err := substrate.ActorTemplateSpecDigest(template)
	require.NoError(t, err)
	require.NoError(t, store.UpsertAgentDefinition(t.Context(), database.AgentDefinition{Namespace: "team-a", AgentName: "agent", AgentUID: "agent-uid", DesiredRevision: id}))
	revision := database.RuntimeRevision{Revision: id, Namespace: "team-a", AgentName: "agent", AgentUID: "agent-uid", SourceSnapshot: []byte(`{}`), AgentCard: &a2apb.AgentCard{Name: "agent"}, ActorTemplateAtespace: "team-a", ActorTemplateName: "template", ActorTemplateUID: "template-uid", ActorTemplateSpecDigest: digest}
	require.NoError(t, store.PublishBootstrapRevision(t.Context(), revision, bundle))
	_, err = store.RecordRuntimeRevision(t.Context(), revision, database.RecordRevisionOptions{})
	require.NoError(t, err)
	backend := &bootstrapBackend{template: template, actor: &ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "golden", Uid: "actor-uid"}, ActorTemplate: &ateapipb.ObjectRef{Atespace: "team-a", Name: "template"}}}
	kube := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()
	conn := newConfigurationConnection(t, kube, func(c *Config) {
		c.BootstrapService = bootstrap.NewService(verifier, store, backend)
		c.Authenticator = runtimeauth.NewProvider(&authimpl.InsecureAuthenticator{}, verifier, store, issuer.URL)
	})
	stub := runtimev1.NewBootstrapServiceClient(conn)
	claims := issuer.Claims("team-a", "golden", "actor-uid")
	claims["aud"] = []string{contract.Audience}
	ctx := metadata.AppendToOutgoingContext(t.Context(), "authorization", "Bearer "+issuer.Token(t, claims))
	got, err := stub.GetBootstrapConfig(ctx, &runtimev1.BootstrapServiceGetBootstrapConfigRequest{})
	require.NoError(t, err)
	require.True(t, proto.Equal(bundle, got.GetBundle()))
	_, err = stub.GetBootstrapConfig(t.Context(), &runtimev1.BootstrapServiceGetBootstrapConfigRequest{})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	claims["aud"] = []string{substrate.RuntimeJWTAudience}
	runtimeCtx := metadata.AppendToOutgoingContext(t.Context(), "authorization", "Bearer "+issuer.Token(t, claims))
	_, err = stub.GetBootstrapConfig(runtimeCtx, &runtimev1.BootstrapServiceGetBootstrapConfigRequest{})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	// Bootstrap credentials must not gain authority on user or runtime APIs.
	_, err = apiv1.NewSessionServiceClient(conn).GetSession(ctx, &apiv1.GetSessionRequest{SessionId: "00000000-0000-0000-0000-000000000001"})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	_, err = runtimev1.NewTaskStoreServiceClient(conn).GetTask(ctx, &runtimev1.TaskStoreServiceGetTaskRequest{})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	_, err = apiv1.NewAgentIdentityServiceClient(conn).ResolveAgentIdentity(ctx, &apiv1.ResolveAgentIdentityRequest{})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}
