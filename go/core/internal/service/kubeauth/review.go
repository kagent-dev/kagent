package kubeauth

import (
	"context"

	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Reviewer struct {
	authorizer auth.CollectionAuthorizer
}

type ReviewTarget struct {
	Namespace string
	Name      string
}

type ReviewResult struct {
	Target       ReviewTarget
	AllowedVerbs []auth.Verb
}

func NewReviewer(authorizer auth.CollectionAuthorizer) *Reviewer {
	return &Reviewer{authorizer: authorizer}
}

func (r *Reviewer) Review(ctx context.Context, resourceType string, verbs []auth.Verb, targets []ReviewTarget) ([]ReviewResult, error) {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok {
		return nil, serviceerrors.NewUnauthenticated("Failed to get authenticated principal", nil)
	}

	results := make([]ReviewResult, len(targets))
	for i, target := range targets {
		results[i].Target = target
	}

	for _, verb := range verbs {
		scope, err := r.authorizer.Scope(ctx, session.Principal(), verb, resourceType)
		if err != nil {
			return nil, serviceerrors.NewUnavailable("Failed to read the "+resourceType+" authorization scope", err)
		}
		matcher, err := CompileScope(scope)
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
