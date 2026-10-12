package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	apiauthorization "github.com/kagent-dev/kagent/go/api/authorization"
	"github.com/kagent-dev/kagent/go/core/internal/grpcserver"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
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

func TestLeaderElectionConfiguration(t *testing.T) {
	t.Setenv("KAGENT_LEADER_ELECT", "")
	if !kagentenv.LeaderElect.Get() {
		t.Fatal("leader election must default to enabled")
	}
	t.Setenv("KAGENT_LEADER_ELECT", "false")
	if kagentenv.LeaderElect.Get() {
		t.Fatal("an explicit false must be preserved for startup validation")
	}
}

func TestMetricsBindAddressNeverFallsBackToTheControllerRuntimeDefault(t *testing.T) {
	set := func(value string) *string { return &value }
	for name, testCase := range map[string]struct {
		value *string
		want  string
	}{
		"unset":    {value: nil, want: "0"},
		"empty":    {value: set(""), want: "0"},
		"disabled": {value: set("0"), want: "0"},
		"port":     {value: set(":8443"), want: ":8443"},
	} {
		t.Run(name, func(t *testing.T) {
			if testCase.value != nil {
				t.Setenv("KAGENT_METRICS_BIND_ADDRESS", *testCase.value)
			}
			if got := metricsBindAddress(); got != testCase.want {
				t.Fatalf("metricsBindAddress() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestMetricsSecureDefaultsOff(t *testing.T) {
	if kagentenv.MetricsSecure.Get() {
		t.Fatal("KAGENT_METRICS_SECURE must default to false")
	}
	t.Setenv("KAGENT_METRICS_SECURE", "true")
	if !kagentenv.MetricsSecure.Get() {
		t.Fatal("KAGENT_METRICS_SECURE=true must enable secure serving")
	}
}

func TestNamespaces(t *testing.T) {
	want := []string{"one", "two"}
	if got := namespaces(" one, ,two,"); !reflect.DeepEqual(got, want) {
		t.Fatalf("namespaces() = %q, want %q", got, want)
	}
}

func TestRegisteredStringDefaults(t *testing.T) {
	for _, variable := range []kagentenv.StringVar{
		kagentenv.HTTPBindAddress,
		kagentenv.SubstrateATEAPIEndpoint,
		kagentenv.KagentNamespace,
	} {
		t.Run(variable.Name(), func(t *testing.T) {
			for _, test := range []struct {
				name  string
				value *string
				want  string
			}{
				{name: "unset", want: variable.DefaultValue()},
				{name: "empty", value: new(""), want: variable.DefaultValue()},
				{name: "override", value: new("override"), want: "override"},
			} {
				t.Run(test.name, func(t *testing.T) {
					t.Setenv(variable.Name(), "")
					if test.value == nil {
						if err := os.Unsetenv(variable.Name()); err != nil {
							t.Fatal(err)
						}
					} else {
						t.Setenv(variable.Name(), *test.value)
					}
					if got := env(variable); got != test.want {
						t.Errorf("env() = %q, want %q", got, test.want)
					}
				})
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

func TestPostgresConfigFromEnv(t *testing.T) {
	t.Setenv("KAGENT_POSTGRES_DATABASE_MAX_CONNS", "8")
	t.Setenv("KAGENT_POSTGRES_DATABASE_MIN_CONNS", "1")
	t.Setenv("KAGENT_POSTGRES_DATABASE_MAX_CONN_IDLE_TIME", "1m")
	t.Setenv("KAGENT_POSTGRES_DATABASE_MAX_CONN_LIFETIME", "10m")
	t.Setenv("KAGENT_POSTGRES_DATABASE_SCHEMA", "kagent_test")

	pool := &pgxpool.Config{}
	config := postgresConfigFromEnv(pool, true)
	if config.Pool != pool || !config.VectorEnabled {
		t.Fatalf("postgres config lost pool or vector setting: %#v", config)
	}
	if config.Schema != "kagent_test" {
		t.Fatalf("Schema = %q, want kagent_test", config.Schema)
	}
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
	sources := migrationSources(false, extra)

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
	t.Setenv("KAGENT_LOG_LEVEL", "not-a-level")
	if err := SetupLogger(); err == nil {
		t.Fatal("SetupLogger accepted an unparseable KAGENT_LOG_LEVEL")
	}
}

func TestRunRequiresDatabaseLogins(t *testing.T) {
	const url = "postgres://user@database/app"
	for _, tt := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "read/write URL", env: map[string]string{}, want: "KAGENT_POSTGRES_READWRITE_DATABASE_URL is required"},
		{name: "read/write role", env: map[string]string{"KAGENT_POSTGRES_READWRITE_DATABASE_URL": url}, want: "KAGENT_POSTGRES_READWRITE_ROLE is required"},
		{name: "owner URL", env: map[string]string{"KAGENT_POSTGRES_READWRITE_DATABASE_URL": url, "KAGENT_POSTGRES_READWRITE_ROLE": "kagent_readwrite"}, want: "KAGENT_POSTGRES_OWNER_DATABASE_URL is required"},
		{name: "owner role", env: map[string]string{"KAGENT_POSTGRES_READWRITE_DATABASE_URL": url, "KAGENT_POSTGRES_READWRITE_ROLE": "kagent_readwrite", "KAGENT_POSTGRES_OWNER_DATABASE_URL": url}, want: "KAGENT_POSTGRES_OWNER_ROLE is required"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("KAGENT_LOG_LEVEL", "info")
			for _, name := range []string{"KAGENT_POSTGRES_READWRITE_DATABASE_URL", "KAGENT_POSTGRES_READWRITE_ROLE", "KAGENT_POSTGRES_OWNER_DATABASE_URL", "KAGENT_POSTGRES_OWNER_ROLE", "KAGENT_SKIP_MIGRATIONS"} {
				t.Setenv(name, tt.env[name])
			}
			if err := Run(t.Context(), Options{}); err == nil || err.Error() != tt.want {
				t.Fatalf("Run() error = %v, want %q", err, tt.want)
			}
		})
	}
}

// With migrations skipped the owner login is never used, so Run must get past
// validation without it and fail only when the read/write login cannot connect.
func TestRunSkippingMigrationsNeedsNoOwner(t *testing.T) {
	t.Setenv("KAGENT_LOG_LEVEL", "info")
	t.Setenv("KAGENT_SKIP_MIGRATIONS", "true")
	t.Setenv("KAGENT_POSTGRES_READWRITE_DATABASE_URL", "postgres://user@127.0.0.1:1/app?sslmode=disable&connect_timeout=1")
	t.Setenv("KAGENT_POSTGRES_READWRITE_ROLE", "kagent_readwrite")
	t.Setenv("KAGENT_POSTGRES_OWNER_DATABASE_URL", "")
	t.Setenv("KAGENT_POSTGRES_OWNER_ROLE", "")
	err := Run(t.Context(), Options{})
	if err == nil || !strings.HasPrefix(err.Error(), "verify database migrations:") {
		t.Fatalf("Run() error = %v, want a read/write connection failure from verification", err)
	}
}

func TestRunRejectsNonpositiveQuiescenceInterval(t *testing.T) {
	t.Setenv("KAGENT_LOG_LEVEL", "info")
	for _, input := range []string{"0s", "-1m"} {
		t.Run(input, func(t *testing.T) {
			t.Setenv(kagentenv.SessionQuiescencePollInterval.Name(), input)
			err := Run(t.Context(), Options{})
			if want := "KAGENT_SESSION_QUIESCENCE_POLL_INTERVAL must be positive"; err == nil || err.Error() != want {
				t.Fatalf("Run() error = %v, want %q", err, want)
			}
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
