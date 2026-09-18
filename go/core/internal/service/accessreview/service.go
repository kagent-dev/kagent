package accessreview

import (
	"context"

	"github.com/kagent-dev/kagent/go/core/internal/service/kubeauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Service struct {
	authorizer auth.CollectionAuthorizer
}

type Target struct {
	Namespace string
	Name      string
}

type Result struct {
	Target       Target
	AllowedVerbs []auth.Verb
}

func NewService(authorizer auth.CollectionAuthorizer) *Service {
	return &Service{authorizer: authorizer}
}

func (s *Service) Check(ctx context.Context, resourceType string, verbs []auth.Verb, targets []Target) ([]Result, error) {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok {
		return nil, serviceerrors.NewUnauthenticated("Failed to get authenticated principal", nil)
	}

	results := make([]Result, len(targets))
	for i, target := range targets {
		results[i].Target = target
	}

	for _, verb := range verbs {
		scope, err := s.authorizer.Scope(ctx, session.Principal(), verb, resourceType)
		if err != nil {
			return nil, serviceerrors.NewUnavailable("Failed to read the "+resourceType+" authorization scope", err)
		}
		matcher, err := kubeauth.CompileScope(scope)
		if err != nil {
			return nil, serviceerrors.NewInternal("Failed to apply the "+resourceType+" authorization scope", err)
		}
		for i, target := range targets {
			var allowed bool
			if target.Name == "" {
				allowed = matcher.MatchesAnyName(target.Namespace)
			} else {
				allowed = matcher.Matches(&metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{
					Namespace: target.Namespace,
					Name:      target.Name,
				}})
			}
			if allowed {
				results[i].AllowedVerbs = append(results[i].AllowedVerbs, verb)
			}
		}
	}

	return results, nil
}
