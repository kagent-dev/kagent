package grpcserver

import (
	"context"
	"net"
	"testing"
	"time"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/agentinstance"
	"github.com/kagent-dev/kagent/go/core/internal/service/scheduledrun"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Uses the generated client, actual interceptors, and PostgreSQL. Kubernetes
// target lookup is faked; runtime execution is outside this reservation slice.
func TestScheduledRunServicePersistence(t *testing.T) {
	store, client, instances, owner := scheduledRunTestServer(t)
	visitor := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "bob"))
	request := &apiv1alpha1.CreateScheduledRunRequest{
		Harness: &apiv1alpha1.ResourceReference{Namespace: "team", Name: "runtime"}, AgentTemplate: &apiv1alpha1.ResourceReference{Namespace: "team", Name: "report"}, RequestId: "create",
		Config: &apiv1alpha1.ScheduledRunConfig{Schedule: "* * * * *", Prompt: "original"},
	}
	created, err := client.CreateScheduledRun(owner, request)
	require.NoError(t, err)
	schedule := created.ScheduledRun
	require.Equal(t, "alice", schedule.Creator)
	require.Equal(t, "UTC", schedule.Config.TimeZone)
	require.Equal(t, 15*time.Minute, schedule.Config.ExecutionTimeout.AsDuration())
	require.NotNil(t, schedule.NextExecutionTime)
	_, err = uuid.Parse(schedule.Id)
	require.NoError(t, err)
	_, err = client.GetScheduledRun(visitor, &apiv1alpha1.GetScheduledRunRequest{ScheduledRunId: schedule.Id})
	require.Equal(t, codes.NotFound, status.Code(err))
	listed, err := client.ListScheduledRuns(visitor, &apiv1alpha1.ListScheduledRunsRequest{})
	require.NoError(t, err)
	require.Empty(t, listed.ScheduledRuns)

	config := proto.CloneOf(schedule.Config)
	config.Paused = true
	updated, err := client.UpdateScheduledRun(owner, &apiv1alpha1.UpdateScheduledRunRequest{ScheduledRunId: schedule.Id, Etag: schedule.Etag, Config: config})
	require.NoError(t, err)
	require.Nil(t, updated.ScheduledRun.NextExecutionTime)
	_, err = client.UpdateScheduledRun(owner, &apiv1alpha1.UpdateScheduledRunRequest{ScheduledRunId: schedule.Id, Etag: schedule.Etag, Config: config})
	require.Equal(t, codes.Aborted, status.Code(err))
	trigger := &apiv1alpha1.TriggerScheduledRunRequest{ScheduledRunId: schedule.Id, RequestId: "manual"}
	reserved, err := client.TriggerScheduledRun(owner, trigger)
	require.NoError(t, err)
	require.NotEmpty(t, reserved.Execution.Id)
	require.Equal(t, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_PENDING, reserved.Execution.State)
	require.Equal(t, "original", reserved.Execution.Prompt)
	_, err = client.TriggerScheduledRun(visitor, trigger)
	require.Equal(t, codes.NotFound, status.Code(err))
	_, err = client.GetScheduledRunExecution(visitor, &apiv1alpha1.GetScheduledRunExecutionRequest{ExecutionId: reserved.Execution.Id})
	require.Equal(t, codes.NotFound, status.Code(err))
	second, err := client.TriggerScheduledRun(owner, &apiv1alpha1.TriggerScheduledRunRequest{ScheduledRunId: schedule.Id, RequestId: "second"})
	require.NoError(t, err)
	firstPage, err := client.ListScheduledRunExecutions(owner, &apiv1alpha1.ListScheduledRunExecutionsRequest{ScheduledRunId: schedule.Id, Page: &apiv1alpha1.PageRequest{Limit: 1}})
	require.NoError(t, err)
	require.Len(t, firstPage.Executions, 1)
	require.Equal(t, second.Execution.Id, firstPage.Executions[0].Id, "newest firing is visible without paging through history")
	require.NotEmpty(t, firstPage.Page.NextPageToken)
	secondPage, err := client.ListScheduledRunExecutions(owner, &apiv1alpha1.ListScheduledRunExecutionsRequest{ScheduledRunId: schedule.Id, Page: &apiv1alpha1.PageRequest{Limit: 1, PageToken: firstPage.Page.NextPageToken}})
	require.NoError(t, err)
	require.Len(t, secondPage.Executions, 1)
	require.Equal(t, reserved.Execution.Id, secondPage.Executions[0].Id)
	require.Empty(t, secondPage.Page.NextPageToken)
	_, err = client.DeleteScheduledRun(owner, &apiv1alpha1.DeleteScheduledRunRequest{ScheduledRunId: schedule.Id})
	require.NoError(t, err)
	replayed, err := client.TriggerScheduledRun(owner, trigger)
	require.NoError(t, err)
	require.True(t, proto.Equal(reserved, replayed))
	trigger.RequestId = "after-delete"
	_, err = client.TriggerScheduledRun(owner, trigger)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	history, err := client.ListScheduledRunExecutions(owner, &apiv1alpha1.ListScheduledRunExecutionsRequest{ScheduledRunId: schedule.Id})
	require.NoError(t, err)
	require.Len(t, history.Executions, 2)
	require.True(t, proto.Equal(reserved.Execution, history.Executions[1]))
	privateHistory, err := client.ListScheduledRunExecutions(visitor, &apiv1alpha1.ListScheduledRunExecutionsRequest{ScheduledRunId: schedule.Id})
	require.NoError(t, err)
	require.Empty(t, privateHistory.Executions)
	// The interceptor must reject malformed IDs before handlers convert to UUIDs.
	for _, id := range []string{"", "invalid"} {
		_, err = client.GetScheduledRun(owner, &apiv1alpha1.GetScheduledRunRequest{ScheduledRunId: id})
		require.Equal(t, codes.InvalidArgument, status.Code(err), "get schedule: %v", err)
		_, err = client.UpdateScheduledRun(owner, &apiv1alpha1.UpdateScheduledRunRequest{ScheduledRunId: id, Etag: schedule.Etag, Config: config})
		require.Equal(t, codes.InvalidArgument, status.Code(err), "update schedule: %v", err)
		_, err = client.DeleteScheduledRun(owner, &apiv1alpha1.DeleteScheduledRunRequest{ScheduledRunId: id})
		require.Equal(t, codes.InvalidArgument, status.Code(err), "delete schedule: %v", err)
		_, err = client.TriggerScheduledRun(owner, &apiv1alpha1.TriggerScheduledRunRequest{ScheduledRunId: id, RequestId: "invalid-id"})
		require.Equal(t, codes.InvalidArgument, status.Code(err), "trigger schedule: %v", err)
		_, err = client.GetScheduledRunExecution(owner, &apiv1alpha1.GetScheduledRunExecutionRequest{ExecutionId: id})
		require.Equal(t, codes.InvalidArgument, status.Code(err), "get execution: %v", err)
		_, err = client.ListScheduledRunExecutions(owner, &apiv1alpha1.ListScheduledRunExecutionsRequest{ScheduledRunId: id})
		require.Equal(t, codes.InvalidArgument, status.Code(err), "list executions: %v", err)
	}
	// Execution history survives deleting the linked conversation and schedule.
	linked, err := store.ReserveScheduledRunExecutionInstance(t.Context(), uuid.MustParse(reserved.Execution.Id), "alice")
	require.NoError(t, err)
	require.NotEmpty(t, linked.AgentInstanceId)
	require.NoError(t, store.DeleteAgentInstance(t.Context(), linked.AgentInstanceId))
	_, err = instances.GetAgentInstance(owner, &apiv1alpha1.GetAgentInstanceRequest{AgentInstanceId: linked.AgentInstanceId})
	require.Equal(t, codes.NotFound, status.Code(err))
	loaded, err := client.GetScheduledRunExecution(owner, &apiv1alpha1.GetScheduledRunExecutionRequest{ExecutionId: linked.Id})
	require.NoError(t, err)
	require.True(t, proto.Equal(linked, loaded.Execution))
	trigger.RequestId = "manual"
	replayed, err = client.TriggerScheduledRun(owner, trigger)
	require.NoError(t, err)
	require.True(t, proto.Equal(linked, replayed.Execution))
	creationRetry, err := client.CreateScheduledRun(owner, request)
	require.NoError(t, err)
	require.Equal(t, schedule.Id, creationRetry.ScheduledRun.Id)
	require.NotNil(t, creationRetry.ScheduledRun.DeletedAt)
	changed := proto.CloneOf(request)
	changed.Config.Prompt = "different"
	_, err = client.CreateScheduledRun(owner, changed)
	require.Equal(t, codes.AlreadyExists, status.Code(err))

	for _, tc := range []struct {
		name   string
		change func(*apiv1alpha1.CreateScheduledRunRequest)
	}{
		{"invalid namespace", func(r *apiv1alpha1.CreateScheduledRunRequest) { r.Harness.Namespace = "Bad/Namespace" }},
		{"missing config", func(r *apiv1alpha1.CreateScheduledRunRequest) { r.Config = nil }},
		{"blank prompt", func(r *apiv1alpha1.CreateScheduledRunRequest) { r.Config.Prompt = " \n\t" }},
		{"negative duration", func(r *apiv1alpha1.CreateScheduledRunRequest) {
			r.Config.ExecutionTimeout = durationpb.New(-time.Second)
		}},
		{"sub-microsecond duration", func(r *apiv1alpha1.CreateScheduledRunRequest) {
			r.Config.ExecutionTimeout = durationpb.New(time.Nanosecond)
		}},
		{"overflow duration", func(r *apiv1alpha1.CreateScheduledRunRequest) {
			r.Config.ExecutionTimeout = &durationpb.Duration{Seconds: 10000000000}
		}},
		{"invalid cron", func(r *apiv1alpha1.CreateScheduledRunRequest) { r.Config.Schedule = "@hourly" }},
		{"invalid zone", func(r *apiv1alpha1.CreateScheduledRunRequest) { r.Config.TimeZone = "Bad/Zone" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := proto.CloneOf(request)
			invalid.RequestId = tc.name
			tc.change(invalid)
			_, err := client.CreateScheduledRun(owner, invalid)
			require.Equal(t, codes.InvalidArgument, status.Code(err), "%v", err)
		})
	}
}

func scheduledRunTestServer(t *testing.T) (*database.Client, apiv1alpha1.ScheduledRunServiceClient, apiv1alpha1.AgentInstanceServiceClient, context.Context) {
	t.Helper()
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	dsn := dbtest.StartT(t.Context(), t)
	dbtest.MigrateT(t, dsn, false)
	db, err := database.Connect(t.Context(), &database.PostgresConfig{URL: dsn})
	require.NoError(t, err)
	t.Cleanup(db.Close)
	store := database.NewClient(db)
	pair := database.AgentTemplateHarnessPair{Namespace: "team", AgentTemplateName: "report", AgentTemplateUID: "template-uid", HarnessName: "runtime", HarnessUID: "harness-uid", DesiredRevision: "scheduled-revision"}
	require.NoError(t, store.UpsertRuntimeRevision(t.Context(), database.RuntimeRevision{
		Revision: pair.DesiredRevision, Namespace: pair.Namespace, AgentTemplateName: pair.AgentTemplateName, AgentTemplateUID: pair.AgentTemplateUID,
		HarnessName: pair.HarnessName, HarnessUID: pair.HarnessUID, SourceSnapshot: []byte("{}"), AgentCard: &a2apb.AgentCard{}, EgressDestinations: []string{},
		ActorTemplateAtespace: "team", ActorTemplateName: "runtime", ActorTemplateUID: "runtime-uid",
	}))
	require.NoError(t, store.UpsertAgentTemplateHarnessPair(t.Context(), pair))
	require.NoError(t, store.MarkRuntimeRevisionSuccessful(t.Context(), pair))
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha3.AddToScheme(scheme))
	harness := testHarness("team", "runtime", "pool")
	harness.Spec.AllowedAgentTemplates = &v1alpha3.HarnessAgentTemplateAdmission{Selector: metav1.LabelSelector{}}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(harness, testAgentTemplate("team", "report", "model")).Build()
	listener := bufconn.Listen(DefaultMaxMessageSize)
	server, err := New(Config{
		Listener: listener, Registerer: prometheus.NewRegistry(), Authenticator: &authimpl.UnsecureAuthenticator{},
		SystemService:        testSystemService(),
		ScheduledRunService:  scheduledrun.NewService(store, kube, &authimpl.NoopAuthorizer{}),
		AgentInstanceService: agentinstance.NewService(store, &authimpl.NoopAuthorizer{}, nil),
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
	client := apiv1alpha1.NewScheduledRunServiceClient(connection)
	instances := apiv1alpha1.NewAgentInstanceServiceClient(connection)
	owner := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "alice"))
	return store, client, instances, owner
}
