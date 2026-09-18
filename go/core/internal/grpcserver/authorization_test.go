package grpcserver

import (
	"context"
	"net"
	"testing"

	apiauthorization "github.com/kagent-dev/kagent/go/api/authorization"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/accessreview"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

type accessReviewScopeCall struct {
	verb         pkgauth.Verb
	resourceType string
}

type accessReviewAuthorizer struct {
	scopeCalls []accessReviewScopeCall
}

func (*accessReviewAuthorizer) Check(context.Context, pkgauth.Principal, pkgauth.Verb, pkgauth.Resource) error {
	return nil
}

func (a *accessReviewAuthorizer) Scope(_ context.Context, _ pkgauth.Principal, verb pkgauth.Verb, resourceType string) (apiauthorization.AuthorizationScope, error) {
	a.scopeCalls = append(a.scopeCalls, accessReviewScopeCall{verb: verb, resourceType: resourceType})
	return apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAll}, nil
}

func TestAuthorizationServiceGeneratedClient(t *testing.T) {
	authorizer := &accessReviewAuthorizer{}
	listener := bufconn.Listen(DefaultMaxMessageSize)
	server, err := New(Config{
		Listener:             listener,
		Registerer:           prometheus.NewRegistry(),
		Authenticator:        &authimpl.UnsecureAuthenticator{},
		SystemService:        testSystemService(),
		AuthorizationService: accessreview.NewService(authorizer),
	})
	require.NoError(t, err)
	serverContext, cancelServer := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Start(serverContext) }()
	t.Cleanup(func() {
		cancelServer()
		assert.NoError(t, <-done)
	})

	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	client := apiv1alpha1.NewAuthorizationServiceClient(connection)
	name := "assistant"

	response, err := client.CheckAccess(t.Context(), &apiv1alpha1.CheckAccessRequest{
		ResourceType: apiv1alpha1.AuthorizationResourceType_AUTHORIZATION_RESOURCE_TYPE_AGENT_TEMPLATE,
		Verbs: []apiv1alpha1.AuthorizationVerb{
			apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_UPDATE,
			apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_CREATE,
		},
		Targets: []*apiv1alpha1.AccessTarget{
			{Namespace: "team-a", Name: &name},
			{Namespace: "team-b"},
		},
	})
	require.NoError(t, err)
	want := &apiv1alpha1.CheckAccessResponse{
		Results: []*apiv1alpha1.ResourceAccess{
			{
				Target: &apiv1alpha1.AccessTarget{Namespace: "team-a", Name: &name},
				AllowedVerbs: []apiv1alpha1.AuthorizationVerb{
					apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_UPDATE,
					apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_CREATE,
				},
			},
			{
				Target: &apiv1alpha1.AccessTarget{Namespace: "team-b"},
				AllowedVerbs: []apiv1alpha1.AuthorizationVerb{
					apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_UPDATE,
					apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_CREATE,
				},
			},
		},
	}
	assert.True(t, proto.Equal(want, response), "response = %v, want %v", response, want)
	assert.Equal(t, []accessReviewScopeCall{
		{verb: pkgauth.VerbUpdate, resourceType: pkgauth.ResourceAgentTemplate},
		{verb: pkgauth.VerbCreate, resourceType: pkgauth.ResourceAgentTemplate},
	}, authorizer.scopeCalls)
}
