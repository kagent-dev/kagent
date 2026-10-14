package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"

	apiauthorization "github.com/kagent-dev/kagent/go/api/authorization"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/internal/grpcserver"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/config"
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
	input := &configv1.DatabaseConfig{VectorEnabled: true, Schema: new("kagent_test"), VectorSchema: new("vectors_test"), Pool: &configv1.PostgresPoolConfig{
		MaxConns: new(int32(8)), MinConns: 1, MaxConnIdleTime: durationpb.New(time.Minute), MaxConnLifetime: durationpb.New(10 * time.Minute),
	}}
	pool := &pgxpool.Config{}
	config := postgresConfig(input, pool)
	if config.Pool != pool || !config.VectorEnabled {
		t.Fatalf("postgres config lost pool or vector setting: %#v", config)
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
	sources := migrationSources(&configv1.DatabaseConfig{Schema: new("kagent_test"), VectorSchema: new("extensions")}, extra)

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
	sources := migrationSources(&configv1.DatabaseConfig{VectorEnabled: true, Schema: new("tenant"), VectorSchema: new("vectors")}, nil)
	require.Len(t, sources, 2)
	for _, source := range sources {
		require.Equal(t, "tenant", source.Schema)
		require.Equal(t, "vectors", source.VectorSchema)
	}
}

// SetupLogger rejects a bad level before it touches the global logger, so this
// case does not disturb whatever logger the rest of the suite runs under.
func TestSetupLoggerRejectsBadLevel(t *testing.T) {
	require.Error(t, SetupLogger("not-a-level"))
}

func TestRunRejectsInvalidConfigBeforeIO(t *testing.T) {
	cfg := &configv1.ControllerConfig{}
	config.DefaultController(cfg)
	require.ErrorContains(t, Run(t.Context(), cfg, Options{}), "database.read_write.url_file")
	cfg.Database.ReadWrite = &configv1.DatabaseLoginConfig{UrlFile: "missing-secret", Role: "kagent_readwrite"}
	require.ErrorContains(t, Run(t.Context(), cfg, Options{}), "database.owner.url_file")
	cfg.Database.Owner = &configv1.DatabaseLoginConfig{UrlFile: "missing-secret", Role: "kagent_owner"}
	cfg.Push.SigningPrivateKeyFile = "missing-seed"
	cfg.Sessions.QuiescenceRecoveryInterval = durationpb.New(0)
	require.ErrorContains(t, Run(t.Context(), cfg, Options{}), "quiescence_recovery_interval")
}

func TestRunRejectsInvalidRuntimeTelemetryBeforeIO(t *testing.T) {
	for _, endpoint := range []string{"http://:4317", "https://%20"} {
		for _, test := range []struct {
			name      string
			telemetry *configv1.TelemetryConfig
		}{
			{name: "traces", telemetry: &configv1.TelemetryConfig{Traces: &configv1.OTLPConfig{Endpoint: endpoint}}},
			{name: "metrics", telemetry: &configv1.TelemetryConfig{Metrics: &configv1.OTLPConfig{Endpoint: endpoint}}},
			{name: "logs", telemetry: &configv1.TelemetryConfig{Logs: &configv1.OTLPConfig{Endpoint: endpoint}}},
		} {
			t.Run(test.name+"/"+endpoint, func(t *testing.T) {
				cfg := &configv1.ControllerConfig{
					Database: &configv1.DatabaseConfig{
						SkipMigrations: true,
						ReadWrite: &configv1.DatabaseLoginConfig{
							UrlFile: filepath.Join(t.TempDir(), "missing-secret"), Role: "kagent_readwrite",
						},
					},
					RuntimeDefaults: &configv1.RuntimeDefaultsConfig{Telemetry: test.telemetry},
				}
				require.EqualError(t, Run(t.Context(), cfg, Options{}),
					"runtimeDefaults: telemetry."+test.name+".endpoint must be an HTTP(S) URL")
			})
		}
	}
}

// With migrations skipped the owner login is never read, so Run gets past its
// configuration and fails only when the read/write login cannot connect.
func TestRunSkippingMigrationsNeedsNoOwner(t *testing.T) {
	urlFile := filepath.Join(t.TempDir(), "readwrite-url")
	require.NoError(t, os.WriteFile(urlFile, []byte("postgres://user@127.0.0.1:1/app?sslmode=disable&connect_timeout=1"), 0o600))
	cfg := &configv1.ControllerConfig{Database: &configv1.DatabaseConfig{
		SkipMigrations: true,
		ReadWrite:      &configv1.DatabaseLoginConfig{UrlFile: urlFile, Role: "kagent_readwrite"},
	}}
	config.DefaultController(cfg)
	require.ErrorContains(t, Run(t.Context(), cfg, Options{}), "verify database migrations:")
}

func TestRunRejectsNonpositivePushInterval(t *testing.T) {
	for _, input := range []time.Duration{0, -time.Minute} {
		t.Run(input.String(), func(t *testing.T) {
			cfg := &configv1.ControllerConfig{}
			config.DefaultController(cfg)
			cfg.Database.ReadWrite = &configv1.DatabaseLoginConfig{UrlFile: "missing-secret", Role: "kagent_readwrite"}
			cfg.Database.Owner = &configv1.DatabaseLoginConfig{UrlFile: "missing-owner", Role: "kagent_owner"}
			cfg.Push.RecoveryInterval = durationpb.New(input)
			require.ErrorContains(t, Run(t.Context(), cfg, Options{}), "recovery_interval")
		})
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
