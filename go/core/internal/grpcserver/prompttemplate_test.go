package grpcserver

import (
	"context"
	"net"
	"testing"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	prompttemplateservice "github.com/kagent-dev/kagent/go/core/internal/service/prompttemplate"
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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPromptTemplateServiceGeneratedClient(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("corev1.AddToScheme() error = %v", err)
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "team",
			Name:      "z-existing",
			Labels:    map[string]string{"kagent.dev/prompt-library": "true"},
		},
		Data:       map[string]string{"z": "last", "a": "first"},
		BinaryData: map[string][]byte{"asset": []byte("binary")},
	}).Build()
	service := prompttemplateservice.NewService(kubeClient, &authimpl.NoopAuthorizer{})

	listener := bufconn.Listen(DefaultMaxMessageSize)
	server, err := New(Config{
		Listener:              listener,
		Registerer:            prometheus.NewRegistry(),
		Authenticator:         &authimpl.UnsecureAuthenticator{},
		SystemService:         testSystemService(),
		PromptTemplateService: service,
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
	client := apiv1alpha1.NewPromptTemplateServiceClient(connection)
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "prompt-user"))
	ref := &apiv1alpha1.ResourceReference{Namespace: "team", Name: "a-created"}

	created, err := client.CreatePromptTemplate(ctx, &apiv1alpha1.CreatePromptTemplateRequest{
		Ref:  ref,
		Data: map[string]string{"intro": "hello", "rules": "be concise"},
	})
	if err != nil {
		t.Fatalf("CreatePromptTemplate() error = %v", err)
	}
	assertPromptTemplate(t, created.GetPromptTemplate(), "team", "a-created", map[string]string{
		"intro": "hello",
		"rules": "be concise",
	})

	_, err = client.CreatePromptTemplate(ctx, &apiv1alpha1.CreatePromptTemplateRequest{
		Ref:  ref,
		Data: map[string]string{"intro": "duplicate"},
	})
	assertPromptTemplateGRPCCode(t, err, codes.AlreadyExists)

	got, err := client.GetPromptTemplate(ctx, &apiv1alpha1.GetPromptTemplateRequest{Ref: ref})
	if err != nil {
		t.Fatalf("GetPromptTemplate() error = %v", err)
	}
	assertPromptTemplate(t, got.GetPromptTemplate(), "team", "a-created", map[string]string{
		"intro": "hello",
		"rules": "be concise",
	})

	updated, err := client.UpdatePromptTemplate(ctx, &apiv1alpha1.UpdatePromptTemplateRequest{
		Ref:  ref,
		Data: map[string]string{"replacement": "only value"},
	})
	if err != nil {
		t.Fatalf("UpdatePromptTemplate() error = %v", err)
	}
	assertPromptTemplate(t, updated.GetPromptTemplate(), "team", "a-created", map[string]string{
		"replacement": "only value",
	})

	listed, err := client.ListPromptTemplates(ctx, &apiv1alpha1.ListPromptTemplatesRequest{Namespace: "team"})
	if err != nil {
		t.Fatalf("ListPromptTemplates() error = %v", err)
	}
	if len(listed.GetPromptTemplates()) != 2 {
		t.Fatalf("ListPromptTemplates() count = %d, want 2", len(listed.GetPromptTemplates()))
	}
	if got := listed.GetPromptTemplates()[0]; got.GetRef().GetName() != "a-created" || got.GetKeyCount() != 1 {
		t.Fatalf("ListPromptTemplates()[0] = %+v, want created template first with one key", got)
	}
	if got := listed.GetPromptTemplates()[1]; got.GetRef().GetName() != "z-existing" || got.GetKeyCount() != 3 {
		t.Fatalf("ListPromptTemplates()[1] = %+v, want existing template with three keys", got)
	}
	if keys := listed.GetPromptTemplates()[1].GetKeys(); len(keys) != 2 || keys[0] != "a" || keys[1] != "z" {
		t.Fatalf("ListPromptTemplates()[1].keys = %v, want [a z]", keys)
	}

	_, err = client.ListPromptTemplates(ctx, &apiv1alpha1.ListPromptTemplatesRequest{})
	assertPromptTemplateGRPCCode(t, err, codes.InvalidArgument)
	_, err = client.GetPromptTemplate(ctx, &apiv1alpha1.GetPromptTemplateRequest{})
	assertPromptTemplateGRPCCode(t, err, codes.InvalidArgument)

	_, err = client.DeletePromptTemplate(ctx, &apiv1alpha1.DeletePromptTemplateRequest{Ref: ref})
	if err != nil {
		t.Fatalf("DeletePromptTemplate() error = %v", err)
	}
	_, err = client.GetPromptTemplate(ctx, &apiv1alpha1.GetPromptTemplateRequest{Ref: ref})
	assertPromptTemplateGRPCCode(t, err, codes.NotFound)
}

func assertPromptTemplate(t *testing.T, template *apiv1alpha1.PromptTemplate, namespace, name string, data map[string]string) {
	t.Helper()
	if template.GetRef().GetNamespace() != namespace || template.GetRef().GetName() != name {
		t.Fatalf("PromptTemplate ref = %+v, want %s/%s", template.GetRef(), namespace, name)
	}
	if len(template.GetData()) != len(data) {
		t.Fatalf("PromptTemplate data = %v, want %v", template.GetData(), data)
	}
	for key, value := range data {
		if template.GetData()[key] != value {
			t.Fatalf("PromptTemplate data[%q] = %q, want %q", key, template.GetData()[key], value)
		}
	}
}

func assertPromptTemplateGRPCCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if got := status.Code(err); got != want {
		t.Fatalf("gRPC status code = %s, want %s (error: %v)", got, want, err)
	}
}
