package grpcserver

import (
	"context"
	"net"
	"testing"
	"time"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	schedulesvc "github.com/kagent-dev/kagent/go/core/internal/service/scheduledrun"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func createScheduleRPC(t *testing.T, store *database.Client, client apiv1.ScheduledRunServiceClient, ctx context.Context, timeout time.Duration) *v1alpha3.ScheduledRun {
	t.Helper()
	resource := &v1alpha3.ScheduledRun{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "daily", UID: types.UID(uuid.NewString()), Generation: 1}, Spec: v1alpha3.ScheduledRunSpec{AgentRef: corev1.LocalObjectReference{Name: "assistant"}, Schedule: "* * * * *", Prompt: "immutable scheduled prompt", Paused: new(true), ExecutionTimeout: &metav1.Duration{Duration: timeout}}}
	object, err := structuredobject.FromGo(resource, v1alpha3.GroupVersion.String(), v1alpha3.ScheduledRunKind, DefaultMaxMessageSize)
	require.NoError(t, err)
	created, err := client.CreateScheduledRun(ctx, &apiv1.CreateScheduledRunRequest{Ref: &apiv1.ResourceReference{Namespace: resource.Namespace, Name: resource.Name}, Resource: object})
	require.NoError(t, err)
	require.NoError(t, structuredobject.ToGo(created.ScheduledRun.Resource, v1alpha3.ScheduledRunKind, resource, DefaultMaxMessageSize))
	// The projection is the reconciler's supported persistence boundary. Runtime
	// integration tests begin after that configuration handoff.
	projection := &database.ScheduledRun{ID: string(resource.UID), Creator: "scheduler", Ref: types.NamespacedName{Namespace: resource.Namespace, Name: resource.Name}, Generation: resource.Generation, ResourceVersion: resource.ResourceVersion, Spec: resource.Spec, AgentUID: "agent-uid"}
	recordScheduleRevision(t, store, projection)
	_, err = store.SyncScheduledRun(t.Context(), projection)
	require.NoError(t, err)
	return resource
}

func TestScheduledRunServiceCRDAndRetainedExecution(t *testing.T) {
	store, client, _, ctx := scheduledRunTestServer(t)
	resource := createScheduleRPC(t, store, client, ctx, time.Minute)
	ref := &apiv1.ResourceReference{Namespace: resource.Namespace, Name: resource.Name}
	got, err := client.GetScheduledRun(ctx, &apiv1.GetScheduledRunRequest{Ref: ref})
	require.NoError(t, err)
	require.Equal(t, "team", got.ScheduledRun.Ref.Namespace)
	trigger := &apiv1.TriggerScheduledRunRequest{ScheduledRunId: string(resource.UID), RequestId: "manual"}
	accepted, err := client.TriggerScheduledRun(ctx, trigger)
	require.NoError(t, err)
	require.Equal(t, "scheduler", accepted.Execution.Creator)
	require.Equal(t, "alice", accepted.Execution.Initiator)
	_, err = client.DeleteScheduledRun(ctx, &apiv1.DeleteScheduledRunRequest{Ref: ref, Uid: string(resource.UID), ResourceVersion: resource.ResourceVersion})
	require.NoError(t, err)
	require.NoError(t, store.RetireScheduledRun(t.Context(), uuid.MustParse(string(resource.UID))))
	history, err := client.ListScheduledRunExecutions(ctx, &apiv1.ListScheduledRunExecutionsRequest{ScheduledRunId: string(resource.UID)})
	require.NoError(t, err)
	require.Len(t, history.Executions, 1)
	require.Equal(t, accepted.Execution.Id, history.Executions[0].Id)
	replay, err := client.TriggerScheduledRun(ctx, trigger)
	require.NoError(t, err)
	require.Equal(t, accepted.Execution.Id, replay.Execution.Id)
}

func scheduledRunTestServer(t *testing.T) (*database.Client, apiv1.ScheduledRunServiceClient, apiv1.SessionServiceClient, context.Context) {
	t.Helper()
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	dsn := dbtest.StartT(t.Context(), t)
	dbtest.MigrateT(t, dsn, false)
	db, err := database.Connect(t.Context(), &database.PostgresConfig{Pool: dbtest.PoolConfig(dsn)})
	require.NoError(t, err)
	t.Cleanup(db.Close)
	store := database.NewClient(db, "public")
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha3.AddToScheme(scheme))
	kube := fake.NewClientBuilder().WithScheme(scheme).Build()
	listener := bufconn.Listen(DefaultMaxMessageSize)
	server, err := New(Config{
		Listener: listener, Authenticator: &authimpl.InsecureAuthenticator{},
		SystemService:       testSystemService(),
		ScheduledRunService: schedulesvc.NewService(store, kube, &pkgauth.NoopAuthorizer{}, nil),
		SessionService:      sessionsvc.NewService(store, &pkgauth.NoopAuthorizer{}, nil),
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Start(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	connection, err := grpc.NewClient("passthrough:///bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	client := apiv1.NewScheduledRunServiceClient(connection)
	sessions := apiv1.NewSessionServiceClient(connection)
	owner := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "alice"))
	return store, client, sessions, owner
}

func recordScheduleRevision(t *testing.T, store *database.Client, schedule *database.ScheduledRun) {
	t.Helper()
	agent := &apiv1.AgentProvenance{Ref: &apiv1.ResourceReference{Namespace: schedule.Ref.Namespace, Name: schedule.Spec.AgentRef.Name}, Uid: schedule.AgentUID}
	require.NoError(t, store.UpsertAgentDefinition(t.Context(), database.AgentDefinition{Namespace: agent.Ref.Namespace, AgentName: agent.Ref.Name, AgentUID: agent.Uid, DesiredRevision: schedule.ID}))
	_, err := store.RecordRuntimeRevision(t.Context(), database.RuntimeRevision{Namespace: agent.Ref.Namespace, AgentName: agent.Ref.Name, AgentUID: agent.Uid, Revision: schedule.ID,
		SourceSnapshot: []byte("{}"), AgentCard: &a2apb.AgentCard{}, EgressDestinations: []string{},
		ActorTemplateAtespace: agent.Ref.Namespace, ActorTemplateName: schedule.ID + "-runtime", ActorTemplateUID: schedule.ID + "-runtime-uid",
	}, database.RecordRevisionOptions{Ready: true})
	require.NoError(t, err)
}
