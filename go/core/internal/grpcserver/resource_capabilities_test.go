package grpcserver

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestResourceCapabilitiesUseActionScopes(t *testing.T) {
	scopes := map[auth.Verb]auth.AuthorizationScope{
		auth.VerbCreate: {Kind: auth.ScopeAnyOf, AnyOf: []auth.ScopeClause{{All: []auth.ScopePredicate{{
			Attribute: auth.AttributeNamespace,
			Operator:  auth.ScopeIn,
			Values:    []string{"team-a"},
		}}}}},
		auth.VerbUpdate: {Kind: auth.ScopeAnyOf, AnyOf: []auth.ScopeClause{{All: []auth.ScopePredicate{{
			Attribute: auth.AttributeNamespace,
			Operator:  auth.ScopeIn,
			Values:    []string{"team-a"},
		}}}}},
		auth.VerbDelete: {Kind: auth.ScopeNone},
	}
	capabilities, err := loadResourceCapabilities(t.Context(), func(_ context.Context, verb auth.Verb) (auth.AuthorizationScope, error) {
		return scopes[verb], nil
	}, true)
	if err != nil {
		t.Fatalf("loadResourceCapabilities() error = %v", err)
	}
	allowed := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "allowed"}}
	denied := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Namespace: "team-b", Name: "denied"}}
	if !capabilities.canCreate || !capabilities.canUpdate(allowed) || capabilities.canUpdate(denied) || capabilities.canDelete(allowed) {
		t.Fatalf("loadResourceCapabilities() = %+v", capabilities)
	}
}
