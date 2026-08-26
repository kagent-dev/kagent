package grpcserver

import (
	"context"
	"testing"

	"buf.build/go/protovalidate"
	protovalidatemiddleware "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/protovalidate"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestProtovalidateUnaryInterceptor(t *testing.T) {
	validator, err := protovalidate.New()
	if err != nil {
		t.Fatal(err)
	}

	handled := false
	_, err = protovalidatemiddleware.UnaryServerInterceptor(validator)(
		t.Context(),
		&apiv1alpha1.CreateAgentInstanceRequest{},
		&grpc.UnaryServerInfo{},
		func(context.Context, any) (any, error) {
			handled = true
			return nil, nil
		},
	)
	if handled {
		t.Fatal("handler called for invalid request")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("validation code = %v, want %v", status.Code(err), codes.InvalidArgument)
	}
	if details := status.Convert(err).Details(); len(details) != 1 {
		t.Fatalf("validation details = %d, want 1", len(details))
	}
}

func TestAgentInstanceRequestValidation(t *testing.T) {
	validator, err := protovalidate.New()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		request proto.Message
		valid   bool
	}{
		{"ordinary name", &apiv1alpha1.CreateAgentInstanceRequest{Namespace: "team-a", Harness: "kagent", AgentTemplate: "assistant", RequestId: "request-1", Name: "Deploy 🚀"}, true},
		{"leading whitespace", &apiv1alpha1.CreateAgentInstanceRequest{Namespace: "team-a", Harness: "kagent", AgentTemplate: "assistant", RequestId: "request-1", Name: " title"}, false},
		{"control character", &apiv1alpha1.CreateAgentInstanceRequest{Namespace: "team-a", Harness: "kagent", AgentTemplate: "assistant", RequestId: "request-1", Name: "first\nsecond"}, false},
		{"invalid template filter", &apiv1alpha1.ListAgentInstancesRequest{Namespace: "team-a", AgentTemplate: "NOT A NAME"}, false},
		{"valid rename", &apiv1alpha1.UpdateAgentInstanceNameRequest{Namespace: "team-a", AgentInstanceId: "11111111-1111-4111-8111-111111111111", Name: "New title"}, true},
		{"invalid rename id", &apiv1alpha1.UpdateAgentInstanceNameRequest{Namespace: "team-a", AgentInstanceId: "not-a-uuid", Name: "New title"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validator.Validate(test.request)
			if (err == nil) != test.valid {
				t.Fatalf("Validate() error = %v, valid = %t", err, test.valid)
			}
		})
	}
}
