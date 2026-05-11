package openshell

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	datamodelv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/datamodelv1"
	inferencev1 "github.com/kagent-dev/kagent/go/api/openshell/gen/inferencev1"
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
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

	execCalls      [][]string
	execStdins     [][]byte
	execSandboxIDs []string
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
		Metadata: &datamodelv1.ObjectMeta{
			Id:   "id-" + req.GetName(),
			Name: req.GetName(),
		},
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

func (f *fakeGateway) CreateProvider(_ context.Context, req *openshellv1.CreateProviderRequest) (*openshellv1.ProviderResponse, error) {
	meta := req.GetProvider().GetMetadata()
	name := ""
	if meta != nil {
		name = meta.GetName()
	}
	return &openshellv1.ProviderResponse{
		Provider: &datamodelv1.Provider{
			Metadata: &datamodelv1.ObjectMeta{Id: "fake-provider-id", Name: name},
			Type:     req.GetProvider().GetType(),
		},
	}, nil
}

func (f *fakeGateway) GetProvider(_ context.Context, _ *openshellv1.GetProviderRequest) (*openshellv1.ProviderResponse, error) {
	return nil, status.Error(codes.NotFound, "provider not found")
}

func (f *fakeGateway) UpdateProvider(_ context.Context, req *openshellv1.UpdateProviderRequest) (*openshellv1.ProviderResponse, error) {
	return &openshellv1.ProviderResponse{Provider: req.GetProvider()}, nil
}

func (f *fakeGateway) ExecSandbox(req *openshellv1.ExecSandboxRequest, stream grpc.ServerStreamingServer[openshellv1.ExecSandboxEvent]) error {
	f.mu.Lock()
	f.execCalls = append(f.execCalls, append([]string(nil), req.Command...))
	f.execStdins = append(f.execStdins, append([]byte(nil), req.GetStdin()...))
	f.execSandboxIDs = append(f.execSandboxIDs, req.GetSandboxId())
	f.mu.Unlock()
	return stream.Send(&openshellv1.ExecSandboxEvent{
		Payload: &openshellv1.ExecSandboxEvent_Exit{
			Exit: &openshellv1.ExecSandboxExit{ExitCode: 0},
		},
	})
}

type fakeInference struct {
	inferencev1.UnimplementedInferenceServer

	mu           sync.Mutex
	lastProvider string
	lastModel    string
}

func (f *fakeInference) SetClusterInference(_ context.Context, req *inferencev1.SetClusterInferenceRequest) (*inferencev1.SetClusterInferenceResponse, error) {
	f.mu.Lock()
	f.lastProvider = req.GetProviderName()
	f.lastModel = req.GetModelId()
	f.mu.Unlock()
	return &inferencev1.SetClusterInferenceResponse{
		ProviderName: req.GetProviderName(),
		ModelId:      req.GetModelId(),
	}, nil
}

func (f *fakeGateway) setPhase(name string, p openshellv1.SandboxPhase) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if sb, ok := f.sandboxes[name]; ok {
		sb.Phase = p
	}
}

func startFake(t *testing.T) (openshellv1.OpenShellClient, inferencev1.InferenceClient, *fakeGateway, *fakeInference, func()) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	fg := &fakeGateway{}
	fi := &fakeInference{}
	openshellv1.RegisterOpenShellServer(srv, fg)
	inferencev1.RegisterInferenceServer(srv, fi)
	go func() { _ = srv.Serve(lis) }()

	dialer := func(context.Context, string) (net.Conn, error) { return lis.Dial() }
	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	cleanup := func() {
		_ = conn.Close()
		srv.Stop()
		_ = lis.Close()
	}
	return openshellv1.NewOpenShellClient(conn), inferencev1.NewInferenceClient(conn), fg, fi, cleanup
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, v1alpha2.AddToScheme(s))
	return s
}

func sampleClawSandbox() *v1alpha2.AgentHarness {
	return &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "a1", Namespace: "ns1"},
		Spec: v1alpha2.AgentHarnessSpec{
			Backend: v1alpha2.AgentHarnessBackendOpenClaw,
			Image:   "img:v1",
			Env:     []corev1.EnvVar{{Name: "FOO", Value: "bar"}},
		},
	}
}

func TestEnsureSandbox_CreatesThenIdempotent(t *testing.T) {
	c, ic, fg, _, cleanup := startFake(t)
	defer cleanup()
	b := NewOpenClawBackend(nil, &OpenShellClients{OpenShell: c, Inference: ic}, Config{GatewayURL: "grpc://gw"}, nil)

	r, err := b.EnsureAgentHarness(context.Background(), sampleClawSandbox())
	require.NoError(t, err)
	require.Equal(t, "ns1-a1", r.Handle.ID)
	require.Equal(t, "grpc://gw#ns1-a1", r.Endpoint)
	require.Equal(t, 1, fg.createCalls)

	r2, err := b.EnsureAgentHarness(context.Background(), sampleClawSandbox())
	require.NoError(t, err)
	require.Equal(t, r.Handle.ID, r2.Handle.ID)
	require.Equal(t, 1, fg.createCalls, "second EnsureSandbox must not re-create")
}

func TestEnsureSandbox_CreateFails(t *testing.T) {
	c, ic, fg, _, cleanup := startFake(t)
	defer cleanup()
	fg.createErr = status.Error(codes.ResourceExhausted, "quota")

	b := NewOpenClawBackend(nil, &OpenShellClients{OpenShell: c, Inference: ic}, Config{}, nil)
	_, err := b.EnsureAgentHarness(context.Background(), sampleClawSandbox())
	require.Error(t, err)
	require.Contains(t, err.Error(), "CreateSandbox")
}

func TestGetStatus_PhaseMapping(t *testing.T) {
	c, ic, fg, _, cleanup := startFake(t)
	defer cleanup()
	b := NewOpenClawBackend(nil, &OpenShellClients{OpenShell: c, Inference: ic}, Config{}, nil)

	r, err := b.EnsureAgentHarness(context.Background(), sampleClawSandbox())
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
	c, ic, _, _, cleanup := startFake(t)
	defer cleanup()
	b := NewOpenClawBackend(nil, &OpenShellClients{OpenShell: c, Inference: ic}, Config{}, nil)

	st, reason, _ := b.GetStatus(context.Background(), sandboxbackend.Handle{})
	require.Equal(t, metav1.ConditionUnknown, st)
	require.Equal(t, "SandboxHandleMissing", reason)
}

func TestGetStatus_NotFound(t *testing.T) {
	c, ic, _, _, cleanup := startFake(t)
	defer cleanup()
	b := NewOpenClawBackend(nil, &OpenShellClients{OpenShell: c, Inference: ic}, Config{}, nil)

	st, reason, _ := b.GetStatus(context.Background(), sandboxbackend.Handle{ID: "missing"})
	require.Equal(t, metav1.ConditionUnknown, st)
	require.Equal(t, "SandboxNotFound", reason)
}

func TestDeleteSandbox(t *testing.T) {
	c, ic, fg, _, cleanup := startFake(t)
	defer cleanup()
	b := NewOpenClawBackend(nil, &OpenShellClients{OpenShell: c, Inference: ic}, Config{}, nil)

	r, err := b.EnsureAgentHarness(context.Background(), sampleClawSandbox())
	require.NoError(t, err)

	require.NoError(t, b.DeleteAgentHarness(context.Background(), r.Handle))
	require.Equal(t, 1, fg.deleteCalls)

	require.NoError(t, b.DeleteAgentHarness(context.Background(), r.Handle))
	require.Equal(t, 2, fg.deleteCalls)

	before := fg.deleteCalls
	require.NoError(t, b.DeleteAgentHarness(context.Background(), sandboxbackend.Handle{}))
	require.Equal(t, before, fg.deleteCalls)
}

func TestCallTimeout(t *testing.T) {
	c, ic, fg, _, cleanup := startFake(t)
	defer cleanup()
	fg.getErr = status.Error(codes.Unavailable, "backend down")

	b := NewOpenClawBackend(nil, &OpenShellClients{OpenShell: c, Inference: ic}, Config{CallTimeout: 50 * time.Millisecond}, nil)
	_, err := b.EnsureAgentHarness(context.Background(), sampleClawSandbox())
	require.Error(t, err)
}

func TestEnsureSandbox_WithModelConfigRef_RegistersProvider(t *testing.T) {
	s := testScheme(t)
	const ns = "ns1"
	mc := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "m1", Namespace: ns},
		Spec: v1alpha2.ModelConfigSpec{
			Model:           "gpt-4o",
			Provider:        v1alpha2.ModelProviderOpenAI,
			APIKeySecret:    "keysec",
			APIKeySecretKey: "OPENAI_API_KEY",
		},
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "keysec", Namespace: ns},
		Data:       map[string][]byte{"OPENAI_API_KEY": []byte("sk-secret")},
	}
	kube := fake.NewClientBuilder().WithScheme(s).WithObjects(mc, sec).Build()

	c, ic, _, fi, cleanup := startFake(t)
	defer cleanup()
	b := NewOpenClawBackend(kube, &OpenShellClients{OpenShell: c, Inference: ic}, Config{GatewayURL: "grpc://gw"}, nil)

	sbx := sampleClawSandbox()
	sbx.Spec.ModelConfigRef = "m1"

	_, err := b.EnsureAgentHarness(context.Background(), sbx)
	require.NoError(t, err)

	fi.mu.Lock()
	defer fi.mu.Unlock()
	require.Equal(t, "openai", fi.lastProvider)
	require.Equal(t, "gpt-4o", fi.lastModel)
}

func TestExecSandboxID_UsesGatewayMetadataId(t *testing.T) {
	c, ic, _, _, cleanup := startFake(t)
	defer cleanup()
	b := NewOpenClawBackend(nil, &OpenShellClients{OpenShell: c, Inference: ic}, Config{}, nil)

	r, err := b.EnsureAgentHarness(context.Background(), sampleClawSandbox())
	require.NoError(t, err)

	ctx := withAuth(context.Background(), "")
	id, err := b.ExecSandboxID(ctx, r.Handle.ID)
	require.NoError(t, err)
	require.Equal(t, "id-ns1-a1", id)
}

func TestOnSandboxReady_ModelConfigRef(t *testing.T) {
	s := testScheme(t)
	const ns = "ns1"
	mc := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "m1", Namespace: ns},
		Spec: v1alpha2.ModelConfigSpec{
			Model:           "gpt-4o",
			Provider:        v1alpha2.ModelProviderOpenAI,
			APIKeySecret:    "keysec",
			APIKeySecretKey: "OPENAI_API_KEY",
		},
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "keysec", Namespace: ns},
		Data:       map[string][]byte{"OPENAI_API_KEY": []byte("sk-secret")},
	}
	kube := fake.NewClientBuilder().WithScheme(s).WithObjects(mc, sec).Build()

	c, ic, fg, _, cleanup := startFake(t)
	defer cleanup()
	b := NewOpenClawBackend(kube, &OpenShellClients{OpenShell: c, Inference: ic}, Config{Token: "tok"}, nil)

	sbx := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "a1", Namespace: ns},
		Spec: v1alpha2.AgentHarnessSpec{
			Backend:        v1alpha2.AgentHarnessBackendOpenClaw,
			ModelConfigRef: "m1",
		},
	}

	r, err := b.EnsureAgentHarness(context.Background(), sbx)
	require.NoError(t, err)

	ctx := withAuth(context.Background(), "tok")
	require.NoError(t, b.OnAgentHarnessReady(ctx, sbx, r.Handle))
	require.Len(t, fg.execCalls, 2)
	fg.mu.Lock()
	require.Equal(t, "id-ns1-a1", fg.execSandboxIDs[0])
	require.Equal(t, "id-ns1-a1", fg.execSandboxIDs[1])
	fg.mu.Unlock()

	installJoined := strings.Join(fg.execCalls[0], " ")
	require.Contains(t, installJoined, `mkdir -p "$HOME/.openclaw"`)
	require.Contains(t, installJoined, `openclaw.json`)
	require.NotEmpty(t, fg.execStdins[0])
	require.Contains(t, string(fg.execStdins[0]), `"openai/gpt-4o"`)
	require.Contains(t, string(fg.execStdins[0]), `"OPENAI_API_KEY"`)
	require.Contains(t, string(fg.execStdins[0]), `openshell:resolve:env:OPENAI_API_KEY`)
	require.Contains(t, string(fg.execStdins[0]), `"secrets"`)
	require.Contains(t, string(fg.execStdins[0]), `"kagent"`)
	require.Contains(t, string(fg.execStdins[0]), `"allowlist"`)

	gatewayJoined := strings.Join(fg.execCalls[1], " ")
	require.Contains(t, gatewayJoined, "openclaw gateway run")
	require.Contains(t, gatewayJoined, "--port 18800")
	require.NotContains(t, gatewayJoined, "--allow-unconfigured")
	require.NotContains(t, gatewayJoined, "--auth none")
	require.Empty(t, fg.execStdins[1])
}

func TestEnsureSandbox_Claw_PinsNemoclawBaseImage(t *testing.T) {
	c, ic, fg, _, cleanup := startFake(t)
	defer cleanup()
	b := NewOpenClawBackend(nil, &OpenShellClients{OpenShell: c, Inference: ic}, Config{GatewayURL: "grpc://gw"}, nil)

	sbx := sampleClawSandbox()
	sbx.Spec.Image = ""
	_, err := b.EnsureAgentHarness(context.Background(), sbx)
	require.NoError(t, err)

	fg.mu.Lock()
	defer fg.mu.Unlock()
	sb := fg.sandboxes["ns1-a1"]
	require.NotNil(t, sb)
	require.Equal(t, NemoclawSandboxBaseImage, sb.GetSpec().GetTemplate().GetImage())
}
