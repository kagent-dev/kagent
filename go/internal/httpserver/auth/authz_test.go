package auth_test

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	pkgauth "github.com/kagent-dev/kagent/go/pkg/auth"
)

func TestReadOnlyAuthorizer(t *testing.T) {
	authorizer := &auth.ReadOnlyAuthorizer{}
	ctx := context.Background()
	principal := pkgauth.Principal{}
	resource := pkgauth.Resource{Name: "test", Type: "agent"}

	tests := []struct {
		name    string
		verb    pkgauth.Verb
		wantErr bool
	}{
		{name: "allows get", verb: pkgauth.VerbGet, wantErr: false},
		{name: "rejects create", verb: pkgauth.VerbCreate, wantErr: true},
		{name: "rejects update", verb: pkgauth.VerbUpdate, wantErr: true},
		{name: "rejects delete", verb: pkgauth.VerbDelete, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := authorizer.Check(ctx, principal, tt.verb, resource)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadOnlyAuthorizer.Check() verb=%s, error = %v, wantErr %v", tt.verb, err, tt.wantErr)
			}
		})
	}
}

func TestNoopAuthorizer(t *testing.T) {
	authorizer := &auth.NoopAuthorizer{}
	ctx := context.Background()
	principal := pkgauth.Principal{}
	resource := pkgauth.Resource{Name: "test", Type: "agent"}

	verbs := []pkgauth.Verb{pkgauth.VerbGet, pkgauth.VerbCreate, pkgauth.VerbUpdate, pkgauth.VerbDelete}
	for _, verb := range verbs {
		t.Run(string(verb), func(t *testing.T) {
			if err := authorizer.Check(ctx, principal, verb, resource); err != nil {
				t.Errorf("NoopAuthorizer.Check() verb=%s, unexpected error: %v", verb, err)
			}
		})
	}
}
