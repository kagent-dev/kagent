package grpcserver

import (
	"context"
	"net"
	"testing"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testHarnessImage = "example.test/runtime@sha256:0000000000000000000000000000000000000000000000000000000000000000"

func newTemplateAndHarnessConnection(t *testing.T, objects ...ctrlclient.Object) *grpc.ClientConn {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1alpha3.AddToScheme(scheme); err != nil {
		t.Fatalf("v1alpha3.AddToScheme() error = %v", err)
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

	listener := bufconn.Listen(DefaultMaxMessageSize)
	server, err := New(Config{
		Listener:             listener,
		Registerer:           prometheus.NewRegistry(),
		Authenticator:        &authimpl.UnsecureAuthenticator{},
		AgentTemplateService: kubecrud.NewService(kubeClient, &authimpl.NoopAuthorizer{}, &v1alpha3.AgentTemplate{}, &v1alpha3.AgentTemplateList{}, "AgentTemplate"),
		HarnessService:       kubecrud.NewService(kubeClient, &authimpl.NoopAuthorizer{}, &v1alpha3.Harness{}, &v1alpha3.HarnessList{}, "Harness"),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	serverContext, cancelServer := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Start(serverContext) }()
	t.Cleanup(func() {
		cancelServer()
		if err := <-done; err != nil {
			t.Errorf("gRPC server shutdown error = %v", err)
		}
	})

	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return connection
}

func testAgentTemplate(namespace, name, modelConfig string) *v1alpha3.AgentTemplate {
	return &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig: &corev1.LocalObjectReference{Name: modelConfig},
			Description: "a template",
		},
	}
}

func testHarness(namespace, name, workerPool string) *v1alpha3.Harness {
	return &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: v1alpha3.HarnessSpec{
			Codex:    &v1alpha3.CodexHarness{},
			Workload: v1alpha3.HarnessWorkload{Image: testHarnessImage},
			Substrate: v1alpha3.HarnessSubstratePolicy{
				WorkerPoolRef:  corev1.LocalObjectReference{Name: workerPool},
				SnapshotPolicy: v1alpha3.HarnessSnapshotPolicy{Location: "s3://snapshots"},
			},
		},
	}
}

func structured(t *testing.T, value any, kind string) *apiv1alpha1.StructuredObject {
	t.Helper()
	resource, err := structuredobject.FromGo(value, v1alpha3.GroupVersion.String(), kind, DefaultMaxMessageSize)
	if err != nil {
		t.Fatalf("structuredobject.FromGo() error = %v", err)
	}
	return resource
}

func assertCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if got, ok := status.FromError(err); !ok || got.Code() != want {
		t.Fatalf("error = %v, want code %v", err, want)
	}
}

func TestAgentTemplateServiceGeneratedClient(t *testing.T) {
	// The existing template carries controller-written status so the response
	// can be checked for the admitting-harness denormalisation, which a caller
	// cannot derive from the template alone.
	existing := testAgentTemplate("team", "z-existing", "gpt")
	existing.Status = v1alpha3.AgentTemplateStatus{
		Harnesses: []v1alpha3.AgentTemplateHarnessStatus{{Harness: "shared", DesiredRevision: "rev-1"}},
	}
	client := apiv1alpha1.NewAgentTemplateServiceClient(newTemplateAndHarnessConnection(t, existing))
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "template-user"))
	ref := &apiv1alpha1.ResourceReference{Namespace: "team", Name: "a-created"}

	created, err := client.CreateAgentTemplate(ctx, &apiv1alpha1.CreateAgentTemplateRequest{
		Ref:      ref,
		Resource: structured(t, testAgentTemplate("team", "a-created", "gpt"), agentTemplateKind),
	})
	if err != nil {
		t.Fatalf("CreateAgentTemplate() error = %v", err)
	}
	if got := created.GetAgentTemplate().GetModelConfigRef(); got.GetNamespace() != "team" || got.GetName() != "gpt" {
		t.Fatalf("CreateAgentTemplate() modelConfigRef = %+v, want team/gpt", got)
	}
	if got := created.GetAgentTemplate().GetResource().GetKind(); got != agentTemplateKind {
		t.Fatalf("CreateAgentTemplate() resource kind = %q, want %q", got, agentTemplateKind)
	}

	_, err = client.CreateAgentTemplate(ctx, &apiv1alpha1.CreateAgentTemplateRequest{
		Ref:      ref,
		Resource: structured(t, testAgentTemplate("team", "a-created", "gpt"), agentTemplateKind),
	})
	assertCode(t, err, codes.AlreadyExists)

	got, err := client.GetAgentTemplate(ctx, &apiv1alpha1.GetAgentTemplateRequest{Ref: ref})
	if err != nil {
		t.Fatalf("GetAgentTemplate() error = %v", err)
	}
	if got.GetAgentTemplate().GetDescription() != "a template" {
		t.Fatalf("GetAgentTemplate() description = %q", got.GetAgentTemplate().GetDescription())
	}

	updated, err := client.UpdateAgentTemplate(ctx, &apiv1alpha1.UpdateAgentTemplateRequest{
		Ref:      ref,
		Resource: structured(t, testAgentTemplate("team", "a-created", "claude"), agentTemplateKind),
	})
	if err != nil {
		t.Fatalf("UpdateAgentTemplate() error = %v", err)
	}
	if updated.GetAgentTemplate().GetModelConfigRef().GetName() != "claude" {
		t.Fatalf("UpdateAgentTemplate() modelConfigRef = %+v", updated.GetAgentTemplate().GetModelConfigRef())
	}

	listed, err := client.ListAgentTemplates(ctx, &apiv1alpha1.ListAgentTemplatesRequest{Namespace: "team"})
	if err != nil {
		t.Fatalf("ListAgentTemplates() error = %v", err)
	}
	if len(listed.GetAgentTemplates()) != 2 {
		t.Fatalf("ListAgentTemplates() count = %d, want 2", len(listed.GetAgentTemplates()))
	}
	if !listed.GetCanCreate() || !listed.GetAgentTemplates()[0].GetCanUpdate() || !listed.GetAgentTemplates()[0].GetCanDelete() {
		t.Fatal("ListAgentTemplates() did not report no-op authorizer capabilities")
	}
	if name := listed.GetAgentTemplates()[0].GetRef().GetName(); name != "a-created" {
		t.Fatalf("ListAgentTemplates()[0] = %q, want a-created first", name)
	}
	if harnesses := listed.GetAgentTemplates()[1].GetAdmittingHarnesses(); len(harnesses) != 1 || harnesses[0] != "shared" {
		t.Fatalf("ListAgentTemplates()[1].admittingHarnesses = %v, want [shared]", harnesses)
	}

	// A resource whose metadata names a different object must be rejected rather
	// than silently written to the ref the caller was authorized against.
	_, err = client.CreateAgentTemplate(ctx, &apiv1alpha1.CreateAgentTemplateRequest{
		Ref:      &apiv1alpha1.ResourceReference{Namespace: "team", Name: "mismatch"},
		Resource: structured(t, testAgentTemplate("other", "elsewhere", "gpt"), agentTemplateKind),
	})
	assertCode(t, err, codes.InvalidArgument)

	_, err = client.CreateAgentTemplate(ctx, &apiv1alpha1.CreateAgentTemplateRequest{
		Ref:      &apiv1alpha1.ResourceReference{Namespace: "team", Name: "wrong-kind"},
		Resource: structured(t, testHarness("team", "wrong-kind", "pool-a"), harnessKind),
	})
	assertCode(t, err, codes.InvalidArgument)

	_, err = client.ListAgentTemplates(ctx, &apiv1alpha1.ListAgentTemplatesRequest{})
	assertCode(t, err, codes.InvalidArgument)
	_, err = client.GetAgentTemplate(ctx, &apiv1alpha1.GetAgentTemplateRequest{})
	assertCode(t, err, codes.InvalidArgument)
	_, err = client.GetAgentTemplate(ctx, &apiv1alpha1.GetAgentTemplateRequest{
		Ref: &apiv1alpha1.ResourceReference{Namespace: "team", Name: "absent"},
	})
	assertCode(t, err, codes.NotFound)

	if _, err := client.DeleteAgentTemplate(ctx, &apiv1alpha1.DeleteAgentTemplateRequest{Ref: ref}); err != nil {
		t.Fatalf("DeleteAgentTemplate() error = %v", err)
	}
	_, err = client.DeleteAgentTemplate(ctx, &apiv1alpha1.DeleteAgentTemplateRequest{Ref: ref})
	assertCode(t, err, codes.NotFound)
}

func TestHarnessServiceGeneratedClient(t *testing.T) {
	existing := testHarness("team", "z-existing", "pool-z")
	existing.Status = v1alpha3.HarnessStatus{
		Conditions: []metav1.Condition{{
			Type:               v1alpha3.HarnessConditionTypeReady,
			Status:             metav1.ConditionTrue,
			Reason:             "Ready",
			LastTransitionTime: metav1.Now(),
		}},
	}
	client := apiv1alpha1.NewHarnessServiceClient(newTemplateAndHarnessConnection(t, existing))
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "harness-user"))
	ref := &apiv1alpha1.ResourceReference{Namespace: "team", Name: "a-created"}

	created, err := client.CreateHarness(ctx, &apiv1alpha1.CreateHarnessRequest{
		Ref:      ref,
		Resource: structured(t, testHarness("team", "a-created", "pool-a"), harnessKind),
	})
	if err != nil {
		t.Fatalf("CreateHarness() error = %v", err)
	}
	if got := created.GetHarness(); got.GetRuntime() != harnessRuntimeCodex || got.GetWorkloadImage() != testHarnessImage {
		t.Fatalf("CreateHarness() = %+v, want codex runtime and pinned image", got)
	}
	if created.GetHarness().GetReady() {
		t.Fatal("CreateHarness() ready = true, want false before the controller observes it")
	}

	_, err = client.CreateHarness(ctx, &apiv1alpha1.CreateHarnessRequest{
		Ref:      ref,
		Resource: structured(t, testHarness("team", "a-created", "pool-a"), harnessKind),
	})
	assertCode(t, err, codes.AlreadyExists)

	listed, err := client.ListHarnesses(ctx, &apiv1alpha1.ListHarnessesRequest{Namespace: "team"})
	if err != nil {
		t.Fatalf("ListHarnesses() error = %v", err)
	}
	if len(listed.GetHarnesses()) != 2 {
		t.Fatalf("ListHarnesses() count = %d, want 2", len(listed.GetHarnesses()))
	}
	if !listed.GetCanCreate() || !listed.GetHarnesses()[0].GetCanDelete() {
		t.Fatal("ListHarnesses() did not report no-op authorizer capabilities")
	}
	if !listed.GetHarnesses()[1].GetReady() {
		t.Fatal("ListHarnesses()[1].ready = false, want the Ready condition reflected")
	}

	_, err = client.CreateHarness(ctx, &apiv1alpha1.CreateHarnessRequest{
		Ref:      &apiv1alpha1.ResourceReference{Namespace: "team", Name: "mismatch"},
		Resource: structured(t, testHarness("other", "elsewhere", "pool-a"), harnessKind),
	})
	assertCode(t, err, codes.InvalidArgument)

	// An AgentHarness payload must not be accepted here: the two kinds are
	// distinct and the kind check is what keeps them from being confused.
	_, err = client.CreateHarness(ctx, &apiv1alpha1.CreateHarnessRequest{
		Ref:      &apiv1alpha1.ResourceReference{Namespace: "team", Name: "wrong-kind"},
		Resource: structured(t, &v1alpha3.AgentHarness{}, "AgentHarness"),
	})
	assertCode(t, err, codes.InvalidArgument)

	_, err = client.ListHarnesses(ctx, &apiv1alpha1.ListHarnessesRequest{})
	assertCode(t, err, codes.InvalidArgument)
	if _, err := client.DeleteHarness(ctx, &apiv1alpha1.DeleteHarnessRequest{Ref: ref}); err != nil {
		t.Fatalf("DeleteHarness() error = %v", err)
	}
	_, err = client.DeleteHarness(ctx, &apiv1alpha1.DeleteHarnessRequest{Ref: ref})
	assertCode(t, err, codes.NotFound)
}
