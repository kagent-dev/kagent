package a2agateway

import (
	"context"
	"errors"
	"iter"
	"net"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/v2/agentinstance"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

const gatewayTestID = "8bd650a8-9775-488f-8bc1-0d52bf7bdcab"

type gatewayTestSession struct{}

func (gatewayTestSession) Principal() auth.Principal {
	return auth.Principal{User: auth.User{ID: "alice"}}
}

type gatewayTestStore struct {
	instance              *apiv1alpha1.AgentInstance
	err                   error
	namespace, id, userID string
}

func (s *gatewayTestStore) GetAgentInstance(_ context.Context, namespace, id, userID string) (*apiv1alpha1.AgentInstance, error) {
	s.namespace, s.id, s.userID = namespace, id, userID
	return s.instance, s.err
}

type gatewayTestAuthorizer struct {
	verb     auth.Verb
	resource auth.Resource
}

func (a *gatewayTestAuthorizer) Check(_ context.Context, _ auth.Principal, verb auth.Verb, resource auth.Resource) error {
	a.verb, a.resource = verb, resource
	return nil
}

type gatewayTestDialer struct {
	client   *a2aclient.Client
	instance *apiv1alpha1.AgentInstance
	err      error
}

func (d *gatewayTestDialer) Dial(_ context.Context, instance *apiv1alpha1.AgentInstance) (*a2aclient.Client, error) {
	d.instance = instance
	return d.client, d.err
}

type gatewayTestRuntime struct {
	a2aclient.Transport
	sent      bool
	destroyed bool
}

func (r *gatewayTestRuntime) SendMessage(context.Context, a2aclient.ServiceParams, *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
	r.sent = true
	return &a2atype.Task{ID: "task-1"}, nil
}

func (r *gatewayTestRuntime) SendStreamingMessage(context.Context, a2aclient.ServiceParams, *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		yield(&a2atype.Task{ID: "task-1"}, nil)
	}
}

func (r *gatewayTestRuntime) Destroy() error {
	r.destroyed = true
	return nil
}

func gatewayTestClient(t *testing.T, runtime *gatewayTestRuntime) *a2aclient.Client {
	t.Helper()
	client, err := a2aclient.NewFromEndpoints(t.Context(), []*a2atype.AgentInterface{{
		URL:             "runtime.test",
		ProtocolBinding: a2atype.TransportProtocolGRPC,
		ProtocolVersion: a2atype.Version,
	}},
		a2aclient.WithDefaultsDisabled(),
		a2aclient.WithTransport(a2atype.TransportProtocolGRPC, a2aclient.TransportFactoryFn(func(context.Context, *a2atype.AgentCard, *a2atype.AgentInterface) (a2aclient.Transport, error) {
			return runtime, nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func gatewayTestContext(authority string) context.Context {
	ctx := auth.AuthSessionTo(context.Background(), gatewayTestSession{})
	return metadata.NewIncomingContext(ctx, metadata.Pairs(":authority", authority))
}

func gatewayTestInstance() *apiv1alpha1.AgentInstance {
	return &apiv1alpha1.AgentInstance{
		Id: gatewayTestID, Namespace: "team-a", Creator: "alice",
		A2AAuthority: agentinstance.Authority("team-a", gatewayTestID),
		State:        apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY,
	}
}

func TestGatewayResolvesAuthenticatedAuthorityBeforeSending(t *testing.T) {
	instance := gatewayTestInstance()
	store := &gatewayTestStore{instance: instance}
	authorizer := &gatewayTestAuthorizer{}
	runtime := &gatewayTestRuntime{}
	gateway := New(store, authorizer, &gatewayTestDialer{client: gatewayTestClient(t, runtime)})

	result, err := gateway.SendMessage(gatewayTestContext(instance.GetA2AAuthority()), &a2atype.SendMessageRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.(*a2atype.Task).ID != "task-1" || !runtime.sent || !runtime.destroyed {
		t.Fatalf("runtime result = %#v, sent %v, destroyed %v", result, runtime.sent, runtime.destroyed)
	}
	if store.namespace != "team-a" || store.id != gatewayTestID || store.userID != "alice" {
		t.Fatalf("store lookup = %q/%q user %q", store.namespace, store.id, store.userID)
	}
	if authorizer.verb != auth.VerbCreate || authorizer.resource != (auth.Resource{Type: "AgentInstance", Name: "team-a/" + gatewayTestID}) {
		t.Fatalf("authorization = %q %#v", authorizer.verb, authorizer.resource)
	}
}

func TestGatewayClosesRuntimeAfterStreaming(t *testing.T) {
	instance := gatewayTestInstance()
	runtime := &gatewayTestRuntime{}
	gateway := New(&gatewayTestStore{instance: instance}, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)})

	var events int
	for _, err := range gateway.SendStreamingMessage(gatewayTestContext(instance.GetA2AAuthority()), &a2atype.SendMessageRequest{}) {
		if err != nil {
			t.Fatal(err)
		}
		events++
	}
	if events != 1 || !runtime.destroyed {
		t.Fatalf("stream events = %d, destroyed %v", events, runtime.destroyed)
	}
}

func TestGatewayRejectsPrivateActorAuthority(t *testing.T) {
	gateway := New(&gatewayTestStore{instance: gatewayTestInstance()}, &gatewayTestAuthorizer{}, &gatewayTestDialer{})
	if _, err := gateway.SendMessage(gatewayTestContext("ai-instance.team-a.actors.resources.substrate.ate.dev"), &a2atype.SendMessageRequest{}); err == nil {
		t.Fatal("SendMessage() accepted a private Actor authority")
	}
}

func TestGatewayHidesInternalErrors(t *testing.T) {
	instance := gatewayTestInstance()
	for _, test := range []struct {
		name    string
		store   *gatewayTestStore
		dialer  *gatewayTestDialer
		message string
	}{
		{name: "store", store: &gatewayTestStore{err: errors.New("password=secret")}, dialer: &gatewayTestDialer{}, message: "failed to load AgentInstance"},
		{name: "dialer", store: &gatewayTestStore{instance: instance}, dialer: &gatewayTestDialer{err: errors.New("internal.host:1234")}, message: "failed to connect to AgentInstance runtime"},
	} {
		t.Run(test.name, func(t *testing.T) {
			gateway := New(test.store, &gatewayTestAuthorizer{}, test.dialer)
			_, err := gateway.SendMessage(gatewayTestContext(instance.GetA2AAuthority()), &a2atype.SendMessageRequest{})
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("SendMessage() error = %v, want %q", err, test.message)
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "internal.host") {
				t.Fatalf("SendMessage() leaked internal error: %v", err)
			}
		})
	}
}

func TestGatewayReadsAuthorityFromGRPC(t *testing.T) {
	instance := gatewayTestInstance()
	runtime := &gatewayTestRuntime{}
	gateway := New(&gatewayTestStore{instance: instance}, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)})
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(auth.AuthSessionTo(ctx, gatewayTestSession{}), req)
	}))
	a2agrpc.NewHandler(gateway).RegisterWith(server)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	connection, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithAuthority(instance.GetA2AAuthority()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	request, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hello")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a2apb.NewA2AServiceClient(connection).SendMessage(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if !runtime.sent {
		t.Fatal("gRPC request did not reach the AgentInstance runtime")
	}
}
