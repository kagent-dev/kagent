package openshell

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	openshellv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/openshellv1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type fakeGateway struct {
	openshellv1.UnimplementedOpenShellServer

	mu        sync.Mutex
	sandboxes map[string]*openshellv1.Sandbox
	createErr error
	getErr    error
	deleteErr error

	createCalls int
	deleteCalls int
}

func (f *fakeGateway) CreateSandbox(_ context.Context, req *openshellv1.CreateSandboxRequest) (*openshellv1.SandboxResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.sandboxes == nil {
		f.sandboxes = map[string]*openshellv1.Sandbox{}
	}
	sb := &openshellv1.Sandbox{
		Id:    "id-" + req.GetName(),
		Name:  req.GetName(),
		Spec:  req.GetSpec(),
		Phase: openshellv1.SandboxPhase_SANDBOX_PHASE_PROVISIONING,
	}
	f.sandboxes[req.GetName()] = sb
	return &openshellv1.SandboxResponse{Sandbox: sb}, nil
}

func (f *fakeGateway) GetSandbox(_ context.Context, req *openshellv1.GetSandboxRequest) (*openshellv1.SandboxResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	sb, ok := f.sandboxes[req.GetName()]
	if !ok {
		return nil, status.Error(codes.NotFound, "sandbox not found")
	}
	return &openshellv1.SandboxResponse{Sandbox: sb}, nil
}

func (f *fakeGateway) DeleteSandbox(_ context.Context, req *openshellv1.DeleteSandboxRequest) (*openshellv1.DeleteSandboxResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCalls++
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	if _, ok := f.sandboxes[req.GetName()]; !ok {
		return nil, status.Error(codes.NotFound, "sandbox not found")
	}
	delete(f.sandboxes, req.GetName())
	return &openshellv1.DeleteSandboxResponse{Deleted: true}, nil
}

func (f *fakeGateway) setPhase(name string, p openshellv1.SandboxPhase) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if sb, ok := f.sandboxes[name]; ok {
		sb.Phase = p
	}
}

func startFake(t *testing.T) (openshellv1.OpenShellClient, *fakeGateway, func()) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	fg := &fakeGateway{}
	openshellv1.RegisterOpenShellServer(srv, fg)
	go func() { _ = srv.Serve(lis) }()

	dialer := func(context.Context, string) (net.Conn, error) { return lis.Dial() }
	conn, err := grpc.DialContext(context.Background(), "bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	cleanup := func() {
		_ = conn.Close()
		srv.Stop()
		_ = lis.Close()
	}
	return openshellv1.NewOpenShellClient(conn), fg, cleanup
}

func sampleSandbox() *v1alpha2.Sandbox {
	return &v1alpha2.Sandbox{
		ObjectMeta: metav1.ObjectMeta{Name: "a1", Namespace: "ns1"},
		Spec: v1alpha2.SandboxSpec{
			Backend: v1alpha2.SandboxBackendOpenshell,
			Image:   "img:v1",
			Env:     []corev1.EnvVar{{Name: "FOO", Value: "bar"}},
		},
	}
}

func TestEnsureSandbox_CreatesThenIdempotent(t *testing.T) {
	c, fg, cleanup := startFake(t)
	defer cleanup()
	b := New(c, Config{GatewayURL: "grpc://gw"})

	r, err := b.EnsureSandbox(context.Background(), sampleSandbox())
	require.NoError(t, err)
	require.Equal(t, "ns1-a1", r.Handle.ID)
	require.Equal(t, "grpc://gw#ns1-a1", r.Endpoint)
	require.Equal(t, 1, fg.createCalls)

	r2, err := b.EnsureSandbox(context.Background(), sampleSandbox())
	require.NoError(t, err)
	require.Equal(t, r.Handle.ID, r2.Handle.ID)
	require.Equal(t, 1, fg.createCalls, "second EnsureSandbox must not re-create")
}

func TestEnsureSandbox_CreateFails(t *testing.T) {
	c, fg, cleanup := startFake(t)
	defer cleanup()
	fg.createErr = status.Error(codes.ResourceExhausted, "quota")

	b := New(c, Config{})
	_, err := b.EnsureSandbox(context.Background(), sampleSandbox())
	require.Error(t, err)
	require.Contains(t, err.Error(), "CreateSandbox")
}

func TestGetStatus_PhaseMapping(t *testing.T) {
	c, fg, cleanup := startFake(t)
	defer cleanup()
	b := New(c, Config{})

	r, err := b.EnsureSandbox(context.Background(), sampleSandbox())
	require.NoError(t, err)

	cases := []struct {
		phase      openshellv1.SandboxPhase
		wantStatus metav1.ConditionStatus
		wantReason string
	}{
		{openshellv1.SandboxPhase_SANDBOX_PHASE_READY, metav1.ConditionTrue, "SandboxReady"},
		{openshellv1.SandboxPhase_SANDBOX_PHASE_PROVISIONING, metav1.ConditionFalse, "SandboxProvisioning"},
		{openshellv1.SandboxPhase_SANDBOX_PHASE_ERROR, metav1.ConditionFalse, "SandboxError"},
		{openshellv1.SandboxPhase_SANDBOX_PHASE_DELETING, metav1.ConditionFalse, "SandboxDeleting"},
		{openshellv1.SandboxPhase_SANDBOX_PHASE_UNKNOWN, metav1.ConditionUnknown, "SandboxPhaseUnknown"},
	}
	for _, tc := range cases {
		fg.setPhase(r.Handle.ID, tc.phase)
		st, reason, _ := b.GetStatus(context.Background(), r.Handle)
		require.Equal(t, tc.wantStatus, st, tc.phase.String())
		require.Equal(t, tc.wantReason, reason, tc.phase.String())
	}
}

func TestGetStatus_EmptyHandle(t *testing.T) {
	c, _, cleanup := startFake(t)
	defer cleanup()
	b := New(c, Config{})

	st, reason, _ := b.GetStatus(context.Background(), sandboxbackend.Handle{})
	require.Equal(t, metav1.ConditionUnknown, st)
	require.Equal(t, "SandboxHandleMissing", reason)
}

func TestGetStatus_NotFound(t *testing.T) {
	c, _, cleanup := startFake(t)
	defer cleanup()
	b := New(c, Config{})

	st, reason, _ := b.GetStatus(context.Background(), sandboxbackend.Handle{ID: "missing"})
	require.Equal(t, metav1.ConditionUnknown, st)
	require.Equal(t, "SandboxNotFound", reason)
}

func TestDeleteSandbox(t *testing.T) {
	c, fg, cleanup := startFake(t)
	defer cleanup()
	b := New(c, Config{})

	r, err := b.EnsureSandbox(context.Background(), sampleSandbox())
	require.NoError(t, err)

	require.NoError(t, b.DeleteSandbox(context.Background(), r.Handle))
	require.Equal(t, 1, fg.deleteCalls)

	require.NoError(t, b.DeleteSandbox(context.Background(), r.Handle))
	require.Equal(t, 2, fg.deleteCalls)

	before := fg.deleteCalls
	require.NoError(t, b.DeleteSandbox(context.Background(), sandboxbackend.Handle{}))
	require.Equal(t, before, fg.deleteCalls)
}

func TestCallTimeout(t *testing.T) {
	c, fg, cleanup := startFake(t)
	defer cleanup()
	fg.getErr = status.Error(codes.Unavailable, "backend down")

	b := New(c, Config{CallTimeout: 50 * time.Millisecond})
	_, err := b.EnsureSandbox(context.Background(), sampleSandbox())
	require.Error(t, err)
}
