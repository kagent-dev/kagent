package grpcserver

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/accessreview"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type authorizationServer struct {
	apiv1alpha1.UnimplementedAuthorizationServiceServer
	service *accessreview.Service
}

func (s *authorizationServer) CheckAccess(ctx context.Context, request *apiv1alpha1.CheckAccessRequest) (*apiv1alpha1.CheckAccessResponse, error) {
	resourceTypes := map[apiv1alpha1.AuthorizationResourceType]string{
		apiv1alpha1.AuthorizationResourceType_AUTHORIZATION_RESOURCE_TYPE_AGENT_TEMPLATE: auth.ResourceAgentTemplate,
		apiv1alpha1.AuthorizationResourceType_AUTHORIZATION_RESOURCE_TYPE_HARNESS:        auth.ResourceHarness,
		apiv1alpha1.AuthorizationResourceType_AUTHORIZATION_RESOURCE_TYPE_MODEL_CONFIG:   auth.ResourceModelConfig,
	}
	verbs := map[apiv1alpha1.AuthorizationVerb]auth.Verb{
		apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_GET:    auth.VerbGet,
		apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_CREATE: auth.VerbCreate,
		apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_UPDATE: auth.VerbUpdate,
		apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_DELETE: auth.VerbDelete,
	}
	authorizationVerbs := map[auth.Verb]apiv1alpha1.AuthorizationVerb{
		auth.VerbGet:    apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_GET,
		auth.VerbCreate: apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_CREATE,
		auth.VerbUpdate: apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_UPDATE,
		auth.VerbDelete: apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_DELETE,
	}
	requestVerbs := make([]auth.Verb, len(request.GetVerbs()))
	for i, verb := range request.GetVerbs() {
		requestVerbs[i] = verbs[verb]
	}
	requestTargets := make([]accessreview.Target, len(request.GetTargets()))
	for i, target := range request.GetTargets() {
		requestTargets[i] = accessreview.Target{Namespace: target.GetNamespace(), Name: target.GetName()}
	}

	results, err := s.service.Check(ctx, resourceTypes[request.GetResourceType()], requestVerbs, requestTargets)
	if err != nil {
		return nil, err
	}

	response := &apiv1alpha1.CheckAccessResponse{Results: make([]*apiv1alpha1.ResourceAccess, len(results))}
	for i, result := range results {
		target := &apiv1alpha1.AccessTarget{Namespace: result.Target.Namespace}
		if result.Target.Name != "" {
			name := result.Target.Name
			target.Name = &name
		}
		allowedVerbs := make([]apiv1alpha1.AuthorizationVerb, len(result.AllowedVerbs))
		for j, verb := range result.AllowedVerbs {
			allowedVerbs[j] = authorizationVerbs[verb]
		}
		response.Results[i] = &apiv1alpha1.ResourceAccess{Target: target, AllowedVerbs: allowedVerbs}
	}
	return response, nil
}
