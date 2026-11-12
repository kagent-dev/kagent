package grpcserver

import (
	"context"
	"net"
	"testing"

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

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
)

const testHarnessImage = "example.test/runtime@sha256:0000000000000000000000000000000000000000000000000000000000000000"

func newTemplateAndHarnessConnection(t *testing.T, objects ...ctrlclient.Object) *grpc.ClientConn {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1alpha3.AddToScheme(scheme); err != nil {
		t.Fatalf("v1alpha3.AddToScheme() error = %v", err)
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	return newConfigurationConnection(t, kubeClient)
}

func newConfigurationConnection(t *testing.T, kubeClient ctrlclient.Client, configure ...func(*Config)) *grpc.ClientConn {
	t.Helper()
	listener := bufconn.Listen(DefaultMaxMessageSize)
	config := Config{
		Listener:               listener,
		Authenticator:          &authimpl.InsecureAuthenticator{},
		SystemService:          testSystemService(),
		AgentService:           kubecrud.NewService(kubeClient, &pkgauth.NoopAuthorizer{}, &v1alpha3.Agent{}, &v1alpha3.AgentList{}, "Agent"),
		AgentTemplateService:   kubecrud.NewService(kubeClient, &pkgauth.NoopAuthorizer{}, &v1alpha3.AgentTemplate{}, &v1alpha3.AgentTemplateList{}, "AgentTemplate"),
		HarnessService:         kubecrud.NewService(kubeClient, &pkgauth.NoopAuthorizer{}, &v1alpha3.Harness{}, &v1alpha3.HarnessList{}, "Harness"),
		SandboxTemplateService: kubecrud.NewService(kubeClient, &pkgauth.NoopAuthorizer{}, &v1alpha3.SandboxTemplate{}, &v1alpha3.SandboxTemplateList{}, v1alpha3.SandboxTemplateKind),
		EgressPolicyService:    kubecrud.NewService(kubeClient, &pkgauth.NoopAuthorizer{}, &v1alpha3.EgressPolicy{}, &v1alpha3.EgressPolicyList{}, v1alpha3.EgressPolicyKind),
	}
	for _, apply := range configure {
		apply(&config)
	}
	server, err := New(config)
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
			ModelConfig: &v1alpha3.ModelConfigReference{Name: modelConfig},
			Description: "a template",
		},
	}
}

func testHarness(namespace, name, workerPool string) *v1alpha3.Harness {
	return &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: v1alpha3.HarnessSpec{
			Codex:    &v1alpha3.CodexHarness{},
			Workload: v1alpha3.HarnessWorkload{Image: new(testHarnessImage)},
			Substrate: v1alpha3.RuntimeSubstratePolicy{
				WorkerPoolRef:  corev1.LocalObjectReference{Name: workerPool},
				SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "s3://snapshots"},
			},
		},
	}
}

func structured(t *testing.T, value any, kind string) *apiv1.StructuredObject {
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
	existing := testAgentTemplate("team", "z-existing", "gpt")

	client := apiv1.NewAgentTemplateServiceClient(newTemplateAndHarnessConnection(t, existing))
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "template-user"))
	ref := &apiv1.ResourceReference{Namespace: "team", Name: "a-created"}

	createdTemplate := testAgentTemplate("team", "a-created", "gpt")
	createdTemplate.Labels = map[string]string{"runtime": "kagent"}
	created, err := client.CreateAgentTemplate(ctx, &apiv1.CreateAgentTemplateRequest{
		Ref:      ref,
		Resource: structured(t, createdTemplate, agentTemplateKind),
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

	_, err = client.CreateAgentTemplate(ctx, &apiv1.CreateAgentTemplateRequest{
		Ref:      ref,
		Resource: structured(t, testAgentTemplate("team", "a-created", "gpt"), agentTemplateKind),
	})
	assertCode(t, err, codes.AlreadyExists)

	got, err := client.GetAgentTemplate(ctx, &apiv1.GetAgentTemplateRequest{Ref: ref})
	if err != nil {
		t.Fatalf("GetAgentTemplate() error = %v", err)
	}
	if got.GetAgentTemplate().GetDescription() != "a template" {
		t.Fatalf("GetAgentTemplate() description = %q", got.GetAgentTemplate().GetDescription())
	}

	updatedTemplate := testAgentTemplate("team", "a-created", "claude")
	updatedTemplate.Labels = map[string]string{"runtime": "codex"}
	updated, err := client.UpdateAgentTemplate(ctx, &apiv1.UpdateAgentTemplateRequest{
		Ref:      ref,
		Resource: structured(t, updatedTemplate, agentTemplateKind),
	})
	if err != nil {
		t.Fatalf("UpdateAgentTemplate() error = %v", err)
	}
	if updated.GetAgentTemplate().GetModelConfigRef().GetName() != "claude" {
		t.Fatalf("UpdateAgentTemplate() modelConfigRef = %+v", updated.GetAgentTemplate().GetModelConfigRef())
	}
	updatedResource := &v1alpha3.AgentTemplate{}
	if err := structuredobject.ToGo(updated.GetAgentTemplate().GetResource(), agentTemplateKind, updatedResource, DefaultMaxMessageSize); err != nil {
		t.Fatalf("decode UpdateAgentTemplate() resource: %v", err)
	}
	if got := updatedResource.Labels["runtime"]; got != "codex" {
		t.Fatalf("UpdateAgentTemplate() label runtime = %q, want codex", got)
	}

	listed, err := client.ListAgentTemplates(ctx, &apiv1.ListAgentTemplatesRequest{Namespace: "team"})
	if err != nil {
		t.Fatalf("ListAgentTemplates() error = %v", err)
	}
	if len(listed.GetAgentTemplates()) != 2 {
		t.Fatalf("ListAgentTemplates() count = %d, want 2", len(listed.GetAgentTemplates()))
	}
	if name := listed.GetAgentTemplates()[0].GetRef().GetName(); name != "a-created" {
		t.Fatalf("ListAgentTemplates()[0] = %q, want a-created first", name)
	}

	// A resource whose metadata names a different object must be rejected rather
	// than silently written to the ref the caller was authorized against.
	_, err = client.CreateAgentTemplate(ctx, &apiv1.CreateAgentTemplateRequest{
		Ref:      &apiv1.ResourceReference{Namespace: "team", Name: "mismatch"},
		Resource: structured(t, testAgentTemplate("other", "elsewhere", "gpt"), agentTemplateKind),
	})
	assertCode(t, err, codes.InvalidArgument)

	_, err = client.CreateAgentTemplate(ctx, &apiv1.CreateAgentTemplateRequest{
		Ref:      &apiv1.ResourceReference{Namespace: "team", Name: "wrong-kind"},
		Resource: structured(t, testHarness("team", "wrong-kind", "pool-a"), harnessKind),
	})
	assertCode(t, err, codes.InvalidArgument)

	listed, err = client.ListAgentTemplates(ctx, &apiv1.ListAgentTemplatesRequest{})
	if err != nil {
		t.Fatalf("ListAgentTemplates() without namespace error = %v", err)
	}
	if len(listed.GetAgentTemplates()) != 2 {
		t.Fatalf("ListAgentTemplates() without namespace count = %d, want 2", len(listed.GetAgentTemplates()))
	}
	_, err = client.GetAgentTemplate(ctx, &apiv1.GetAgentTemplateRequest{})
	assertCode(t, err, codes.InvalidArgument)
	_, err = client.GetAgentTemplate(ctx, &apiv1.GetAgentTemplateRequest{
		Ref: &apiv1.ResourceReference{Namespace: "team", Name: "absent"},
	})
	assertCode(t, err, codes.NotFound)

	if _, err := client.DeleteAgentTemplate(ctx, &apiv1.DeleteAgentTemplateRequest{Ref: ref}); err != nil {
		t.Fatalf("DeleteAgentTemplate() error = %v", err)
	}
	_, err = client.DeleteAgentTemplate(ctx, &apiv1.DeleteAgentTemplateRequest{Ref: ref})
	assertCode(t, err, codes.NotFound)
}

func TestAgentTemplateModelConfigRef(t *testing.T) {
	t.Run("Given a template whose model has another kind, when a caller gets it, assert that modelConfigRef is unset", func(t *testing.T) {
		template := testAgentTemplate("team", "custom-model", "gpt")
		template.Spec.ModelConfig.Group, template.Spec.ModelConfig.Kind = new("example.com"), new("Model")
		client := apiv1.NewAgentTemplateServiceClient(newTemplateAndHarnessConnection(t, template))
		ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "template-user"))
		got, err := client.GetAgentTemplate(ctx, &apiv1.GetAgentTemplateRequest{Ref: &apiv1.ResourceReference{Namespace: "team", Name: "custom-model"}})
		if err != nil {
			t.Fatalf("GetAgentTemplate() error = %v", err)
		}
		if ref := got.GetAgentTemplate().GetModelConfigRef(); ref != nil {
			t.Fatalf("GetAgentTemplate() modelConfigRef = %+v, want nil", ref)
		}
	})
}

func TestHarnessServiceGeneratedClient(t *testing.T) {
	existing := testHarness("team", "z-existing", "pool-z")
	client := apiv1.NewHarnessServiceClient(newTemplateAndHarnessConnection(t, existing))
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "harness-user"))
	ref := &apiv1.ResourceReference{Namespace: "team", Name: "a-created"}

	created, err := client.CreateHarness(ctx, &apiv1.CreateHarnessRequest{
		Ref:      ref,
		Resource: structured(t, testHarness("team", "a-created", "pool-a"), harnessKind),
	})
	if err != nil {
		t.Fatalf("CreateHarness() error = %v", err)
	}
	if got := created.GetHarness(); got.GetRuntime() != harnessRuntimeCodex || got.GetWorkloadImage() != testHarnessImage {
		t.Fatalf("CreateHarness() = %+v, want codex runtime and pinned image", got)
	}
	if _, ok := created.GetHarness().GetResource().GetValue().GetFields()["status"]; ok {
		t.Fatal("CreateHarness() resource must not contain status")
	}

	_, err = client.CreateHarness(ctx, &apiv1.CreateHarnessRequest{
		Ref:      ref,
		Resource: structured(t, testHarness("team", "a-created", "pool-a"), harnessKind),
	})
	assertCode(t, err, codes.AlreadyExists)

	listed, err := client.ListHarnesses(ctx, &apiv1.ListHarnessesRequest{Namespace: "team"})
	if err != nil {
		t.Fatalf("ListHarnesses() error = %v", err)
	}
	if len(listed.GetHarnesses()) != 2 {
		t.Fatalf("ListHarnesses() count = %d, want 2", len(listed.GetHarnesses()))
	}

	_, err = client.CreateHarness(ctx, &apiv1.CreateHarnessRequest{
		Ref:      &apiv1.ResourceReference{Namespace: "team", Name: "mismatch"},
		Resource: structured(t, testHarness("other", "elsewhere", "pool-a"), harnessKind),
	})
	assertCode(t, err, codes.InvalidArgument)

	// A payload for another kind must not be accepted here.
	_, err = client.CreateHarness(ctx, &apiv1.CreateHarnessRequest{
		Ref:      &apiv1.ResourceReference{Namespace: "team", Name: "wrong-kind"},
		Resource: structured(t, map[string]any{}, "OtherHarness"),
	})
	assertCode(t, err, codes.InvalidArgument)

	listed, err = client.ListHarnesses(ctx, &apiv1.ListHarnessesRequest{})
	if err != nil {
		t.Fatalf("ListHarnesses() without namespace error = %v", err)
	}
	if len(listed.GetHarnesses()) != 2 {
		t.Fatalf("ListHarnesses() without namespace count = %d, want 2", len(listed.GetHarnesses()))
	}
	if _, err := client.DeleteHarness(ctx, &apiv1.DeleteHarnessRequest{Ref: ref}); err != nil {
		t.Fatalf("DeleteHarness() error = %v", err)
	}
	_, err = client.DeleteHarness(ctx, &apiv1.DeleteHarnessRequest{Ref: ref})
	assertCode(t, err, codes.NotFound)
}

func TestHarnessUpdate(t *testing.T) {
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "harness-user"))
	ref := &apiv1.ResourceReference{Namespace: "team", Name: "codex"}

	t.Run("Given a pinned Harness, when a caller updates it to a new pool without an image, assert that a later list reports the new spec and an empty image", func(t *testing.T) {
		existing := testHarness("team", "codex", "pool-a")
		existing.Labels = map[string]string{"tier": "old"}
		client := apiv1.NewHarnessServiceClient(newTemplateAndHarnessConnection(t, existing))
		incoming := testHarness("team", "codex", "pool-b")
		incoming.Spec.Workload = v1alpha3.HarnessWorkload{}
		incoming.Labels = map[string]string{"tier": "new"}

		updated, err := client.UpdateHarness(ctx, &apiv1.UpdateHarnessRequest{Ref: ref, Resource: structured(t, incoming, harnessKind)})
		if err != nil {
			t.Fatalf("UpdateHarness() error = %v", err)
		}
		if got := updated.GetHarness(); got.GetRuntime() != harnessRuntimeCodex || got.GetWorkloadImage() != "" {
			t.Fatalf("UpdateHarness() = %+v, want codex runtime with no pinned image", got)
		}

		listed, err := client.ListHarnesses(ctx, &apiv1.ListHarnessesRequest{Namespace: "team"})
		if err != nil {
			t.Fatalf("ListHarnesses() error = %v", err)
		}
		if len(listed.GetHarnesses()) != 1 {
			t.Fatalf("ListHarnesses() count = %d, want 1", len(listed.GetHarnesses()))
		}
		stored := &v1alpha3.Harness{}
		if err := structuredobject.ToGo(listed.GetHarnesses()[0].GetResource(), harnessKind, stored, DefaultMaxMessageSize); err != nil {
			t.Fatalf("decode listed Harness: %v", err)
		}
		if stored.Spec.Substrate.WorkerPoolRef.Name != "pool-b" || stored.Spec.Workload.Image != nil || stored.Labels["tier"] != "new" {
			t.Fatalf("listed Harness = %+v, want pool-b, no image and the new label", stored)
		}
	})

	t.Run("Given no Harness, when a caller updates one, assert that the update is not found", func(t *testing.T) {
		client := apiv1.NewHarnessServiceClient(newTemplateAndHarnessConnection(t))
		_, err := client.UpdateHarness(ctx, &apiv1.UpdateHarnessRequest{Ref: ref, Resource: structured(t, testHarness("team", "codex", "pool-b"), harnessKind)})
		assertCode(t, err, codes.NotFound)
	})

	t.Run("Given a Harness, when a caller updates it with a payload naming another object, assert that the update is rejected and the stored spec is unchanged", func(t *testing.T) {
		client := apiv1.NewHarnessServiceClient(newTemplateAndHarnessConnection(t, testHarness("team", "codex", "pool-a")))
		_, err := client.UpdateHarness(ctx, &apiv1.UpdateHarnessRequest{Ref: ref, Resource: structured(t, testHarness("team", "other", "pool-b"), harnessKind)})
		assertCode(t, err, codes.InvalidArgument)

		listed, err := client.ListHarnesses(ctx, &apiv1.ListHarnessesRequest{Namespace: "team"})
		if err != nil {
			t.Fatalf("ListHarnesses() error = %v", err)
		}
		stored := &v1alpha3.Harness{}
		if err := structuredobject.ToGo(listed.GetHarnesses()[0].GetResource(), harnessKind, stored, DefaultMaxMessageSize); err != nil {
			t.Fatalf("decode listed Harness: %v", err)
		}
		if stored.Spec.Substrate.WorkerPoolRef.Name != "pool-a" {
			t.Fatalf("stored pool = %q, want pool-a", stored.Spec.Substrate.WorkerPoolRef.Name)
		}
	})

	t.Run("Given a Harness, when a caller updates it without a resource, assert that validation rejects the request", func(t *testing.T) {
		client := apiv1.NewHarnessServiceClient(newTemplateAndHarnessConnection(t, testHarness("team", "codex", "pool-a")))
		_, err := client.UpdateHarness(ctx, &apiv1.UpdateHarnessRequest{Ref: ref})
		assertCode(t, err, codes.InvalidArgument)
	})
}

func TestHarnessRuntime(t *testing.T) {
	for want, spec := range map[string]v1alpha3.HarnessSpec{
		harnessRuntimeKagent: {Kagent: &v1alpha3.KagentHarness{}},
		harnessRuntimeCodex:  {Codex: &v1alpha3.CodexHarness{}},
		harnessRuntimeClaude: {Claude: &v1alpha3.ClaudeHarness{}},
		harnessRuntimeBYO:    {BYO: &v1alpha3.BYOHarness{}},
		"":                   {},
	} {
		if got := harnessRuntime(&v1alpha3.Harness{Spec: spec}); got != want {
			t.Errorf("harnessRuntime() = %q, want %q", got, want)
		}
	}
}
