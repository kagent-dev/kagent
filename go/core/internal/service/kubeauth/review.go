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
	share, shared := auth.ShareContextFrom(ctx)

	results := make([]ReviewResult, len(targets))
	for i, target := range targets {
		results[i].Target = target
	}

	for _, verb := range verbs {
		access := auth.AccessMode(verb)
		if verb == auth.VerbGet || verb == auth.VerbList {
			access = auth.AccessRead
		}
		if shared && !share.AllowsAccess(access) {
			continue
		}
		var matcher, getMatcher *Matcher
		for i, target := range targets {
			// Local scope matching does not observe cancellation, so stop between targets.
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			var allowed bool
			if target.Name != "" {
				resource := auth.Resource{
					Type: resourceType, Namespace: target.Namespace, Name: target.Name,
				}
				allowed = r.authorizer.Check(ctx, principal, verb, resource) == nil
				// Catalog updates require GET to read the object before writing it.
				if allowed && verb == auth.VerbUpdate {
					allowed = r.authorizer.Check(ctx, principal, auth.VerbGet, resource) == nil
				}
			} else {
				if matcher == nil {
					var err error
					matcher, err = r.scope(ctx, principal, verb, resourceType)
					if err != nil {
						return nil, err
					}
					if verb == auth.VerbUpdate {
						getMatcher, err = r.scope(ctx, principal, auth.VerbGet, resourceType)
						if err != nil {
							return nil, err
						}
					}
				}
				if verb == auth.VerbUpdate {
					allowed = matcher.overlapsNamespace(*getMatcher, target.Namespace)
				} else {
					allowed = matcher.matchesAnyName(target.Namespace)
				}
			}
			if allowed {
				results[i].AllowedVerbs = append(results[i].AllowedVerbs, verb)
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (r *AccessReviewer) scope(ctx context.Context, principal auth.Principal, verb auth.Verb, resourceType string) (*Matcher, error) {
	scope, err := r.authorizer.Scope(ctx, principal, verb, resourceType)
	if err != nil {
		return nil, serviceerrors.NewUnavailable("Failed to read the "+resourceType+" authorization scope", err)
	}
	compiled, err := CompileScope(scope)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to apply the "+resourceType+" authorization scope", err)
	}
	return &compiled, nil
}
