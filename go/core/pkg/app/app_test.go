package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"testing"

	"github.com/kagent-dev/kagent/go/core/internal/grpcserver"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
)

// stubAuthenticator and stubAuthorizer stand in for a library consumer's own
// implementations. They only need to be distinguishable from core's defaults.
type stubAuthenticator struct{ auth.AuthProvider }

func (stubAuthenticator) Authenticate(context.Context, http.Header, url.Values) (auth.Session, error) {
	return nil, nil
}

type stubAuthorizer struct{}

func (stubAuthorizer) Check(context.Context, auth.Principal, auth.Verb, auth.Resource) error {
	return nil
}

func TestOptionsResolve(t *testing.T) {
	consumerAuthn := stubAuthenticator{}
	consumerAuthz := stubAuthorizer{}

	tests := []struct {
		name      string
		opts      Options
		wantAuthn auth.AuthProvider
		wantAuthz auth.Authorizer
	}{
		{
			name:      "both nil selects core defaults",
			opts:      Options{},
			wantAuthn: &authimpl.UnsecureAuthenticator{},
			wantAuthz: &authimpl.NoopAuthorizer{},
		},
		{
			name:      "authenticator only leaves the default authorizer",
			opts:      Options{Authenticator: consumerAuthn},
			wantAuthn: consumerAuthn,
			wantAuthz: &authimpl.NoopAuthorizer{},
		},
		{
			name:      "authorizer only leaves the default authenticator",
			opts:      Options{Authorizer: consumerAuthz},
			wantAuthn: &authimpl.UnsecureAuthenticator{},
			wantAuthz: consumerAuthz,
		},
		{
			name:      "both supplied replaces both",
			opts:      Options{Authenticator: consumerAuthn, Authorizer: consumerAuthz},
			wantAuthn: consumerAuthn,
			wantAuthz: consumerAuthz,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authn, authz := test.opts.resolve()
			if authn == nil || authz == nil {
				t.Fatalf("resolve returned a nil component: authn=%v authz=%v", authn, authz)
			}
			if got, want := fmt.Sprintf("%T", authn), fmt.Sprintf("%T", test.wantAuthn); got != want {
				t.Errorf("authenticator = %s, want %s", got, want)
			}
			if got, want := fmt.Sprintf("%T", authz), fmt.Sprintf("%T", test.wantAuthz); got != want {
				t.Errorf("authorizer = %s, want %s", got, want)
			}
		})
	}
}

func TestNamespaces(t *testing.T) {
	want := []string{"one", "two"}
	if got := namespaces(" one, ,two,"); !reflect.DeepEqual(got, want) {
		t.Fatalf("namespaces() = %q, want %q", got, want)
	}
}

func TestNamespaceCache(t *testing.T) {
	if got := namespaceCache(nil); got != nil {
		t.Fatalf("namespaceCache(nil) = %#v, want nil", got)
	}
	got := namespaceCache([]string{"team-a", "team-b"})
	if len(got) != 2 {
		t.Fatalf("namespaceCache() = %#v", got)
	}
	if _, ok := got["team-a"]; !ok {
		t.Fatal("namespaceCache() missing team-a")
	}
	if _, ok := got["team-b"]; !ok {
		t.Fatal("namespaceCache() missing team-b")
	}
}

// The built-in tracks must reach their final version before a library consumer's,
// which may reference them, so order is the contract here -- not membership.
func TestExtraMigrationsAppendAfterBuiltins(t *testing.T) {
	extra := []migrations.Source{{Name: "custom-track"}}
	sources := append(migrations.BuiltinSources(false), extra...)

	if len(sources) != len(migrations.BuiltinSources(false))+len(extra) {
		t.Fatalf("sources = %d entries, want builtins + %d", len(sources), len(extra))
	}
	if got := sources[0].Name; got != "core" {
		t.Errorf("first source = %q, want the built-in %q", got, "core")
	}
	if got := sources[len(sources)-1].Name; got != "custom-track" {
		t.Errorf("last source = %q, want the extra track", got)
	}
}

// SetupLogger rejects a bad level before it touches the global logger, so this
// case does not disturb whatever logger the rest of the suite runs under.
func TestSetupLoggerRejectsBadLevel(t *testing.T) {
	t.Setenv("LOG_LEVEL", "not-a-level")
	if err := SetupLogger(); err == nil {
		t.Fatal("SetupLogger accepted an unparseable LOG_LEVEL")
	}
}

func TestMergePolicies(t *testing.T) {
	defaults := grpcserver.MethodPolicies{"/core.Svc/Get": auth.AccessRead}

	t.Run("adds a consumer method", func(t *testing.T) {
		merged, err := mergePolicies(defaults, map[string]auth.AccessMode{
			"/consumer.Svc/Public": auth.AccessPublic,
		})
		if err != nil {
			t.Fatalf("mergePolicies: %v", err)
		}
		if got := merged["/consumer.Svc/Public"]; got != auth.AccessPublic {
			t.Errorf("consumer method = %q, want %q", got, auth.AccessPublic)
		}
		if got := merged["/core.Svc/Get"]; got != auth.AccessRead {
			t.Errorf("core method = %q, want %q", got, auth.AccessRead)
		}
	})

	// The security property: a consumer must not be able to reclassify a core
	// method, or Options becomes a way to make an authenticated method public.
	t.Run("refuses to reclassify a core method", func(t *testing.T) {
		_, err := mergePolicies(defaults, map[string]auth.AccessMode{
			"/core.Svc/Get": auth.AccessPublic,
		})
		if err == nil {
			t.Fatal("mergePolicies allowed a core method to be overridden")
		}
	})

	t.Run("nil extra leaves defaults intact", func(t *testing.T) {
		merged, err := mergePolicies(defaults, nil)
		if err != nil || len(merged) != len(defaults) {
			t.Fatalf("mergePolicies(nil) = %v, %v", merged, err)
		}
	})
}
