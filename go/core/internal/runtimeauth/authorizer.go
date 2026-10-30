package runtimeauth

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/authorization"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/authscope"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// Authorizer applies configured policy within the verified actor's native
// resource boundary for HTTP, MCP, and gRPC callers.
type Authorizer struct {
	users auth.CollectionAuthorizer
	store SessionStore
}

var _ auth.CollectionAuthorizer = (*Authorizer)(nil)

func NewAuthorizer(users auth.CollectionAuthorizer, store SessionStore) *Authorizer {
	return &Authorizer{users: users, store: store}
}

func (a *Authorizer) Check(ctx context.Context, principal auth.Principal, verb auth.Verb, resource auth.Resource) error {
	if principal.RuntimeBinding == nil {
		return a.users.Check(ctx, principal, verb, resource)
	}
	session, err := Session(ctx, a.store)
	if err != nil {
		return err
	}
	if !runtimeResourceAllowed(session, verb, resource) {
		return fmt.Errorf("actor is not authorized for %s on %s", verb, resource.Type)
	}
	return a.users.Check(ctx, principal, verb, resource)
}

func runtimeResourceAllowed(session *apiv1.Session, verb auth.Verb, resource auth.Resource) bool {
	switch resource.Type {
	case "Agent":
		if verb == auth.VerbGet && resource.Namespace == session.GetAgent().GetNamespace() && resource.Name == session.GetAgent().GetName() {
			return true
		}
	case "Session":
		// Collection authorization grants no individual Session access; the service
		// checks each row before pagination. Empty create never grants a new Session.
		if (verb == auth.VerbGet || verb == auth.VerbList) && resource.Name == "" {
			return true
		}
		if resource.Name == session.Id && (verb == auth.VerbGet || verb == auth.VerbCreate || verb == auth.VerbUpdate) {
			return true
		}
	case "TaskStore":
		return resource.Name == session.Id && resource.Namespace == session.GetAgent().GetNamespace() &&
			(verb == auth.VerbGet || verb == auth.VerbList || verb == auth.VerbCreate || verb == auth.VerbUpdate)
	case "Memory":
		if resource.Name == session.GetAgent().GetNamespace()+"/"+session.GetAgent().GetName() &&
			(verb == auth.VerbGet || verb == auth.VerbList || verb == auth.VerbCreate || verb == auth.VerbDelete) {
			return true
		}
	}
	return false
}

func (a *Authorizer) Scope(ctx context.Context, principal auth.Principal, verb auth.Verb, resourceType string) (authorization.AuthorizationScope, error) {
	if principal.RuntimeBinding == nil {
		return a.users.Scope(ctx, principal, verb, resourceType)
	}
	session, err := Session(ctx, a.store)
	if err != nil {
		return authorization.AuthorizationScope{}, err
	}
	if verb != auth.VerbList || resourceType != "Agent" {
		return authorization.AuthorizationScope{Kind: authorization.ScopeNone}, nil
	}
	scope, err := a.users.Scope(ctx, principal, verb, resourceType)
	if err != nil {
		return authorization.AuthorizationScope{}, err
	}
	matcher, err := authscope.CompileScope(scope, authorization.AttributeNamespace)
	if err != nil {
		return authorization.AuthorizationScope{}, err
	}
	if !matcher.Matches(session.GetAgent().GetNamespace(), session.GetAgent().GetName()) {
		return authorization.AuthorizationScope{Kind: authorization.ScopeNone}, nil
	}
	return authorization.AuthorizationScope{Kind: authorization.ScopeAnyOf, AnyOf: []authorization.ScopeClause{{All: []authorization.ScopePredicate{
		{Attribute: authorization.AttributeNamespace, Operator: authorization.ScopeIn, Values: []string{session.GetAgent().GetNamespace()}},
		{Attribute: authorization.AttributeName, Operator: authorization.ScopeIn, Values: []string{session.GetAgent().GetName()}},
	}}}}, nil
}
