package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/pkg/config"
)

func TestControllerConfig(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		invalid     bool
	}{
		{"defaults", "{}", false},
		{"actor JWT issuer", "substrate: {actorJwtIssuer: https://actors.example}", false},
		{"empty actor JWT issuer", "substrate: {actorJwtIssuer: ''}", true},
		{"HTTP actor JWT issuer", "substrate: {actorJwtIssuer: http://actors.example}", true},
		{"partial images", "harnessImages: {kagent: 'registry/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'}", false},
		{"empty image", "harnessImages: {codex: ''}", false},
		{"image tag", "harnessImages: {claude: 'registry/image:latest'}", true},
		{"invalid digest", "harnessImages: {codex: 'registry/image@sha256:bad'}", true},
		{"unknown image", "harnessImages: {unknown: ''}", true},
		{"images JSON string", "harnessImages: '{}'", true},
		{"duplicate", "logging:\n  level: info\n  level: debug", true},
		{"unknown", "loging: {}", true},
		{"duplicate JSON", `{"logging":{"level":"info","level":"debug"}}`, true},
		{"null list item", "kubernetes: {watchNamespaces: [null]}", true},
		{"negative capture limit", "runtimeDefaults: {telemetry: {maxCaptureBytes: -1}}", true},
		{"invalid telemetry endpoint", "runtimeDefaults: {telemetry: {traces: {endpoint: 'file:///private'}}}", true},
		{"null", "logging: null", false},
		{"type", "kubernetes: {leaderElection: 'false'}", true},
		{"alias", "logging: &log {level: info}\nruntimeDefaults: {logging: *log}", false},
		{"documents", "{}\n---\n{}", false},
		{"field aliases", "kubernetes: {leaderElection: true, leader_election: false}", true},
		{"pool bounds", "database: {pool: {minConns: 9, maxConns: 4}}", true},
		{"connect retry timeout", "database: {pool: {connectRetryTimeout: 90s}}", false},
		{"disabled connect retry", "database: {pool: {connectRetryTimeout: 0s}}", false},
		{"negative connect retry timeout", "database: {pool: {connectRetryTimeout: -1s}}", true},
		{"overlong connect retry timeout", "database: {pool: {connectRetryTimeout: 601s}}", true},
		{"database schemas", "database: {schema: tenant, vectorSchema: public}", false},
		{"invalid skip migrations type", "database: {skipMigrations: 'true'}", true},
		{"empty database schema", "database: {schema: ''}", true},
		{"invalid database schema", "database: {schema: 'tenant,public'}", true},
		{"invalid vector schema", "database: {vectorSchema: 'extension;DROP'}", true},
		{"ttl bounds", "sandboxes: {defaultTtl: 7200s, maxTtl: 3600s}", true},
		{"interval", "sessions: {expirationInterval: 0s}", true},
		{"old gateway push configuration", "gateway: {push: {allowHttp: true}}", true},
		{"push configuration", "push: {issuer: https://issuer.example, allowHttp: true, allowPrivateNetworks: true}", false},
		{"invalid push issuer", "push: {issuer: invalid}", true},
		{"push recovery interval", "push: {recoveryInterval: 15s}", false},
		{"zero push recovery interval", "push: {recoveryInterval: 0s}", true},
		{"negative push recovery interval", "push: {recoveryInterval: -60s}", true},
		{"push delivery settings", "push: {maxConcurrency: 4, sendTimeout: 60s}", false},
		{"serial push delivery", "push: {maxConcurrency: 1}", false},
		{"zero push concurrency", "push: {maxConcurrency: 0}", true},
		{"negative push concurrency", "push: {maxConcurrency: -1}", true},
		{"zero push timeout", "push: {sendTimeout: 0s}", true},
		{"negative push timeout", "push: {sendTimeout: -1s}", true},
		{"overflowing push timeout", "push: {sendTimeout: 9223372037s}", true},
		{"log level", "logging: {level: verbose}", true},
		{"runtime service name", "runtimeDefaults: {telemetry: {resourceAttributes: {service.name: shared}}}", true},
		{"runtime service namespace", "runtimeDefaults: {telemetry: {resourceAttributes: {service.namespace: shared}}}", true},
		{"runtime resource attributes", "runtimeDefaults: {telemetry: {resourceAttributes: {deployment.environment.name: production}}}", false},
		{"controller service labels", "telemetry: {resourceAttributes: {service.name: controller, service.namespace: control-plane}}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &configv1.ControllerConfig{}
			err := config.Decode([]byte(tc.input), cfg)
			if err == nil {
				config.DefaultController(cfg)
				setLogins(cfg)
				err = config.Validate(cfg)
			}
			if tc.invalid {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDatabaseSkipMigrations(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        bool
	}{
		{"default", "{}", false},
		{"apply", "database: {skipMigrations: false}", false},
		{"verify", "database: {skipMigrations: true}", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &configv1.ControllerConfig{}
			require.NoError(t, config.Decode([]byte(tc.input), cfg))
			config.DefaultController(cfg)
			setLogins(cfg)
			require.NoError(t, config.Validate(cfg))
			require.Equal(t, tc.want, cfg.GetDatabase().GetSkipMigrations())
		})
	}
}

func TestDatabaseConnectRetryTimeout(t *testing.T) {
	scenarios := []struct {
		name, input string
		want        time.Duration
	}{
		{"Given no connect retry timeout, when the controller config is defaulted, assert that logins are retried for 90s", "{}", 90 * time.Second},
		{"Given connectRetryTimeout 0s, when the controller config is defaulted, assert that the retry stays disabled", "database: {pool: {connectRetryTimeout: 0s}}", 0},
		{"Given connectRetryTimeout 90s, when the controller config is defaulted, assert that logins are retried for 90s", "database: {pool: {connectRetryTimeout: 90s}}", 90 * time.Second},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			cfg := &configv1.ControllerConfig{}
			require.NoError(t, config.Decode([]byte(scenario.input), cfg))

			config.DefaultController(cfg)

			setLogins(cfg)
			require.NoError(t, config.Validate(cfg))
			require.Equal(t, scenario.want, cfg.GetDatabase().GetPool().GetConnectRetryTimeout().AsDuration())
		})
	}
}

func TestDefaultingPreservesPresence(t *testing.T) {
	cfg := &configv1.ControllerConfig{}
	require.NoError(t, config.Decode([]byte("kubernetes: {leaderElection: false}\nsessions: {idleTtl: 0s}\ndatabase: {pool: {minConns: 0}}"), cfg))
	config.DefaultController(cfg)
	setLogins(cfg)
	require.NoError(t, config.Validate(cfg))
	require.False(t, cfg.Kubernetes.GetLeaderElection())
	require.Equal(t, time.Duration(0), cfg.Sessions.IdleTtl.AsDuration())
	require.Equal(t, int32(0), cfg.Database.Pool.GetMinConns())
	require.Equal(t, time.Minute, cfg.Sessions.ExpirationInterval.AsDuration())
	require.Equal(t, "kagent", cfg.Database.GetSchema())
	require.Equal(t, "extensions", cfg.Database.GetVectorSchema())
	require.Empty(t, cfg.Push.SigningPrivateKeyFile)
	require.Equal(t, time.Minute, cfg.Push.RecoveryInterval.AsDuration())
	require.EqualValues(t, 10, cfg.Push.GetMaxConcurrency())
	require.Equal(t, 5*time.Second, cfg.Push.SendTimeout.AsDuration())
}

func TestExplicitEmptyStringFailsSchemaValidation(t *testing.T) {
	cfg := &configv1.CLIConfig{}
	require.NoError(t, config.Decode([]byte("connection: {userId: ''}"), cfg))
	config.DefaultCLI(cfg)
	require.Error(t, config.Validate(cfg))
}

func setLogins(cfg *configv1.ControllerConfig) {
	cfg.Database.Owner = &configv1.DatabaseLoginConfig{UrlFile: "owner-url", Role: "kagent_owner"}
	cfg.Database.ReadWrite = &configv1.DatabaseLoginConfig{UrlFile: "readwrite-url", Role: "kagent_readwrite"}
}

func TestDatabaseLogins(t *testing.T) {
	for _, tc := range []struct {
		name, input, wantErr string
	}{
		{"both logins", "database: {owner: {urlFile: o, role: kagent_owner}, readWrite: {urlFile: rw, role: kagent_readwrite}}", ""},
		{"no read/write login", "database: {owner: {urlFile: o, role: kagent_owner}}", "database.read_write"},
		{"no read/write role", "database: {owner: {urlFile: o, role: kagent_owner}, readWrite: {urlFile: rw}}", "database.read_write"},
		{"no owner login", "database: {readWrite: {urlFile: rw, role: kagent_readwrite}}", "database.owner"},
		{"no owner role", "database: {owner: {urlFile: o}, readWrite: {urlFile: rw, role: kagent_readwrite}}", "database.owner"},
		{"no owner when skipping migrations", "database: {skipMigrations: true, readWrite: {urlFile: rw, role: kagent_readwrite}}", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &configv1.ControllerConfig{}
			require.NoError(t, config.Decode([]byte(tc.input), cfg))
			config.DefaultController(cfg)
			err := config.Validate(cfg)
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}

func TestDefaultingUsesProtoPresence(t *testing.T) {
	for _, tc := range []struct {
		name, input    string
		leaderElection bool
		idleTTL        time.Duration
		limit          *int64
	}{
		{name: "omitted", input: "{}", leaderElection: true, idleTTL: 7 * 24 * time.Hour},
		{name: "null", input: "kubernetes: {leaderElection: null}\nsessions: {idleTtl: null}\nruntimeDefaults: {maxContentLength: null}\ndatabase: {pool: {minConns: null}}", leaderElection: true, idleTTL: 7 * 24 * time.Hour},
		{name: "explicit zero", input: "kubernetes: {leaderElection: false}\nsessions: {idleTtl: 0s}\nruntimeDefaults: {maxContentLength: 0}\ndatabase: {pool: {minConns: 0}}", limit: new(int64(0))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &configv1.ControllerConfig{}
			require.NoError(t, config.Decode([]byte(tc.input), cfg))
			config.DefaultController(cfg)
			setLogins(cfg)
			require.NoError(t, config.Validate(cfg))
			require.Equal(t, tc.leaderElection, cfg.GetKubernetes().GetLeaderElection())
			require.Equal(t, tc.idleTTL, cfg.GetSessions().GetIdleTtl().AsDuration())
			require.Equal(t, tc.limit, cfg.GetRuntimeDefaults().MaxContentLength)
			require.Zero(t, cfg.GetDatabase().GetPool().GetMinConns())
			require.Zero(t, cfg.GetSessions().GetShareMaxTtl().AsDuration())
		})
	}
}

func TestYAMLConfigUsesLibraryScalarAndAliasSemantics(t *testing.T) {
	cfg := &configv1.ControllerConfig{}
	require.NoError(t, config.Decode([]byte(`
logging: &logging
  level: debug
runtimeDefaults:
  logging:
    <<: *logging
kubernetes:
  leaderElection: off
database:
  pool: {minConns: 0x10, maxConns: '32'}
telemetry:
  resourceAttributes: {123: 'off'}
`), cfg))
	config.DefaultController(cfg)
	setLogins(cfg)
	require.NoError(t, config.Validate(cfg))
	require.Equal(t, "debug", cfg.GetRuntimeDefaults().GetLogging().GetLevel())
	require.False(t, cfg.GetKubernetes().GetLeaderElection())
	require.Equal(t, int32(16), cfg.GetDatabase().GetPool().GetMinConns())
	require.Equal(t, int32(32), cfg.GetDatabase().GetPool().GetMaxConns())
	require.Equal(t, map[string]string{"123": "off"}, cfg.GetTelemetry().GetResourceAttributes())
}

func TestYAMLConfigReadsOnlyFirstDocument(t *testing.T) {
	cfg := &configv1.ControllerConfig{}
	require.NoError(t, config.Decode([]byte("logging: {level: debug}\n---\n["), cfg))
	require.Equal(t, "debug", cfg.GetLogging().GetLevel())
}

func TestSessionAutomaticRecovery(t *testing.T) {
	for _, scenario := range []struct {
		name, input string
		want        bool
	}{
		{"omitted", "{}", false},
		{"disabled", "sessions: {automaticRecovery: false}", false},
		{"enabled", "sessions: {automaticRecovery: true}", true},
	} {
		t.Run("Given controller config with automatic recovery "+scenario.name+", when it loads, assert that the setting is preserved", func(t *testing.T) {
			cfg := &configv1.ControllerConfig{}
			require.NoError(t, config.Decode([]byte(scenario.input), cfg))
			config.DefaultController(cfg)
			setLogins(cfg)
			require.NoError(t, config.Validate(cfg))
			require.Equal(t, scenario.want, cfg.GetSessions().GetAutomaticRecovery())
		})
	}
}

func TestWorkerPoolNamespacesOutsideWatch(t *testing.T) {
	for name, input := range map[string]string{
		"cluster-wide": "substrate: {workerPoolNamespaces: [ate-workers]}",
		"watched":      "kubernetes: {watchNamespaces: [team-a, ate-workers]}\nsubstrate: {workerPoolNamespaces: [ate-workers]}",
		"not watched":  "kubernetes: {watchNamespaces: [team-a]}\nsubstrate: {workerPoolNamespaces: [ate-workers]}",
	} {
		t.Run(name, func(t *testing.T) {
			cfg := &configv1.ControllerConfig{}
			require.NoError(t, config.Decode([]byte(input), cfg))
			config.DefaultController(cfg)
			setLogins(cfg)
			require.NoError(t, config.Validate(cfg))
		})
	}
}
