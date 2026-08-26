/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package app

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/gorilla/mux"

	"github.com/hashicorp/go-multierror"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	versionmetrics "github.com/kagent-dev/kagent/go/core/internal/metrics"
	"github.com/kagent-dev/kagent/go/core/internal/telemetry"

	"github.com/kagent-dev/kagent/go/core/internal/grpcserver"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver"
	agentservice "github.com/kagent-dev/kagent/go/core/internal/service/agent"
	agenttemplateservice "github.com/kagent-dev/kagent/go/core/internal/service/agenttemplate"
	feedbackservice "github.com/kagent-dev/kagent/go/core/internal/service/feedback"
	harnessservice "github.com/kagent-dev/kagent/go/core/internal/service/harness"
	memoryservice "github.com/kagent-dev/kagent/go/core/internal/service/memory"
	modelservice "github.com/kagent-dev/kagent/go/core/internal/service/model"
	prompttemplateservice "github.com/kagent-dev/kagent/go/core/internal/service/prompttemplate"
	sessionservice "github.com/kagent-dev/kagent/go/core/internal/service/session"
	systemservice "github.com/kagent-dev/kagent/go/core/internal/service/system"
	taskservice "github.com/kagent-dev/kagent/go/core/internal/service/task"
	toolservice "github.com/kagent-dev/kagent/go/core/internal/service/tool"
	common "github.com/kagent-dev/kagent/go/core/internal/utils"
	a2agateway "github.com/kagent-dev/kagent/go/core/v2/a2agateway"
	"github.com/kagent-dev/kagent/go/core/v2/agentinstance"
	v2controller "github.com/kagent-dev/kagent/go/core/v2/controller"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/translator"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	// +kubebuilder:scaffold:imports
)

var (
	scheme          = runtime.NewScheme()
	setupLog        = ctrl.Log.WithName("setup")
	kagentNamespace = common.GetResourceNamespace()

	// These variables should be set during build time using -ldflags
	Version   = version.Version
	GitCommit = version.GitCommit
	BuildDate = version.BuildDate
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(v1alpha3.AddToScheme(scheme))
	utilruntime.Must(v1alpha3.AddToScheme(scheme))
	utilruntime.Must(atev1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

type Config struct {
	Metrics struct {
		Addr     string
		CertPath string
		CertName string
		CertKey  string
	}
	Webhook struct {
		CertPath string
		CertName string
		CertKey  string
	}
	Proxy struct {
		URL string
	}
	Auth struct {
		Mode        string
		UserIDClaim string
	}
	LeaderElection     bool
	ProbeAddr          string
	SecureMetrics      bool
	EnableHTTP2        bool
	DefaultModelConfig types.NamespacedName
	HttpServerAddr     string
	WatchNamespaces    string
	A2ABaseUrl         string
	GRPC               struct {
		BindAddress     string
		MaxMessageBytes int
		Reflection      bool
		TLSCertFile     string
		TLSKeyFile      string
	}

	// MCPEgressPlaintext, when set, gates the egress URL rewrite: agent tool
	// URLs and the controller's tool-discovery dial that point at a
	// RemoteMCPServer are rewritten from https://host[:port] to
	// http://host:<port-or-443> so traffic egresses in plaintext to a proxy
	// that originates TLS upstream. Off by default;
	MCPEgressPlaintext bool
	Database           struct {
		Url                  string
		UrlFile              string
		VectorEnabled        bool
		SkipMigrations       bool
		MaxConns             int           // 0 = unset (pgx default)
		MinConns             int           // -1 = unset (pgx default); 0 is a valid value
		MaxConnIdleTime      time.Duration // 0 = unset (pgx default)
		MaxConnLifetime      time.Duration // 0 = unset (pgx default)
		SessionRetentionDays int           // 0 = disabled; sliding idle TTL on session.updated_at
	}
	Substrate struct {
		AteAPIEndpoint             string
		AteAPICAFile               string
		AteAPIClientCertFile       string
		AtenetRouterURL            string
		DialTimeout                time.Duration
		CallTimeout                time.Duration
		DefaultWorkerPoolNamespace string
		DefaultWorkerPoolName      string
	}
}

func (cfg *Config) SetFlags(commandLine *flag.FlagSet) {
	commandLine.StringVar(&cfg.Metrics.Addr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	commandLine.StringVar(&cfg.ProbeAddr, "health-probe-bind-address", ":8082", "The address the probe endpoint binds to.")
	commandLine.BoolVar(&cfg.LeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	commandLine.BoolVar(&cfg.SecureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	commandLine.StringVar(&cfg.Metrics.CertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	commandLine.StringVar(&cfg.Metrics.CertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	commandLine.StringVar(&cfg.Metrics.CertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	commandLine.StringVar(&cfg.Webhook.CertPath, "webhook-cert-path", "",
		"The directory that contains the webhook server certificate.")
	commandLine.StringVar(&cfg.Webhook.CertName, "webhook-cert-name", "tls.crt", "The name of the webhook server certificate file.")
	commandLine.StringVar(&cfg.Webhook.CertKey, "webhook-cert-key", "tls.key", "The name of the webhook server key file.")
	commandLine.BoolVar(&cfg.EnableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")

	commandLine.StringVar(&cfg.DefaultModelConfig.Name, "default-model-config-name", "default-model-config", "The name of the default model config.")
	commandLine.StringVar(&cfg.DefaultModelConfig.Namespace, "default-model-config-namespace", kagentNamespace, "The namespace of the default model config.")
	commandLine.StringVar(&cfg.HttpServerAddr, "http-server-address", ":8083", "The address the HTTP server binds to.")
	commandLine.StringVar(&cfg.GRPC.BindAddress, "grpc-bind-address", grpcserver.DefaultBindAddress, "The address the gRPC server binds to.")
	commandLine.IntVar(&cfg.GRPC.MaxMessageBytes, "grpc-max-message-bytes", grpcserver.DefaultMaxMessageSize, "Maximum gRPC request and response message size in bytes.")
	commandLine.BoolVar(&cfg.GRPC.Reflection, "grpc-reflection", false, "Enable gRPC server reflection.")
	commandLine.StringVar(&cfg.GRPC.TLSCertFile, "grpc-tls-cert-file", "", "Path to the optional gRPC server TLS certificate.")
	commandLine.StringVar(&cfg.GRPC.TLSKeyFile, "grpc-tls-key-file", "", "Path to the optional gRPC server TLS private key.")
	commandLine.StringVar(&cfg.A2ABaseUrl, "a2a-base-url", "http://127.0.0.1:8083", "The base URL of the A2A Server endpoint, as advertised to clients.")
	commandLine.StringVar(&cfg.Database.Url, "postgres-database-url", "postgres://postgres:kagent@kagent-postgresql.kagent.svc.cluster.local:5432/postgres", "The URL of the PostgreSQL database.")
	commandLine.StringVar(&cfg.Database.UrlFile, "postgres-database-url-file", "", "Path to a file containing the PostgreSQL database URL. Takes precedence over --postgres-database-url.")
	commandLine.BoolVar(&cfg.Database.VectorEnabled, "database-vector-enabled", true, "Enable pgvector extension and memory table. Requires pgvector to be installed on the PostgreSQL server.")
	commandLine.BoolVar(&cfg.Database.SkipMigrations, "skip-migrations", false, "Do not run database migrations at startup; instead verify the database is already migrated and fail if it is not. Migrations must be applied out-of-band (e.g. from a pipeline or pre-upgrade hook). Settable via the SKIP_MIGRATIONS env var.")
	commandLine.IntVar(&cfg.Database.MaxConns, "db-max-conns", 0, "Maximum number of connections in the Postgres pool. 0 leaves the pgx default.")
	commandLine.IntVar(&cfg.Database.MinConns, "db-min-conns", -1, "Minimum number of connections in the Postgres pool. -1 leaves the pgx default.")
	commandLine.DurationVar(&cfg.Database.MaxConnIdleTime, "db-max-conn-idle-time", 0, "Maximum idle time before a Postgres pool connection is closed. 0 leaves the pgx default (30m).")
	commandLine.DurationVar(&cfg.Database.MaxConnLifetime, "db-max-conn-lifetime", 0, "Maximum lifetime of a Postgres pool connection. 0 leaves the pgx default (1h).")
	commandLine.IntVar(&cfg.Database.SessionRetentionDays, "session-retention-days", 0, "Hard-delete idle sessions and cascaded conversation state (events, tasks, checkpoints, shares) when session.updated_at is older than this many days. 0 disables (default). Retention is a sliding window: activity refreshes updated_at.")

	commandLine.StringVar(&cfg.WatchNamespaces, "watch-namespaces", "", "The namespaces to watch for .")

	commandLine.StringVar(&cfg.Proxy.URL, "proxy-url", "", "Proxy URL for internally-built k8s URLs (e.g., http://proxy.kagent.svc.cluster.local:8080)")

	commandLine.StringVar(&cfg.Auth.Mode, "auth-mode", "unsecure", "Authentication mode: unsecure or trusted-proxy")
	commandLine.StringVar(&cfg.Auth.UserIDClaim, "auth-user-id-claim", "sub", "JWT claim name for user identity")

	commandLine.BoolVar(&cfg.MCPEgressPlaintext, "mcp-egress-plaintext", false,
		"When set, rewrite RemoteMCPServer tool URLs and the controller's tool-discovery dial from https://host[:port] to http://host:<port-or-443> so MCP traffic egresses in plaintext to a TLS-originating proxy. Off by default.")

	commandLine.StringVar(&cfg.Substrate.AteAPIEndpoint, "substrate-ate-api-endpoint", "", "gRPC target for Agent Substrate ate-api (e.g. dns:///api.ate-system.svc:443).")
	commandLine.StringVar(&cfg.Substrate.AteAPICAFile, "substrate-ate-api-ca-file", "", "Path to the CA certificates used to verify ate-api.")
	commandLine.StringVar(&cfg.Substrate.AteAPIClientCertFile, "substrate-ate-api-client-cert-file", "", "Path to the PEM client certificate and private key used for ate-api mTLS.")
	commandLine.StringVar(&cfg.Substrate.AtenetRouterURL, "substrate-atenet-router-url", "", "HTTP URL for Substrate atenet-router (Envoy). Defaults to http://atenet-router.ate-system.svc:80 when unset.")
	commandLine.DurationVar(&cfg.Substrate.DialTimeout, "substrate-dial-timeout", 10*time.Second, "Timeout for the initial dial to ate-api.")
	commandLine.DurationVar(&cfg.Substrate.CallTimeout, "substrate-call-timeout", 30*time.Second, "Per-RPC timeout for ate-api calls.")
	commandLine.StringVar(&cfg.Substrate.DefaultWorkerPoolNamespace, "substrate-default-workerpool-namespace", kagentNamespace, "Default Agent Substrate WorkerPool namespace when spec.substrate.workerPoolRef is unset.")
	commandLine.StringVar(&cfg.Substrate.DefaultWorkerPoolName, "substrate-default-workerpool-name", "", "Default Agent Substrate WorkerPool name when spec.substrate.workerPoolRef is unset.")
}

// postgresConfigFromApp builds a database.PostgresConfig from app flags.
// Zero/unset flag values leave the corresponding pool field nil so pgx defaults apply.
func postgresConfigFromApp(dbURL string, cfg *Config) *database.PostgresConfig {
	pgCfg := &database.PostgresConfig{
		URL:           dbURL,
		VectorEnabled: cfg.Database.VectorEnabled,
	}
	if cfg.Database.MaxConns > 0 {
		v := int32(cfg.Database.MaxConns)
		pgCfg.MaxConns = &v
	}
	if cfg.Database.MinConns >= 0 {
		v := int32(cfg.Database.MinConns)
		pgCfg.MinConns = &v
	}
	if cfg.Database.MaxConnIdleTime > 0 {
		v := cfg.Database.MaxConnIdleTime
		pgCfg.MaxConnIdleTime = &v
	}
	if cfg.Database.MaxConnLifetime > 0 {
		v := cfg.Database.MaxConnLifetime
		pgCfg.MaxConnLifetime = &v
	}
	return pgCfg
}

// LoadFromEnv loads configuration values from environment variables.
// Flag names are converted to uppercase with underscores (e.g., metrics-bind-address -> METRICS_BIND_ADDRESS).
func LoadFromEnv(fs *flag.FlagSet) error {
	var loadErr error

	fs.VisitAll(func(f *flag.Flag) {
		envName := strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_"))

		if envVal := os.Getenv(envName); envVal != "" {
			if err := f.Value.Set(envVal); err != nil {
				loadErr = multierror.Append(loadErr, fmt.Errorf("failed to set flag %s from env %s=%s: %w", f.Name, envName, envVal, err))
			}
		}
	})

	return loadErr
}

type BootstrapConfig struct {
	Ctx      context.Context
	Manager  manager.Manager
	Router   *mux.Router
	DbClient dbpkg.Client
	Config   *Config
}

type CtrlManagerConfigFunc func(manager.Manager) error

type ExtensionConfig struct {
	Authenticator    auth.AuthProvider
	Authorizer       auth.Authorizer
	MCPServerPlugins []translator.MCPTranslatorPlugin
	// A2AHandler serves the A2A API. grpcserver registers that service only
	// when this is non-nil, so leaving it unset keeps today's behaviour: the
	// AgentInstance API is available but nothing can talk to an instance.
	//
	// An extension can build one with a2agateway.New, using the DbClient it is
	// handed in BootstrapConfig.
	A2AHandler a2asrv.RequestHandler
}

type GetExtensionConfig func(bootstrap BootstrapConfig) (*ExtensionConfig, error)

// Start boots the controller. extraSources registers additional migration
// tracks beyond the built-in sources; they are applied after the built-in
// (core, vector) tracks, in the order given. Pass nil to run only the built-in
// migrations.
func Start(getExtensionConfig GetExtensionConfig, extraSources []migrations.Source) {
	var tlsOpts []func(*tls.Config)
	var cfg Config

	// Reused below for mgr.Start; SetupSignalHandler must be called once per process.
	ctx := ctrl.SetupSignalHandler()

	cfg.SetFlags(flag.CommandLine)

	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	// Load configuration from environment variables (overrides flags)
	if err := LoadFromEnv(flag.CommandLine); err != nil {
		setupLog.Error(err, "failed to load configuration from environment variables")
		os.Exit(1)
	}
	logger := zap.New(zap.UseFlagOptions(&opts))
	ctrl.SetLogger(logger)

	shutdownTracing, err := telemetry.InitTracerProvider(ctx, Version)
	if err != nil {
		setupLog.Error(err, "failed to initialize tracing")
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(shutdownCtx); err != nil {
			setupLog.Error(err, "failed to shutdown tracing")
		}
	}()

	setupLog.Info("Starting KAgent Controller", "version", Version, "git_commit", GitCommit, "build_date", BuildDate, "config", cfg)

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !cfg.EnableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Create watchers for metrics and webhooks certificates
	var metricsCertWatcher, webhookCertWatcher *certwatcher.CertWatcher

	ctrlmetrics.Registry.MustRegister(versionmetrics.NewBuildInfoCollector())

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.0/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   cfg.Metrics.Addr,
		SecureServing: cfg.SecureMetrics,
		TLSOpts:       tlsOpts,
	}

	if cfg.SecureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.0/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(cfg.Metrics.CertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", cfg.Metrics.CertPath, "metrics-cert-name", cfg.Metrics.CertName, "metrics-cert-key", cfg.Metrics.CertKey)

		var err error
		metricsCertWatcher, err = certwatcher.New(
			filepath.Join(cfg.Metrics.CertPath, cfg.Metrics.CertName),
			filepath.Join(cfg.Metrics.CertPath, cfg.Metrics.CertKey),
		)
		if err != nil {
			setupLog.Error(err, "to initialize metrics certificate watcher", "error", err)
			os.Exit(1)
		}

		metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, func(config *tls.Config) {
			config.GetCertificate = metricsCertWatcher.GetCertificate
		})
	}

	if len(cfg.Webhook.CertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", cfg.Webhook.CertPath, "webhook-cert-name", cfg.Webhook.CertName, "webhook-cert-key", cfg.Webhook.CertKey)

		var err error
		webhookCertWatcher, err = certwatcher.New(
			filepath.Join(cfg.Webhook.CertPath, cfg.Webhook.CertName),
			filepath.Join(cfg.Webhook.CertPath, cfg.Webhook.CertKey),
		)
		if err != nil {
			setupLog.Error(err, "to initialize webhook certificate watcher", "error", err)
			os.Exit(1)
		}
	}

	// filter out invalid namespaces from the watchNamespaces flag (comma separated list)
	watchNamespacesList := filterValidNamespaces(strings.Split(cfg.WatchNamespaces, ","))

	clientOpts := client.Options{}
	if len(watchNamespacesList) > 0 {
		// In namespaced RBAC mode a Role cannot grant access to cluster-scoped
		// lifecycle, so prevent the cached client from starting a cluster-scoped
		// Namespace informer whose list/watch would keep crashing.
		clientOpts.Cache = &client.CacheOptions{
			DisableFor: []client.Object{&corev1.Namespace{}},
		}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		HealthProbeBindAddress: cfg.ProbeAddr,
		LeaderElection:         cfg.LeaderElection,
		LeaderElectionID:       "0e9f6799.kagent.dev",
		Client:                 clientOpts,
		Cache: cache.Options{
			DefaultNamespaces: configureNamespaceWatching(watchNamespacesList),
		},
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}
	// Resolve the database URL once so both the migration runner and the pool
	// connection use exactly the same value.
	dbURL, err := database.ResolveURL(cfg.Database.Url, cfg.Database.UrlFile)
	if err != nil {
		setupLog.Error(err, "unable to resolve database URL")
		os.Exit(1)
	}

	// Run migrations before connecting; schema must exist before queries.
	// Built-in sources run first, then any downstream-registered extras.
	// With --skip-migrations (SKIP_MIGRATIONS) the server applies nothing and
	// instead verifies the database is already migrated, so migrations can run
	// out-of-band and this connection needs no DDL privileges.
	sources := append(migrations.BuiltinSources(cfg.Database.VectorEnabled), extraSources...)
	if cfg.Database.SkipMigrations {
		setupLog.Info("skipping database migrations; verifying schema is migrated")
		if err := migrations.VerifyMigrated(ctx, dbURL, sources); err != nil {
			setupLog.Error(err, "database migration verification failed")
			os.Exit(1)
		}
		setupLog.Info("database schema verified")
	} else {
		setupLog.Info("running database migrations")
		if err := migrations.RunUp(ctx, dbURL, sources); err != nil {
			setupLog.Error(err, "database migration failed")
			os.Exit(1)
		}
		setupLog.Info("database migrations complete")
	}

	// Connect to database
	db, err := database.Connect(ctx, postgresConfigFromApp(dbURL, &cfg))
	if err != nil {
		setupLog.Error(err, "unable to connect to database")
		os.Exit(1)
	}

	dbClient := database.NewClient(db)
	router := mux.NewRouter()
	extensionCfg, err := getExtensionConfig(BootstrapConfig{
		Ctx:      ctx,
		Manager:  mgr,
		Router:   router,
		DbClient: dbClient,
		Config:   &cfg,
	})
	if err != nil {
		setupLog.Error(err, "unable to get start config")
		os.Exit(1)
	}

	substrateAteClient, err := substrate.Dial(ctx, substrateAppConfig(&cfg))
	if err != nil {
		setupLog.Error(err, "unable to dial substrate ate-api for sandbox agents")
		os.Exit(1)
	}
	v2Runtime, err := v2controller.NewRuntime(mgr.GetConfig(), watchNamespacesList, ctx.Done())
	if err != nil {
		setupLog.Error(err, "unable to initialize v2 KRT runtime")
		os.Exit(1)
	}
	preparationReconciler, err := v2controller.NewReconciler(mgr.GetConfig(), v2Runtime.Collections, dbClient)
	if err != nil {
		setupLog.Error(err, "unable to initialize AgentTemplate preparation")
		os.Exit(1)
	}
	instanceWorkflow := agentinstance.NewActorWorkflow(dbClient, substrateAteClient)
	if err := mgr.Add(v2Runtime); err != nil {
		setupLog.Error(err, "unable to register v2 KRT runtime")
		os.Exit(1)
	}
	if err := mgr.Add(preparationReconciler); err != nil {
		setupLog.Error(err, "unable to register AgentTemplate preparation")
		os.Exit(1)
	}
	agentInstanceService := agentinstance.NewService(dbClient, extensionCfg.Authorizer, instanceWorkflow)

	atenetRouterURL := cfg.Substrate.AtenetRouterURL
	if atenetRouterURL == "" {
		atenetRouterURL = substrate.DefaultAtenetRouterURL
	}
	// Dials an instance's runtime through the atenet router, which is how the A2A
	// gateway reaches a private actor: the instance's authority is not routable
	// directly.
	a2aGatewayDialer, err := a2agateway.NewRuntimeDialer(atenetRouterURL, extensionCfg.Authenticator)
	if err != nil {
		setupLog.Error(err, "unable to create A2A runtime dialer")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder
	if metricsCertWatcher != nil {
		setupLog.Info("Adding metrics certificate watcher to manager")
		if err := mgr.Add(metricsCertWatcher); err != nil {
			setupLog.Error(err, "unable to add metrics certificate watcher to manager")
			os.Exit(1)
		}
	}

	if webhookCertWatcher != nil {
		setupLog.Info("Adding webhook certificate watcher to manager")
		if err := mgr.Add(webhookCertWatcher); err != nil {
			setupLog.Error(err, "unable to add webhook certificate watcher to manager")
			os.Exit(1)
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	if err := mgr.Add(&adminServer{port: ":6060"}); err != nil {
		setupLog.Error(err, "unable to set up admin server")
		os.Exit(1)
	}

	modelConfigService := modelservice.NewService(
		mgr.GetClient(),
		extensionCfg.Authorizer,
		common.GetResourceNamespace(),
	)
	agentService := agentservice.NewService(
		mgr.GetClient(),
		extensionCfg.Authorizer,
		cfg.DefaultModelConfig.Namespace,
	)
	toolService := toolservice.NewService(
		mgr.GetClient(),
		dbClient,
		extensionCfg.Authorizer,
		common.GetResourceNamespace(),
		nil,
	)
	promptTemplateService := prompttemplateservice.NewService(mgr.GetClient(), extensionCfg.Authorizer)
	systemService := systemservice.NewService(systemservice.WithInventory(
		mgr.GetClient(),
		watchNamespacesList,
		extensionCfg.Authorizer,
		substrateAteClient,
	))
	feedbackService := feedbackservice.NewService(dbClient)
	memoryService := memoryservice.NewService(dbClient)
	sessionService := sessionservice.NewService(dbClient)
	taskService := taskservice.NewService(dbClient)
	agentTemplateService := agenttemplateservice.NewService(mgr.GetClient(), extensionCfg.Authorizer)
	harnessService := harnessservice.NewService(mgr.GetClient(), extensionCfg.Authorizer)

	// A2A over gRPC, routed to an AgentInstance: the gateway reads the
	// `x-kagent-agent-instance-{namespace,id}` metadata and dials that instance's
	// own `a2a_authority` through the atenet router.
	//
	// An extension may supply its own; absent one this controller serves the
	// gateway itself, so a browser that can list and create agents over gRPC-Web
	// also has somewhere to send a message. The workflow is what lets the gateway
	// suspend an instance when a turn completes — the same one the lifecycle RPCs
	// use, rather than a second over the same client.
	a2aHandler := extensionCfg.A2AHandler
	if a2aHandler == nil {
		a2aHandler = a2agateway.New(dbClient, extensionCfg.Authorizer, a2aGatewayDialer,
			instanceWorkflow, cfg.A2ABaseUrl)
	}

	grpcServer, err := grpcserver.New(grpcserver.Config{
		BindAddress:           cfg.GRPC.BindAddress,
		MaxMessageBytes:       cfg.GRPC.MaxMessageBytes,
		Reflection:            cfg.GRPC.Reflection,
		TLSCertFile:           cfg.GRPC.TLSCertFile,
		TLSKeyFile:            cfg.GRPC.TLSKeyFile,
		Authenticator:         extensionCfg.Authenticator,
		ShareStore:            dbClient,
		Registerer:            ctrlmetrics.Registry,
		AgentService:          agentService,
		ModelService:          modelConfigService,
		ToolService:           toolService,
		AgentTemplateService:  agentTemplateService,
		HarnessService:        harnessService,
		PromptTemplateService: promptTemplateService,
		SystemService:         systemService,
		FeedbackService:       feedbackService,
		MemoryService:         memoryService,
		SessionService:        sessionService,
		TaskService:           taskService,
		AgentInstanceService:  agentInstanceService,
		A2AHandler:            a2aHandler,
	})
	if err != nil {
		setupLog.Error(err, "unable to create gRPC server")
		os.Exit(1)
	}
	if err := mgr.Add(grpcServer); err != nil {
		setupLog.Error(err, "unable to set up gRPC server")
		os.Exit(1)
	}

	httpServer, err := httpserver.NewHTTPServer(httpserver.ServerConfig{
		Router:        router,
		BindAddr:      cfg.HttpServerAddr,
		KubeClient:    mgr.GetClient(),
		DbClient:      dbClient,
		Authenticator: extensionCfg.Authenticator,
		// Lets a browser reach the gRPC services over the same origin the app is
		// served from; see grpcserver.WebHandler. Built after the gRPC server
		// because it is that server's own rule about which requests are its.
		GrpcWebRouter: grpcServer.WebHandlerOr,
	})
	if err != nil {
		setupLog.Error(err, "unable to create HTTP server")
		os.Exit(1)
	}
	if err := mgr.Add(httpServer); err != nil {
		setupLog.Error(err, "unable to set up HTTP server")
		os.Exit(1)
	}

	// DB TTL cleanup (memory + sessions) runs only on the leader to avoid duplicate deletes.
	// Currently configured to run every 24 hours.
	if err := mgr.Add(httpserver.NewDbCleanupRunnable(dbClient, 24*time.Hour, cfg.Database.SessionRetentionDays)); err != nil {
		setupLog.Error(err, "unable to set up DB cleanup runnable")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func substrateAppConfig(cfg *Config) substrate.Config {
	sc := substrate.Config{
		AteAPIEndpoint: cfg.Substrate.AteAPIEndpoint,
		CAFile:         cfg.Substrate.AteAPICAFile,
		ClientCertFile: cfg.Substrate.AteAPIClientCertFile,
		DialTimeout:    cfg.Substrate.DialTimeout,
		CallTimeout:    cfg.Substrate.CallTimeout,
	}
	return sc
}

// configureNamespaceWatching sets up the controller manager to watch specific namespaces
// based on the provided configuration. It returns the list of namespaces being watched,
// or nil if watching all namespaces.
func configureNamespaceWatching(watchNamespacesList []string) map[string]cache.Config {
	if len(watchNamespacesList) == 0 {
		setupLog.Info("Watching all namespaces (no valid namespaces specified)")
		return map[string]cache.Config{"": {}}
	}
	setupLog.Info("Watching specific namespaces at cache level", "namespaces", watchNamespacesList)

	namespacesMap := make(map[string]cache.Config)
	for _, ns := range watchNamespacesList {
		namespacesMap[ns] = cache.Config{}
	}

	return namespacesMap
}

// filterValidNamespaces removes invalid namespace names from the provided list.
// A valid namespace must be a valid DNS-1123 label.
func filterValidNamespaces(namespaces []string) []string {
	var validNamespaces []string

	for _, ns := range namespaces {
		if strings.TrimSpace(ns) == "" {
			continue
		}

		if errs := validation.IsDNS1123Label(ns); len(errs) > 0 {
			setupLog.Info("Ignoring invalid namespace name",
				"namespace", ns,
				"validation_errors", strings.Join(errs, ", "))
		} else {
			validNamespaces = append(validNamespaces, ns)
		}
	}

	return validNamespaces
}

var _ manager.Runnable = &adminServer{}

type adminServer struct {
	port string
}

func (a *adminServer) Start(ctx context.Context) error {
	setupLog.Info("starting pprof server")
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.HandleFunc("/debug/pprof/goroutine", pprof.Handler("goroutine").ServeHTTP)
	mux.HandleFunc("/debug/pprof/heap", pprof.Handler("heap").ServeHTTP)
	mux.HandleFunc("/debug/pprof/block", pprof.Handler("block").ServeHTTP)
	mux.HandleFunc("/debug/pprof/threadcreate", pprof.Handler("threadcreate").ServeHTTP)
	mux.HandleFunc("/debug/pprof/mutex", pprof.Handler("mutex").ServeHTTP)
	mux.HandleFunc("/debug/pprof/allocs", pprof.Handler("allocs").ServeHTTP)
	setupLog.Info("pprof server started", "address", a.port)
	return http.ListenAndServe(a.port, mux)
}
