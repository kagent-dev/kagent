package sandbox_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/agent-substrate/env/guest"
	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/jackc/pgx/v5/pgxpool"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	sandboxservice "github.com/kagent-dev/kagent/go/core/internal/service/sandbox"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type testSession string

func (s testSession) Principal() auth.Principal {
	return auth.Principal{User: auth.User{ID: string(s)}}
}

type testAuth struct{}

func (testAuth) Authenticate(_ context.Context, headers http.Header, _ url.Values) (auth.Session, error) {
	if user := headers.Get("x-user-id"); user != "" {
		return testSession(user), nil
	}
	return nil, status.Error(codes.Unauthenticated, "missing test identity")
}

func (testAuth) UpstreamAuth(request *http.Request, _ auth.Session, principal auth.Principal) error {
	request.Header.Set("X-User-Id", principal.User.ID)
	return nil
}

type testActors struct {
	substrate.LifecycleClient
	mu          sync.Mutex
	actor       *ateapipb.Actor
	calls       []string
	readErr     error
	mutationErr error
}

func (a *testActors) GetActor(context.Context, string, string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.readErr != nil {
		return nil, a.readErr
	}
	if a.actor == nil {
		return nil, status.Error(codes.NotFound, "missing")
	}
	return proto.CloneOf(a.actor), nil
}
func (a *testActors) CreateActor(_ context.Context, space, name, templateSpace, templateName string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "create")
	a.actor = &ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{Atespace: space, Name: name, Uid: "uid"},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: templateSpace, Name: templateName},
		Status:        &ateapipb.ActorStatus{State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED}}
	return proto.CloneOf(a.actor), a.mutationErr
}
func (a *testActors) EnsureActorEgressPolicy(context.Context, string, string, *ateapipb.EgressPolicy) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "policy")
	return a.mutationErr
}
func (a *testActors) ResumeActor(context.Context, string, string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "resume")
	a.actor.Status.State = ateapipb.ActorState_ACTOR_STATE_RUNNING
	return proto.CloneOf(a.actor), a.mutationErr
}
func (a *testActors) SuspendActor(context.Context, string, string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "suspend")
	a.actor.Status.State = ateapipb.ActorState_ACTOR_STATE_SUSPENDED
	return proto.CloneOf(a.actor), a.mutationErr
}
func (a *testActors) DeleteActor(context.Context, string, string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "delete")
	a.actor = nil
	return a.mutationErr
}

func (a *testActors) observedCalls() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.calls)
}

func guestFixture(t *testing.T) (*sandboxservice.Service, context.Context, *testActors, func() http.Header) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	t.Cleanup(cancel)
	conn, cleanup, err := dbtest.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	require.NoError(t, dbtest.Migrate(conn, false))
	pool, err := pgxpool.New(t.Context(), conn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store := database.NewClient(pool)
	require.NoError(t, store.UpsertSandboxTemplateDefinition(ctx, database.SandboxTemplateDefinition{Namespace: "team-a", SandboxTemplateName: "scratch", SandboxTemplateUID: "template-uid", DesiredRevision: "revision"}))
	require.NoError(t, store.RecordSandboxRevision(ctx, database.SandboxRevision{
		RuntimeArtifact:     database.RuntimeArtifact{Revision: "revision", Kind: "sandbox", Namespace: "team-a", ActorTemplateAtespace: "team-a", ActorTemplateName: "revision", ActorTemplateUID: "revision-uid"},
		SandboxTemplateName: "scratch", SandboxTemplateUID: "template-uid", SourceSnapshot: []byte("{}"),
	}, true))
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha3.AddToScheme(scheme))
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&v1alpha3.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "scratch", UID: "template-uid"},
	}).Build()
	server, closeGuest, err := guest.NewServer(guest.Config{Workspace: t.TempDir(), LogDir: t.TempDir(), EnableProcess: true, EnableFileSystem: true})
	require.NoError(t, err)
	t.Cleanup(closeGuest)
	t.Cleanup(server.Stop)
	var mu sync.Mutex
	var lastHeaders http.Header
	router := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		mu.Lock()
		lastHeaders = request.Header.Clone()
		mu.Unlock()
		server.ServeHTTP(w, request)
	}))
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	router.Config.Protocols = protocols
	router.Start()
	t.Cleanup(router.Close)
	guests, err := sandboxservice.NewGuestDialer(router.URL, testAuth{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, guests.Close()) })
	actors := &testActors{}
	service, err := sandboxservice.NewService(sandboxservice.Config{Store: store, Kube: kube, Authorizer: auth.NoopAuthorizer{}, Actors: actors, Guests: guests,
		DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour})
	require.NoError(t, err)
	return service, auth.AuthSessionTo(t.Context(), testSession("alice")), actors, func() http.Header {
		mu.Lock()
		defer mu.Unlock()
		return lastHeaders.Clone()
	}
}

func createRequest() *apiv1alpha1.CreateSandboxRequest {
	return &apiv1alpha1.CreateSandboxRequest{SandboxTemplate: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "scratch"}, RequestId: "create"}
}

func TestSandboxGuestLifecycle(t *testing.T) {
	service, ctx, actors, headers := guestFixture(t)
	instance, err := service.Create(ctx, createRequest())
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, instance.State)
	require.Equal(t, []string{"create", "policy", "resume"}, actors.observedCalls())
	retry, err := service.Create(ctx, createRequest())
	require.NoError(t, err)
	require.Equal(t, instance.Id, retry.Id)
	require.True(t, proto.Equal(instance.ExpiresAt, retry.ExpiresAt))
	require.Len(t, actors.observedCalls(), 3)

	mallory := auth.AuthSessionTo(t.Context(), testSession("mallory"))
	_, err = service.Get(mallory, instance.Id)
	require.ErrorIs(t, err, database.ErrNotFound)
	_, err = service.StartProcess(mallory, instance.Id, &guestpb.StartProcessRequest{})
	require.ErrorIs(t, err, database.ErrNotFound)
	list, err := service.List(mallory, &apiv1alpha1.ListSandboxesRequest{})
	require.NoError(t, err)
	require.Empty(t, list.Sandboxes)

	// Guest routing derives from storage, even with caller-supplied metadata.
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("ate-target-actor", "other/victim", "authorization", "untrusted", "x-user-id", "mallory"))
	messages := []*guestpb.WriteFileRequest{
		{Path: "input.bin", Mode: 0o600},
		{Chunk: []byte{0, 128, 255}},
	}
	written, err := service.WriteFile(ctx, instance.Id, func() (*guestpb.WriteFileRequest, error) {
		if len(messages) == 0 {
			return nil, io.EOF
		}
		next := messages[0]
		messages = messages[1:]
		return next, nil
	})
	require.NoError(t, err)
	require.EqualValues(t, 3, written.BytesWritten)
	require.Equal(t, "team-a/"+substrate.ActorName(instance.Id), headers().Get("ate-target-actor"))
	require.Equal(t, "alice", headers().Get("x-user-id"))
	require.Empty(t, headers().Get("authorization"))
	var data []byte
	require.NoError(t, service.ReadFile(ctx, instance.Id, &guestpb.ReadFileRequest{Path: "input.bin"}, func(chunk *guestpb.FileChunk) error { data = append(data, chunk.Data...); return nil }))
	require.Equal(t, []byte{0, 128, 255}, data)

	start := &guestpb.StartProcessRequest{Command: []string{"sh", "-c", "printf once >> count; cat count"}}
	process, err := service.StartProcess(ctx, instance.Id, start)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		current, err := service.GetProcess(ctx, instance.Id, &guestpb.GetProcessRequest{ProcessId: process.ProcessId})
		return err == nil && current.Status == guestpb.ProcessStatus_PROCESS_STATUS_COMPLETED
	}, 5*time.Second, 10*time.Millisecond)
	var output []byte
	require.NoError(t, service.StreamProcessOutputs(ctx, instance.Id, &guestpb.StreamProcessOutputsRequest{ProcessId: process.ProcessId}, func(chunk *guestpb.OutputChunk) error {
		output = append(output, chunk.Data...)
		return nil
	}))
	require.Equal(t, "once", string(output))
	again, err := service.StartProcess(ctx, instance.Id, start)
	require.NoError(t, err)
	require.NotEqual(t, process.ProcessId, again.ProcessId)
	output = nil
	require.NoError(t, service.StreamProcessOutputs(ctx, instance.Id, &guestpb.StreamProcessOutputsRequest{ProcessId: again.ProcessId, Follow: true}, func(chunk *guestpb.OutputChunk) error {
		output = append(output, chunk.Data...)
		return nil
	}))
	require.Equal(t, "onceonce", string(output), "each start executes a new command")
	_, err = service.GetProcess(mallory, instance.Id, &guestpb.GetProcessRequest{ProcessId: process.ProcessId})
	require.ErrorIs(t, err, database.ErrNotFound)
	_, err = service.KillProcess(mallory, instance.Id, &guestpb.KillProcessRequest{ProcessId: process.ProcessId})
	require.ErrorIs(t, err, database.ErrNotFound)
	_, err = service.GetProcess(ctx, instance.Id, &guestpb.GetProcessRequest{ProcessId: "unknown-guest-process"})
	require.Equal(t, codes.NotFound, status.Code(err))
	err = service.StreamProcessOutputs(ctx, instance.Id, &guestpb.StreamProcessOutputsRequest{ProcessId: "unknown-guest-process"}, func(*guestpb.OutputChunk) error {
		t.Fatal("unknown process returned output")
		return nil
	})
	require.Equal(t, codes.NotFound, status.Code(err))
	running, err := service.StartProcess(ctx, instance.Id, &guestpb.StartProcessRequest{Command: []string{"sleep", "60"}})
	require.NoError(t, err)
	_, err = service.KillProcess(ctx, instance.Id, &guestpb.KillProcessRequest{ProcessId: running.ProcessId})
	require.NoError(t, err)
	killed, err := service.GetProcess(ctx, instance.Id, &guestpb.GetProcessRequest{ProcessId: running.ProcessId})
	require.NoError(t, err)
	require.Equal(t, running.ProcessId, killed.ProcessId)
	require.Equal(t, guestpb.ProcessStatus_PROCESS_STATUS_TERMINATED, killed.Status)

	// Neither an active process nor a streaming file write reserves a lifecycle
	// boundary. The caller accepts interruption when suspending this sandbox.
	_, err = service.StartProcess(ctx, instance.Id, &guestpb.StartProcessRequest{Command: []string{"sleep", "60"}})
	require.NoError(t, err)
	writeStarted, finishWrite := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(finishWrite) })
	t.Cleanup(unblock)
	writeResult := make(chan error, 1)
	go func() {
		first := true
		_, err := service.WriteFile(ctx, instance.Id, func() (*guestpb.WriteFileRequest, error) {
			if first {
				first = false
				return &guestpb.WriteFileRequest{Path: "streaming"}, nil
			}
			close(writeStarted)
			<-finishWrite
			return nil, io.ErrUnexpectedEOF
		})
		writeResult <- err
	}()
	select {
	case <-writeStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("file stream did not start")
	}
	instance, err = service.Suspend(ctx, instance.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_SUSPENDED, instance.State)
	unblock()
	require.ErrorIs(t, <-writeResult, io.ErrUnexpectedEOF)

	err = service.ReadFile(ctx, instance.Id, &guestpb.ReadFileRequest{Path: "input.bin"}, func(*guestpb.FileChunk) error { t.Fatal("suspended guest reached"); return nil })
	require.ErrorIs(t, err, database.ErrFailedPrecondition)
	instance, err = service.Resume(ctx, instance.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, instance.State)
	instance, err = service.Delete(ctx, instance.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETED, instance.State)
	deletedActor, err := actors.GetActor(ctx, "", "")
	require.Nil(t, deletedActor)
	require.Equal(t, codes.NotFound, status.Code(err))
	_, err = service.Create(ctx, createRequest())
	require.ErrorIs(t, err, database.ErrFailedPrecondition, "retry cannot resurrect a deleted workspace")
}
