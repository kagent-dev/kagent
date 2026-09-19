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
		{"valid checkpoint rename", &apiv1alpha1.UpdateCheckpointNameRequest{CheckpointId: "11111111-1111-4111-8111-111111111111", Name: "Before the detour"}, true},
		{"empty checkpoint rename", &apiv1alpha1.UpdateCheckpointNameRequest{CheckpointId: "11111111-1111-4111-8111-111111111111"}, true},
		{"checkpoint rename control character", &apiv1alpha1.UpdateCheckpointNameRequest{CheckpointId: "11111111-1111-4111-8111-111111111111", Name: "first\nsecond"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validator.Validate(test.request)
			if (err == nil) != test.valid {
				t.Fatalf("Validate() error = %v, valid = %t", err, test.valid)
			}
		})
	}
}

func TestCheckAccessRequestValidation(t *testing.T) {
	validator, err := protovalidate.New()
	if err != nil {
		t.Fatal(err)
	}
	name := "assistant"
	emptyName := ""
	targets := make([]*apiv1alpha1.AccessTarget, 101)
	for i := range targets {
		targets[i] = &apiv1alpha1.AccessTarget{Namespace: "team-a"}
	}
	for _, test := range []struct {
		name    string
		request *apiv1alpha1.CheckAccessRequest
		valid   bool
	}{
		{
			name: "named and namespace targets",
			request: &apiv1alpha1.CheckAccessRequest{
				ResourceType: apiv1alpha1.AuthorizationResourceType_AUTHORIZATION_RESOURCE_TYPE_AGENT_TEMPLATE,
				Verbs:        []apiv1alpha1.AuthorizationVerb{apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_UPDATE, apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_CREATE},
				Targets:      []*apiv1alpha1.AccessTarget{{Namespace: "team-a", Name: &name}, {Namespace: "team-b"}},
			},
			valid: true,
		},
		{
			name: "harness create and delete",
			request: &apiv1alpha1.CheckAccessRequest{
				ResourceType: apiv1alpha1.AuthorizationResourceType_AUTHORIZATION_RESOURCE_TYPE_HARNESS,
				Verbs:        []apiv1alpha1.AuthorizationVerb{apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_CREATE, apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_DELETE},
				Targets:      []*apiv1alpha1.AccessTarget{{Namespace: "team-a"}},
			},
			valid: true,
		},
		{
			name:    "missing resource type",
			request: &apiv1alpha1.CheckAccessRequest{Verbs: []apiv1alpha1.AuthorizationVerb{apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_CREATE}, Targets: []*apiv1alpha1.AccessTarget{{Namespace: "team-a"}}},
		},
		{
			name:    "missing verbs",
			request: &apiv1alpha1.CheckAccessRequest{ResourceType: apiv1alpha1.AuthorizationResourceType_AUTHORIZATION_RESOURCE_TYPE_MODEL_CONFIG, Targets: []*apiv1alpha1.AccessTarget{{Namespace: "team-a"}}},
		},
		{
			name:    "duplicate verbs",
			request: &apiv1alpha1.CheckAccessRequest{ResourceType: apiv1alpha1.AuthorizationResourceType_AUTHORIZATION_RESOURCE_TYPE_MODEL_CONFIG, Verbs: []apiv1alpha1.AuthorizationVerb{apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_GET, apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_GET}, Targets: []*apiv1alpha1.AccessTarget{{Namespace: "team-a"}}},
		},
		{
			name:    "unknown verb",
			request: &apiv1alpha1.CheckAccessRequest{ResourceType: apiv1alpha1.AuthorizationResourceType_AUTHORIZATION_RESOURCE_TYPE_MODEL_CONFIG, Verbs: []apiv1alpha1.AuthorizationVerb{99}, Targets: []*apiv1alpha1.AccessTarget{{Namespace: "team-a"}}},
		},
		{
			name:    "missing targets",
			request: &apiv1alpha1.CheckAccessRequest{ResourceType: apiv1alpha1.AuthorizationResourceType_AUTHORIZATION_RESOURCE_TYPE_MODEL_CONFIG, Verbs: []apiv1alpha1.AuthorizationVerb{apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_GET}},
		},
		{
			name:    "nil target",
			request: &apiv1alpha1.CheckAccessRequest{ResourceType: apiv1alpha1.AuthorizationResourceType_AUTHORIZATION_RESOURCE_TYPE_MODEL_CONFIG, Verbs: []apiv1alpha1.AuthorizationVerb{apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_GET}, Targets: []*apiv1alpha1.AccessTarget{nil}},
		},
		{
			name:    "too many targets",
			request: &apiv1alpha1.CheckAccessRequest{ResourceType: apiv1alpha1.AuthorizationResourceType_AUTHORIZATION_RESOURCE_TYPE_MODEL_CONFIG, Verbs: []apiv1alpha1.AuthorizationVerb{apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_GET}, Targets: targets},
		},
		{
			name:    "invalid namespace",
			request: &apiv1alpha1.CheckAccessRequest{ResourceType: apiv1alpha1.AuthorizationResourceType_AUTHORIZATION_RESOURCE_TYPE_HARNESS, Verbs: []apiv1alpha1.AuthorizationVerb{apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_DELETE}, Targets: []*apiv1alpha1.AccessTarget{{Namespace: "NOT A NAMESPACE"}}},
		},
		{
			name:    "present empty name",
			request: &apiv1alpha1.CheckAccessRequest{ResourceType: apiv1alpha1.AuthorizationResourceType_AUTHORIZATION_RESOURCE_TYPE_MODEL_CONFIG, Verbs: []apiv1alpha1.AuthorizationVerb{apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_GET}, Targets: []*apiv1alpha1.AccessTarget{{Namespace: "team-a", Name: &emptyName}}},
		},
		{
			name:    "unsupported harness verb",
			request: &apiv1alpha1.CheckAccessRequest{ResourceType: apiv1alpha1.AuthorizationResourceType_AUTHORIZATION_RESOURCE_TYPE_HARNESS, Verbs: []apiv1alpha1.AuthorizationVerb{apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_UPDATE}, Targets: []*apiv1alpha1.AccessTarget{{Namespace: "team-a", Name: &name}}},
		},
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
		&apiv1alpha1.UpdateCheckpointNameRequest{CheckpointId: "invalid"},
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
