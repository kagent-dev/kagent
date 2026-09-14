package grpcserver

import (
	"context"
	"slices"
	"testing"

	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type recordingResourceScopeSource struct {
	scopes map[auth.Verb]auth.AuthorizationScope
	calls  []auth.Verb
}

func (s *recordingResourceScopeSource) Scope(_ context.Context, verb auth.Verb) (auth.AuthorizationScope, error) {
	s.calls = append(s.calls, verb)
	return s.scopes[verb], nil
}

func TestResourceCapabilitiesUseActionScopes(t *testing.T) {
	source := &recordingResourceScopeSource{scopes: map[auth.Verb]auth.AuthorizationScope{
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
	}}
	capabilities, err := loadCollectionResourceCapabilities(t.Context(), source, true)
	if err != nil {
		t.Fatalf("loadCollectionResourceCapabilities() error = %v", err)
	}
	allowed := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "allowed"}}
	denied := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Namespace: "team-b", Name: "denied"}}
	if !capabilities.canCreate || !capabilities.canUpdate(allowed) || capabilities.canUpdate(denied) || capabilities.canDelete(allowed) {
		t.Fatalf("loadCollectionResourceCapabilities() = %+v", capabilities)
	}

	source.calls = nil
	if _, err := loadResourceCapabilities(t.Context(), source, true); err != nil {
		t.Fatalf("loadResourceCapabilities() error = %v", err)
	}
	if want := []auth.Verb{auth.VerbUpdate, auth.VerbDelete}; !slices.Equal(source.calls, want) {
		t.Fatalf("loadResourceCapabilities() calls = %v, want %v", source.calls, want)
	}
}
