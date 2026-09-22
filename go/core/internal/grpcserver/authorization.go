package grpcserver

import (
	"context"
	"fmt"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubeauth"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

var authorizationResourceTypes = map[apiv1alpha1.AuthorizationResourceType]string{
	apiv1alpha1.AuthorizationResourceType_AUTHORIZATION_RESOURCE_TYPE_AGENT_TEMPLATE: auth.ResourceAgentTemplate,
	apiv1alpha1.AuthorizationResourceType_AUTHORIZATION_RESOURCE_TYPE_HARNESS:        auth.ResourceHarness,
	apiv1alpha1.AuthorizationResourceType_AUTHORIZATION_RESOURCE_TYPE_MODEL_CONFIG:   auth.ResourceModelConfig,
}

var authorizationVerbs = map[apiv1alpha1.AuthorizationVerb]auth.Verb{
	apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_GET:    auth.VerbGet,
	apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_CREATE: auth.VerbCreate,
	apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_UPDATE: auth.VerbUpdate,
	apiv1alpha1.AuthorizationVerb_AUTHORIZATION_VERB_DELETE: auth.VerbDelete,
}

type authorizationServer struct {
	apiv1alpha1.UnimplementedAuthorizationServiceServer
	reviewer *kubeauth.AccessReviewer
}

func (s *authorizationServer) CheckAccess(ctx context.Context, request *apiv1alpha1.CheckAccessRequest) (*apiv1alpha1.CheckAccessResponse, error) {
	resourceType, ok := authorizationResourceTypes[request.GetResourceType()]
	if !ok {
		return nil, fmt.Errorf("authorization resource type %q has no domain mapping", request.GetResourceType())
	}
	requestVerbs := make([]auth.Verb, len(request.GetVerbs()))
	responseVerbs := make(map[auth.Verb]apiv1alpha1.AuthorizationVerb, len(request.GetVerbs()))
	for i, apiVerb := range request.GetVerbs() {
		domainVerb, ok := authorizationVerbs[apiVerb]
		if !ok {
			return nil, fmt.Errorf("authorization verb %q has no domain mapping", apiVerb)
		}
		requestVerbs[i] = domainVerb
		responseVerbs[domainVerb] = apiVerb
	}
	requestTargets := make([]kubeauth.ReviewTarget, len(request.GetTargets()))
	for i, target := range request.GetTargets() {
		requestTargets[i] = kubeauth.ReviewTarget{Namespace: target.GetNamespace(), Name: target.GetName()}
	}

	results, err := s.reviewer.Review(ctx, resourceType, requestVerbs, requestTargets)
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
			allowedVerbs[j] = responseVerbs[verb]
		}
		response.Results[i] = &apiv1alpha1.ResourceAccess{Target: target, AllowedVerbs: allowedVerbs}
	}
	return response, nil
}
