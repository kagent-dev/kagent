package runtimeauth

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/authorization"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// Authorizer keeps the default actor policy separate from configured user
// permissions. Resource checks apply equally to HTTP, MCP, and gRPC callers.
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
	switch resource.Type {
	case "Agent":
		if verb == auth.VerbGet && resource.Namespace == session.GetAgent().GetNamespace() && resource.Name == session.GetAgent().GetName() {
			return nil
		}
	case "Session":
		// Collection authorization grants no individual Session access; the service
		// checks each row before pagination. Empty create never grants a new Session.
		if (verb == auth.VerbGet || verb == auth.VerbList) && resource.Name == "" {
			return nil
		}
		if resource.Name == session.Id && (verb == auth.VerbGet || verb == auth.VerbCreate || verb == auth.VerbUpdate) {
			return nil
		}
	case "Memory":
		if resource.Name == session.GetAgent().GetNamespace()+"/"+session.GetAgent().GetName() &&
			(verb == auth.VerbGet || verb == auth.VerbList || verb == auth.VerbCreate || verb == auth.VerbDelete) {
			return nil
		}
	}
	return fmt.Errorf("actor is not authorized for %s on %s", verb, resource.Type)
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
	return authorization.AuthorizationScope{Kind: authorization.ScopeAnyOf, AnyOf: []authorization.ScopeClause{{All: []authorization.ScopePredicate{
		{Attribute: authorization.AttributeNamespace, Operator: authorization.ScopeIn, Values: []string{session.GetAgent().GetNamespace()}},
		{Attribute: authorization.AttributeName, Operator: authorization.ScopeIn, Values: []string{session.GetAgent().GetName()}},
	}}}}, nil
}
