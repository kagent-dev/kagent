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
		{"ordinary name", &apiv1alpha1.CreateAgentInstanceRequest{Harness: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "kagent"}, AgentTemplate: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"}, RequestId: "request-1", Name: "Deploy 🚀"}, true},
		{"different target namespaces", &apiv1alpha1.CreateAgentInstanceRequest{Harness: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "kagent"}, AgentTemplate: &apiv1alpha1.ResourceReference{Namespace: "team-b", Name: "assistant"}, RequestId: "request-1"}, false},
		{"list without namespace", &apiv1alpha1.ListAgentInstancesRequest{}, true},
		{"missing target namespace", &apiv1alpha1.ListAgentInstancesRequest{AgentTemplate: &apiv1alpha1.ResourceReference{Name: "assistant"}}, false},
		{"leading whitespace", &apiv1alpha1.CreateAgentInstanceRequest{Harness: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "kagent"}, AgentTemplate: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"}, RequestId: "request-1", Name: " title"}, false},
		{"control character", &apiv1alpha1.CreateAgentInstanceRequest{Harness: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "kagent"}, AgentTemplate: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"}, RequestId: "request-1", Name: "first\nsecond"}, false},
		{"invalid template filter", &apiv1alpha1.ListAgentInstancesRequest{AgentTemplate: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "NOT A NAME"}}, false},
		{"valid rename", &apiv1alpha1.UpdateAgentInstanceNameRequest{AgentInstanceId: "11111111-1111-4111-8111-111111111111", Name: "New title"}, true},
		{"invalid rename id", &apiv1alpha1.UpdateAgentInstanceNameRequest{AgentInstanceId: "not-a-uuid", Name: "New title"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validator.Validate(test.request)
			if (err == nil) != test.valid {
				t.Fatalf("Validate() error = %v, valid = %t", err, test.valid)
			}
		})
	}
}

func TestInvalidInstanceAndCheckpointIDsNeverReachHandlers(t *testing.T) {
	validator, err := protovalidate.New()
	if err != nil {
		t.Fatal(err)
	}
	requests := []proto.Message{
		&apiv1alpha1.GetAgentInstanceRequest{AgentInstanceId: "invalid"},
		&apiv1alpha1.UpdateAgentInstanceNameRequest{AgentInstanceId: "invalid"},
		&apiv1alpha1.SuspendAgentInstanceRequest{AgentInstanceId: "invalid"},
		&apiv1alpha1.ResumeAgentInstanceRequest{AgentInstanceId: "invalid"},
		&apiv1alpha1.DeleteAgentInstanceRequest{AgentInstanceId: "invalid"},
		&apiv1alpha1.CreateAgentInstanceShareRequest{AgentInstanceId: "invalid", Permission: apiv1alpha1.AgentInstanceSharePermission_AGENT_INSTANCE_SHARE_PERMISSION_READ_ONLY},
		&apiv1alpha1.ListAgentInstanceSharesRequest{AgentInstanceId: "invalid"},
		&apiv1alpha1.RevokeAgentInstanceShareRequest{ShareId: "invalid"},
		&apiv1alpha1.CreateCheckpointRequest{AgentInstanceId: "invalid", RequestId: "request"},
		&apiv1alpha1.GetCheckpointRequest{CheckpointId: "invalid"},
		&apiv1alpha1.ListCheckpointsRequest{AgentInstanceId: "invalid"},
		&apiv1alpha1.DeleteCheckpointRequest{CheckpointId: "invalid"},
		&apiv1alpha1.ForkAgentInstanceRequest{CheckpointId: "invalid", RequestId: "request"},
	}
	for _, request := range requests {
		t.Run(string(proto.MessageName(request)), func(t *testing.T) {
			_, err := protovalidatemiddleware.UnaryServerInterceptor(validator)(
				t.Context(), request, &grpc.UnaryServerInfo{},
				func(context.Context, any) (any, error) {
					t.Fatal("handler called with an invalid UUID")
					return nil, nil
				},
			)
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("validation code = %v, want %v", status.Code(err), codes.InvalidArgument)
			}
		})
	}
}
