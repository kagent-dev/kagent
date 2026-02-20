package auth_test

import (
	"context"
	"testing"

	authimpl "github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/pkg/auth"
)

func TestNoopAuthorizer(t *testing.T) {
	authorizer := &authimpl.NoopAuthorizer{}
	principal := auth.Principal{User: auth.User{ID: "test-user"}}
	resource := auth.Resource{Name: "test-agent", Type: "agents"}

	for _, verb := range []auth.Verb{auth.VerbGet, auth.VerbCreate, auth.VerbUpdate, auth.VerbDelete} {
		if err := authorizer.Check(context.Background(), principal, verb, resource); err != nil {
			t.Errorf("NoopAuthorizer should allow %s but got error: %v", verb, err)
		}
	}
}

func TestReadOnlyAuthorizer_AllowsGet(t *testing.T) {
	authorizer := &authimpl.ReadOnlyAuthorizer{}
	principal := auth.Principal{User: auth.User{ID: "test-user"}}
	resource := auth.Resource{Name: "test-agent", Type: "agents"}

	if err := authorizer.Check(context.Background(), principal, auth.VerbGet, resource); err != nil {
		t.Errorf("ReadOnlyAuthorizer should allow get but got error: %v", err)
	}
}

func TestReadOnlyAuthorizer_RejectsMutations(t *testing.T) {
	authorizer := &authimpl.ReadOnlyAuthorizer{}
	principal := auth.Principal{User: auth.User{ID: "test-user"}}
	resource := auth.Resource{Name: "test-agent", Type: "agents"}

	for _, verb := range []auth.Verb{auth.VerbCreate, auth.VerbUpdate, auth.VerbDelete} {
		err := authorizer.Check(context.Background(), principal, verb, resource)
		if err == nil {
			t.Errorf("ReadOnlyAuthorizer should reject %s but allowed it", verb)
		}
	}
}

func TestReadOnlyAuthorizer_ErrorMessage(t *testing.T) {
	authorizer := &authimpl.ReadOnlyAuthorizer{}
	principal := auth.Principal{User: auth.User{ID: "test-user"}}
	resource := auth.Resource{Name: "my-agent", Type: "agents"}

	err := authorizer.Check(context.Background(), principal, auth.VerbCreate, resource)
	if err == nil {
		t.Fatal("expected error for create verb")
	}

	expected := "forbidden: read-only mode is enabled, create operations on agents are not allowed"
	if err.Error() != expected {
		t.Errorf("expected error message %q, got %q", expected, err.Error())
	}
}
