package e2e_test

import (
	"context"
	"testing"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

func TestE2EAccessReview(t *testing.T) {
	t.Parallel()
	connection, err := grpc.NewClient(interactionTarget(t), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "e2e"), time.Minute)
	t.Cleanup(cancel)
	name := "smoke"

	response, err := apiv1alpha1.NewAuthorizationServiceClient(connection).CheckAccess(ctx, &apiv1alpha1.CheckAccessRequest{
		ResourceType: apiv1alpha1.AuthorizationResourceType_AUTHORIZATION_RESOURCE_TYPE_AGENT_TEMPLATE,
		Verbs: []apiv1alpha1.AuthorizationVerb{
			apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_CREATE,
			apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_UPDATE,
		},
		Targets: []*apiv1alpha1.AccessTarget{
			{Namespace: "kagent"},
			{Namespace: "kagent", Name: &name},
		},
	})
	require.NoError(t, err)
	want := &apiv1alpha1.CheckAccessResponse{Results: []*apiv1alpha1.ResourceAccess{
		{
			Target:       &apiv1alpha1.AccessTarget{Namespace: "kagent"},
			AllowedVerbs: []apiv1alpha1.AuthorizationVerb{apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_CREATE, apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_UPDATE},
		},
		{
			Target:       &apiv1alpha1.AccessTarget{Namespace: "kagent", Name: &name},
			AllowedVerbs: []apiv1alpha1.AuthorizationVerb{apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_CREATE, apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_UPDATE},
		},
	}}
	require.True(t, proto.Equal(want, response), "response = %v, want %v", response, want)
}
