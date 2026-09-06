package grpcserver

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/agentinstance"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

// Exercise generated clients, authorization, services and PostgreSQL together.
// Runtime calls are mocked; ActorWorkflow's deletion ordering has separate coverage.
func TestAgentInstanceTombstonesThroughGRPC(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	dsn := dbtest.StartT(t.Context(), t)
	dbtest.MigrateT(t, dsn, false)
	pool, err := database.Connect(t.Context(), &database.PostgresConfig{URL: dsn})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store := database.NewClient(pool)
	pair := dbpkg.AgentTemplateHarnessPair{Namespace: "team", AgentTemplateName: "assistant", AgentTemplateUID: "template-uid", HarnessName: "runtime", HarnessUID: "harness-uid", DesiredRevision: "revision"}
	require.NoError(t, store.UpsertRuntimeRevision(t.Context(), dbpkg.RuntimeRevision{
		Revision: pair.DesiredRevision, Namespace: pair.Namespace, AgentTemplateName: pair.AgentTemplateName, AgentTemplateUID: pair.AgentTemplateUID,
		HarnessName: pair.HarnessName, HarnessUID: pair.HarnessUID, SourceSnapshot: []byte("{}"), AgentCard: []byte("{}"), EgressDestinations: []string{},
		ActorTemplateAtespace: "team", ActorTemplateName: "runtime", ActorTemplateUID: "runtime-uid",
	}))
	require.NoError(t, store.UpsertAgentTemplateHarnessPair(t.Context(), pair))
	require.NoError(t, store.MarkRuntimeRevisionSuccessful(t.Context(), pair))
	workflow := &tombstoneTestWorkflow{store: store}
	listener := bufconn.Listen(DefaultMaxMessageSize)
	server, err := New(Config{
		Listener: listener, Registerer: prometheus.NewRegistry(), Authenticator: &authimpl.UnsecureAuthenticator{},
		SystemService: testSystemService(), AgentInstanceService: agentinstance.NewService(store, &authimpl.NoopAuthorizer{}, workflow),
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Start(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	client := apiv1alpha1.NewAgentInstanceServiceClient(conn)
	owner := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "alice"))
	visitor := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "bob"))
	request := &apiv1alpha1.CreateAgentInstanceRequest{Namespace: "team", Harness: "runtime", AgentTemplate: "assistant", RequestId: "create", Name: "Original instance"}
	created, err := client.CreateAgentInstance(owner, request)
	require.NoError(t, err)
	id := created.AgentInstance.Id
	share, err := client.CreateAgentInstanceShare(owner, &apiv1alpha1.CreateAgentInstanceShareRequest{Namespace: "team", AgentInstanceId: id, Permission: apiv1alpha1.AgentInstanceSharePermission_AGENT_INSTANCE_SHARE_PERMISSION_READ_ONLY})
	require.NoError(t, err)
	require.NotEmpty(t, share.Token)
	deletion := &apiv1alpha1.DeleteAgentInstanceRequest{Namespace: "team", AgentInstanceId: id}
	deleted, err := client.DeleteAgentInstance(owner, deletion)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_DELETED, deleted.AgentInstance.State)
	require.NotNil(t, deleted.AgentInstance.DeletedAt)
	require.Empty(t, deleted.AgentInstance.PreparedRevision)
	require.Empty(t, deleted.AgentInstance.A2AAuthority)
	again, err := client.DeleteAgentInstance(owner, deletion)
	require.NoError(t, err)
	require.True(t, proto.Equal(deleted, again))
	retry, err := client.CreateAgentInstance(owner, request)
	require.NoError(t, err)
	require.True(t, proto.Equal(deleted.AgentInstance, retry.AgentInstance))
	require.Equal(t, int32(1), workflow.creates.Load())
	require.Equal(t, int32(1), workflow.deletes.Load())
	get := &apiv1alpha1.GetAgentInstanceRequest{Namespace: "team", AgentInstanceId: id}
	_, err = client.GetAgentInstance(owner, get)
	require.Equal(t, codes.NotFound, status.Code(err))
	get.IncludeDeleted = true
	history, err := client.GetAgentInstance(owner, get)
	require.NoError(t, err)
	require.True(t, proto.Equal(deleted.AgentInstance, history.AgentInstance))
	_, err = client.GetAgentInstance(visitor, get)
	require.Equal(t, codes.NotFound, status.Code(err))
	list := &apiv1alpha1.ListAgentInstancesRequest{Namespace: "team", AgentTemplate: "assistant", Harness: "runtime"}
	live, err := client.ListAgentInstances(owner, list)
	require.NoError(t, err)
	require.Empty(t, live.AgentInstances)
	list.IncludeDeleted = true
	all, err := client.ListAgentInstances(owner, list)
	require.NoError(t, err)
	require.Len(t, all.AgentInstances, 1)
	private, err := client.ListAgentInstances(visitor, list)
	require.NoError(t, err)
	require.Empty(t, private.AgentInstances)
	_, err = client.SuspendAgentInstance(owner, &apiv1alpha1.SuspendAgentInstanceRequest{Namespace: "team", AgentInstanceId: id})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	_, err = client.ResumeAgentInstance(owner, &apiv1alpha1.ResumeAgentInstanceRequest{Namespace: "team", AgentInstanceId: id})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	_, err = client.UpdateAgentInstanceName(owner, &apiv1alpha1.UpdateAgentInstanceNameRequest{Namespace: "team", AgentInstanceId: id, Name: "Changed"})
	require.Equal(t, codes.NotFound, status.Code(err))
	shares, err := client.ListAgentInstanceShares(owner, &apiv1alpha1.ListAgentInstanceSharesRequest{Namespace: "team", AgentInstanceId: id})
	require.NoError(t, err)
	require.Empty(t, shares.Shares)
}

type tombstoneTestWorkflow struct {
	store            dbpkg.Client
	creates, deletes atomic.Int32
}

func (w *tombstoneTestWorkflow) Create(ctx context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	w.creates.Add(1)
	return w.store.MarkAgentInstanceReady(ctx, instance.Id, "runtime.example")
}
func (w *tombstoneTestWorkflow) Delete(ctx context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	w.deletes.Add(1)
	return w.store.DeleteAgentInstance(ctx, instance.Id)
}
func (*tombstoneTestWorkflow) Suspend(context.Context, *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	panic("deleted instance reached runtime")
}
func (*tombstoneTestWorkflow) Resume(context.Context, *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	panic("deleted instance reached runtime")
}
