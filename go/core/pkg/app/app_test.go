package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	apiauthorization "github.com/kagent-dev/kagent/go/api/authorization"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/grpcserver"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/config"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
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

func (stubAuthorizer) Scope(context.Context, auth.Principal, auth.Verb, string) (apiauthorization.AuthorizationScope, error) {
	return apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAll}, nil
}

func TestOptionsResolve(t *testing.T) {
	consumerAuthn := stubAuthenticator{}
	consumerAuthz := stubAuthorizer{}

	tests := []struct {
		name      string
		opts      Options
		wantAuthn auth.AuthProvider
		wantAuthz auth.CollectionAuthorizer
	}{
		{
			name:      "both nil selects core defaults",
			opts:      Options{},
			wantAuthn: &authimpl.InsecureAuthenticator{},
			wantAuthz: &auth.NoopAuthorizer{},
		},
		{
			name:      "authenticator only leaves the default authorizer",
			opts:      Options{Authenticator: consumerAuthn},
			wantAuthn: consumerAuthn,
			wantAuthz: &auth.NoopAuthorizer{},
		},
		{
			name:      "authorizer only leaves the default authenticator",
			opts:      Options{Authorizer: consumerAuthz},
			wantAuthn: &authimpl.InsecureAuthenticator{},
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

func TestPostgresConfig(t *testing.T) {
	input := &apiv1.DatabaseConfig{VectorEnabled: true, Role: "kagent_app", Schema: new("kagent_test"), VectorSchema: new("vectors_test"), Pool: &apiv1.PostgresPoolConfig{
		MaxConns: new(int32(8)), MinConns: new(int32(1)), MaxConnIdleTime: durationpb.New(time.Minute), MaxConnLifetime: durationpb.New(10 * time.Minute),
	}}
	config := postgresConfig(input, "postgres://user:password@database/app")
	if config.URL != "postgres://user:password@database/app" || !config.VectorEnabled {
		t.Fatalf("postgres config lost connection URL or vector setting: %#v", config)
	}
	if config.Role != "kagent_app" {
		t.Fatalf("Role = %q, want kagent_app", config.Role)
	}
	if config.Schema != "kagent_test" {
		t.Fatalf("Schema = %q, want kagent_test", config.Schema)
	}
	require.Equal(t, "vectors_test", config.VectorSchema)
	if config.MaxConns == nil || *config.MaxConns != 8 {
		t.Fatalf("MaxConns = %v, want 8", config.MaxConns)
	}
	if config.MinConns == nil || *config.MinConns != 1 {
		t.Fatalf("MinConns = %v, want 1", config.MinConns)
	}
	if config.MaxConnIdleTime == nil || *config.MaxConnIdleTime != time.Minute {
		t.Fatalf("MaxConnIdleTime = %v, want 1m", config.MaxConnIdleTime)
	}
	if config.MaxConnLifetime == nil || *config.MaxConnLifetime != 10*time.Minute {
		t.Fatalf("MaxConnLifetime = %v, want 10m", config.MaxConnLifetime)
	}
}

// The built-in tracks must reach their final version before a library consumer's,
// which may reference them, so order is the contract here -- not membership.
func TestExtraMigrationsAppendAfterBuiltins(t *testing.T) {
	extra := []migrations.Source{{Name: "custom-track", Schema: "custom"}}
	sources, err := migrationSources(&apiv1.DatabaseConfig{Schema: new("kagent_test"), VectorSchema: new("extensions")}, extra)
	if err != nil {
		t.Fatal(err)
	}

	if len(sources) != len(migrations.BuiltinSources(false))+len(extra) {
		t.Fatalf("sources = %d entries, want builtins + %d", len(sources), len(extra))
	}
	if got := sources[0].Name; got != "core" {
		t.Errorf("first source = %q, want the built-in %q", got, "core")
	}
	require.Equal(t, "kagent_test", sources[0].Schema)
	if got := sources[len(sources)-1].Name; got != "custom-track" {
		t.Errorf("last source = %q, want the extra track", got)
	}
}

func TestMigrationSourcesUseConfiguredSchemas(t *testing.T) {
	sources, err := migrationSources(&apiv1.DatabaseConfig{VectorEnabled: true, Schema: new("tenant"), VectorSchema: new("vectors")}, nil)
	require.NoError(t, err)
	require.Len(t, sources, 2)
	for _, source := range sources {
		require.Equal(t, "tenant", source.Schema)
		require.Equal(t, "vectors", source.VectorSchema)
	}
}

func TestExtraMigrationsRequireSchema(t *testing.T) {
	_, err := migrationSources(&apiv1.DatabaseConfig{Schema: new("kagent_test"), VectorSchema: new("extensions")}, []migrations.Source{{Name: "custom-track"}})
	if err == nil || err.Error() != `extra migration source "custom-track" must set Schema` {
		t.Fatalf("migrationSources error = %v", err)
	}
}

// SetupLogger rejects a bad level before it touches the global logger, so this
// case does not disturb whatever logger the rest of the suite runs under.
func TestSetupLoggerRejectsBadLevel(t *testing.T) {
	require.Error(t, SetupLogger("not-a-level"))
}

func TestRunRejectsInvalidConfigBeforeIO(t *testing.T) {
	cfg := &apiv1.ControllerConfig{}
	config.DefaultController(cfg)
	require.ErrorContains(t, Run(t.Context(), cfg, Options{}), "database.url_file")
	cfg.Database.UrlFile = "missing-secret"
	cfg.Gateway.Push.SigningPrivateKeyFile = "missing-seed"
	cfg.Sessions.QuiescenceRecoveryInterval = durationpb.New(0)
	require.ErrorContains(t, Run(t.Context(), cfg, Options{}), "quiescence_recovery_interval")
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
