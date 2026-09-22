package kubeauth

import (
	"context"

	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type AccessReviewer struct {
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

func NewAccessReviewer(authorizer auth.CollectionAuthorizer) *AccessReviewer {
	return &AccessReviewer{authorizer: authorizer}
}

func (r *AccessReviewer) Review(ctx context.Context, resourceType string, verbs []auth.Verb, targets []ReviewTarget) ([]ReviewResult, error) {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok {
		return nil, serviceerrors.NewUnauthenticated("Failed to get authenticated principal", nil)
	}
	principal := session.Principal()

	results := make([]ReviewResult, len(targets))
	for i, target := range targets {
		results[i].Target = target
	}

	for _, verb := range verbs {
		var matcher *Matcher
		for i, target := range targets {
			var allowed bool
			if target.Name != "" {
				allowed = r.authorizer.Check(ctx, principal, verb, auth.Resource{
					Type: resourceType, Namespace: target.Namespace, Name: target.Name,
				}) == nil
			} else {
				if matcher == nil {
					scope, err := r.authorizer.Scope(ctx, principal, verb, resourceType)
					if err != nil {
						return nil, serviceerrors.NewUnavailable("Failed to read the "+resourceType+" authorization scope", err)
					}
					compiled, err := CompileScope(scope)
					if err != nil {
						return nil, serviceerrors.NewInternal("Failed to apply the "+resourceType+" authorization scope", err)
					}
					matcher = &compiled
				}
				allowed = matcher.MatchesAnyName(target.Namespace)
			}
			if allowed {
				results[i].AllowedVerbs = append(results[i].AllowedVerbs, verb)
			}
		}
	}

	return results, nil
}
