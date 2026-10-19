// Package app boots the kagent controller.
//
// It exists so that the controller can be started by something other than this
// repository's own main package. The only parts of it a library consumer may
// replace are authentication and authorization; everything else — the database,
// the controller manager, the Substrate connection, the A2A gateway and the gRPC
// server — is core's to build. Those are not policy decisions, and a component
// that reaches agent runtimes should not take whatever handler a library
// consumer happens to assemble for it.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	atev1alpha1 "go.ate.dev/substrate/pkg/api/v1alpha1"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/a2agateway"
	"github.com/kagent-dev/kagent/go/core/internal/controller"
	remotemcpcontroller "github.com/kagent-dev/kagent/go/core/internal/controller/remotemcpserver"
	scheduledruncontroller "github.com/kagent-dev/kagent/go/core/internal/controller/scheduledrun"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/grpcserver"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/mcp"
	"github.com/kagent-dev/kagent/go/core/internal/service/checkpoint"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	memoryservice "github.com/kagent-dev/kagent/go/core/internal/service/memory"
	modelservice "github.com/kagent-dev/kagent/go/core/internal/service/model"
	prompttemplateservice "github.com/kagent-dev/kagent/go/core/internal/service/prompttemplate"
	sandboxservice "github.com/kagent-dev/kagent/go/core/internal/service/sandbox"
	"github.com/kagent-dev/kagent/go/core/internal/service/scheduledrun"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	systemservice "github.com/kagent-dev/kagent/go/core/internal/service/system"
	"github.com/kagent-dev/kagent/go/core/internal/service/taskstore"
	toolservice "github.com/kagent-dev/kagent/go/core/internal/service/tool"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/config"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
	"github.com/kagent-dev/kagent/go/core/pkg/postgres"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/kagent-dev/kagent/go/pkg/telemetry"
)

// Options are the components a library consumer may supply in place of core's own.
//
// A nil field is not an error: it selects the library default. The shipped
// controller selects its authenticator from server.auth settings. Supplying
// one does not change how core uses it — the authenticator still guards the
// gRPC server and the /mcp endpoint, and the authorizer is still consulted by
// every service that takes one — so a library consumer cannot narrow where
// its own policy applies.
type Options struct {
	// Authenticator identifies the caller. Nil selects InsecureAuthenticator,
	// which admits every request.
	Authenticator auth.AuthProvider
	// Authorizer decides what an identified caller may do and which collection
	// entries it may see. Nil selects NoopAuthorizer, which permits every action.
	Authorizer auth.CollectionAuthorizer
	// SetupWithManager registers additional controllers and scheme types on
	// core's manager. It runs after the manager exists and before it starts, so
	// a scheme added here is in place before any cache is built. Returning an
	// error aborts startup.
	//
	// This exists so that a library consumer does not have to run a second manager
	// alongside core's: two managers means two caches of the same objects and a
	// second leader election to keep consistent with the first.
	SetupWithManager func(manager.Manager) error
	// ExtraMigrations are applied after the built-in tracks, in the order
	// given. A library consumer that owns tables uses this rather than migrating
	// separately, so that one run leaves the database wholly at one version.
	// Each source must set Schema.
	ExtraMigrations []migrations.Source
	// GRPCServices registers additional services on core's gRPC server, so a
	// consumer's API shares core's transport, authenticator and interceptors
	// instead of standing up a second server on another port.
	//
	// Every method it registers needs an entry in MethodPolicies: the
	// authentication interceptor refuses a method it has no policy for, so a
	// service registered without one is closed rather than open.
	GRPCServices func(grpc.ServiceRegistrar)
	// MethodPolicies declares access for the methods GRPCServices registers,
	// keyed by full method name.
	//
	// Run fails if a key names one of core's own methods. A consumer describes
	// its own surface here; letting it reclassify core's would turn a config
	// field into a way to make an authenticated method public.
	MethodPolicies map[string]auth.AccessMode
}

// resolve substitutes core's defaults for whichever components the caller left
// nil. It never returns a nil component, so callers do not have to check.
func (o Options) resolve() (auth.AuthProvider, auth.CollectionAuthorizer) {
	authenticator := o.Authenticator
	if authenticator == nil {
		authenticator = &authimpl.InsecureAuthenticator{}
	}
	authorizer := o.Authorizer
	if authorizer == nil {
		authorizer = &auth.NoopAuthorizer{}
	}
	return authenticator, authorizer
}

// SetupLogger installs the explicitly configured controller logger.
func SetupLogger(level string) error {
	logger, err := logging.New(os.Stderr, level)
	if err != nil {
		return fmt.Errorf("invalid logging level: %w", err)
	}
	slog.SetDefault(logger)
	ctrl.SetLogger(logging.AsLogr(logger))
	return nil
}

// Run boots the controller and blocks until ctx is cancelled or a component
// fails. It returns the first error rather than exiting, so a library consumer
// keeps control of how the process ends.
func Run(ctx context.Context, input *configv1.ControllerConfig, opts Options) error {
	if input == nil {
		return fmt.Errorf("controller configuration is required")
	}
	cfg := proto.Clone(input).(*configv1.ControllerConfig)
	config.DefaultController(cfg)
	if err := config.Validate(cfg); err != nil {
		return err
	}
	if err := config.Telemetry(cfg.GetRuntimeDefaults().GetTelemetry()).Validate(); err != nil {
		return fmt.Errorf("runtimeDefaults: %w", err)
	}
	// The read/write login serves every runtime query and verifies migrations
	// when they are skipped. The owner login only creates the schema and migrates.
	readWriteConfig, err := databasePoolConfig("database.readWrite", cfg.GetDatabase().GetReadWrite())
	if err != nil {
		return err
	}
	var ownerConfig *pgxpool.Config
	if !cfg.GetDatabase().GetSkipMigrations() {
		if ownerConfig, err = databasePoolConfig("database.owner", cfg.GetDatabase().GetOwner()); err != nil {
			return err
		}
	}
	var pushPrivateKey string
	if cfg.GetPush().GetSigningPrivateKeyFile() != "" {
		pushPrivateKey, err = config.ReadSecret(cfg.GetPush().GetSigningPrivateKeyFile())
		if err != nil {
			return fmt.Errorf("push.signingPrivateKeyFile: %w", err)
		}
	}
	if err := SetupLogger(cfg.GetLogging().GetLevel()); err != nil {
		return err
	}
	quiescenceInterval := cfg.GetSessions().GetQuiescenceRecoveryInterval().AsDuration()
	logger := slog.Default()
	ctx = logging.IntoContext(ctx, logger)
	if err := validateHarnessImagesRelease(cfg.GetHarnessImages(), version.Version); err != nil {
		return err
	}
	telemetryOptions := telemetry.Options{Config: config.Telemetry(cfg.GetTelemetry()), Defaults: []attribute.KeyValue{
		semconv.ServiceName("kagent-controller"), semconv.ServiceNamespace("kagent"), semconv.ServiceVersion(version.Version),
	}}
	if cfg.GetMetrics().GetBindAddress() != "0" {
		reader, err := otelprometheus.New(otelprometheus.WithRegisterer(crmetrics.Registry))
		if err != nil {
			return fmt.Errorf("create Prometheus metric reader: %w", err)
		}
		telemetryOptions.MetricReaders = []sdkmetric.Reader{reader}
	}
	// otelgrpc snapshots the global providers and propagator when its handler is
	// constructed, so telemetry has to be registered before any server is built.
	providers, err := telemetry.Init(ctx, telemetryOptions)
	if err != nil {
		logger.ErrorContext(ctx, "failed to initialize telemetry", "error", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := providers.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(shutdownCtx, "failed to shut down telemetry", "error", err)
		}
	}()

	sources := migrationSources(cfg.GetDatabase(), opts.ExtraMigrations)
	if cfg.GetDatabase().GetSkipMigrations() {
		if err := migrations.VerifyMigrated(ctx, readWriteConfig, sources); err != nil {
			return fmt.Errorf("verify database migrations: %w", err)
		}
	} else if err := migrations.RunUp(ctx, ownerConfig, sources); err != nil {
		return fmt.Errorf("run database migrations: %w", err)
	}
	db, err := database.Connect(ctx, postgresConfig(cfg.GetDatabase(), readWriteConfig))
	if err != nil {
		return err
	}
	defer db.Close()
	store := database.NewClient(db, cfg.GetDatabase().GetVectorSchema())

	kubeConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return fmt.Errorf("load Kubernetes config: %w", err)
	}
	// The manager's client is what serves the Harness and AgentTemplate RPCs, so
	// it needs v1alpha3 in its scheme; the controller-runtime default carries
	// only the built-in kinds and would fail every one of those calls at runtime
	// rather than at startup.
	managerScheme := k8sruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(managerScheme))
	utilruntime.Must(kagentv1alpha3.AddToScheme(managerScheme))
	utilruntime.Must(atev1alpha1.AddToScheme(managerScheme))
	watchNamespaces := cfg.GetKubernetes().GetWatchNamespaces()
	managerClientOptions := client.Options{}
	managerCacheOptions := cache.Options{DefaultNamespaces: namespaceCache(watchNamespaces)}
	if len(watchNamespaces) > 0 {
		// A namespaced Role cannot list cluster-scoped Namespace objects. Read them
		// directly so SystemService can fall back to the configured names on a
		// Forbidden response without a failing Namespace informer blocking startup.
		managerClientOptions.Cache = &client.CacheOptions{DisableFor: []client.Object{&corev1.Namespace{}}}
	}
	metricsOptions := metricsserver.Options{
		BindAddress:   cfg.GetMetrics().GetBindAddress(),
		SecureServing: cfg.GetMetrics().GetSecure(),
	}
	if metricsOptions.SecureServing {
		// SecureServing alone only encrypts. The filter authenticates the scraper
		// with a TokenReview and authorizes it with a SubjectAccessReview on the
		// /metrics nonResourceURL.
		metricsOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}
	manager, err := ctrl.NewManager(kubeConfig, ctrl.Options{
		Scheme:                  managerScheme,
		Cache:                   managerCacheOptions,
		Client:                  managerClientOptions,
		Metrics:                 metricsOptions,
		LeaderElection:          cfg.GetKubernetes().GetLeaderElection(),
		LeaderElectionID:        "0e9f6799.kagent.dev",
		LeaderElectionNamespace: cfg.GetKubernetes().GetNamespace(),
	})
	if err != nil {
		return fmt.Errorf("create controller manager: %w", err)
	}
	runtime, err := controller.NewRuntime(kubeConfig, watchNamespaces, ctx.Done(), cfg.GetRuntimeDefaults(), cfg.GetHarnessImages())
	if err != nil {
		return err
	}
	actors, err := substrate.Dial(ctx, substrate.Config{
		EgressPolicies: store,
		AteAPIEndpoint: cfg.GetSubstrate().GetApiEndpoint(),
		CAFile:         cfg.GetSubstrate().GetCaFile(),
		ClientCertFile: cfg.GetSubstrate().GetClientCertFile(),
		CallTimeout:    30 * time.Second,
	})
	if err != nil {
		return err
	}
	defer actors.Close()
	reconciler, err := controller.NewReconciler(kubeConfig, runtime.Collections, store, actors)
	if err != nil {
		return err
	}
	if err := manager.Add(reconciler); err != nil {
		return fmt.Errorf("add reconciler to controller manager: %w", err)
	}
	runtimeGC, err := controller.NewRuntimeRevisionGC(store, actors, cfg.GetRuntimeRevisions().GetGcInterval().AsDuration(), otel.GetMeterProvider())
	if err != nil {
		return fmt.Errorf("create runtime revision GC: %w", err)
	}
	if err := manager.Add(runtimeGC); err != nil {
		return fmt.Errorf("add runtime revision GC to controller manager: %w", err)
	}
	if opts.SetupWithManager != nil {
		if err := opts.SetupWithManager(manager); err != nil {
			return fmt.Errorf("set up library consumer controllers: %w", err)
		}
	}
	mcpClient := toolservice.NewRuntimeMCPClient(manager.GetClient())
	remoteMCPDiscovery := remotemcpcontroller.New(manager.GetClient(), mcpClient, store)
	if err := remoteMCPDiscovery.SetupWithManager(manager); err != nil {
		return fmt.Errorf("set up RemoteMCPServer discovery: %w", err)
	}

	authenticator, authorizer := opts.resolve()
	resourceNamespace := cfg.GetKubernetes().GetNamespace()
	models := modelservice.NewService(manager.GetClient(), authorizer, resourceNamespace)
	tools := toolservice.NewService(manager.GetClient(), store, authorizer, resourceNamespace, mcpClient)
	prompts := prompttemplateservice.NewService(manager.GetClient(), authorizer)
	system := systemservice.NewService(manager.GetClient(), watchNamespaces, authorizer, actors)
	memory := memoryservice.NewService(store)
	quiescenceWake := make(chan struct{}, 1)
	pushWake := make(chan struct{}, 1)
	sessionWorkflow := sessionsvc.NewActorWorkflow(store, actors, quiescenceWake, quiescenceInterval)
	runtimeTasks := taskstore.NewService(store, quiescenceWake, pushWake)
	if err := manager.Add(sessionWorkflow); err != nil {
		return fmt.Errorf("register idle session worker: %w", err)
	}
	expiration, err := sessionsvc.NewExpirationWorker(store, sessionWorkflow, cfg.GetSessions().GetIdleTtl().AsDuration(), cfg.GetSessions().GetExpirationInterval().AsDuration())
	if err != nil {
		return err
	}
	if err := manager.Add(expiration); err != nil {
		return fmt.Errorf("register session expiration worker: %w", err)
	}
	shareMaxTTL := cfg.GetSessions().GetShareMaxTtl().AsDuration()
	sessions := sessionsvc.NewService(store, authorizer, sessionWorkflow, sessionsvc.WithShareMaxTTL(shareMaxTTL))
	checkpoints := checkpoint.NewService(store, authorizer, actors, sessionWorkflow)
	gatewayDialer, err := a2agateway.NewRuntimeDialer(
		cfg.GetSubstrate().GetRouterUrl(),
		authenticator,
	)
	if err != nil {
		return err
	}
	agents := kubecrud.NewService(manager.GetClient(), authorizer, &kagentv1alpha3.Agent{}, &kagentv1alpha3.AgentList{}, "Agent")
	interactions := sessionsvc.NewInteractionService(store, agents, sessions, cfg.GetPush().GetAllowHttp())
	pushSender := sessionsvc.NewHTTPPushSender(5*time.Second,
		cfg.GetPush().GetAllowHttp(), cfg.GetPush().GetAllowPrivateNetworks())
	pushIssuer := cfg.GetPush().GetIssuer()
	pushSigner, err := sessionsvc.NewPushJWTSigner(pushPrivateKey, strings.TrimRight(pushIssuer, "/"))
	if err != nil {
		return fmt.Errorf("configure push JWT signing: %w", err)
	}
	if err := manager.Add(sessionsvc.NewPushWorker(store, pushSender, pushSigner, pushWake, cfg.GetPush().GetRecoveryInterval().AsDuration())); err != nil {
		return fmt.Errorf("failed to add push worker: %w", err)
	}
	gateway := a2agateway.New(interactions, gatewayDialer, cfg.GetGateway().GetPublicUrl())
	schedules := scheduledrun.NewService(store, manager.GetClient(), authorizer)
	if err := manager.Add(scheduledruncontroller.NewScheduler(store, cfg.GetScheduledRuns().GetPollInterval().AsDuration())); err != nil {
		return fmt.Errorf("add scheduled run scheduler: %w", err)
	}
	if err := manager.Add(scheduledruncontroller.NewController(store, sessionWorkflow,
		gateway, cfg.GetScheduledRuns().GetExecutionInterval().AsDuration())); err != nil {
		return fmt.Errorf("add scheduled run controller: %w", err)
	}
	sandboxTemplates := kubecrud.NewService(manager.GetClient(), authorizer, &kagentv1alpha3.SandboxTemplate{}, &kagentv1alpha3.SandboxTemplateList{}, kagentv1alpha3.SandboxTemplateKind)
	guests, err := sandboxservice.NewGuestDialer(cfg.GetSubstrate().GetRouterUrl(), authenticator)
	if err != nil {
		return err
	}
	defer guests.Close()
	policy := substrate.SandboxPolicy{
		GuestImage: cfg.GetSandboxes().GetGuestImage(),
		CPU:        cfg.GetSandboxes().GetCpu(),
		Memory:     cfg.GetSandboxes().GetMemory(),
	}
	preparation, err := controller.NewSandboxReconciler(kubeConfig, runtime, store, actors, policy)
	if err != nil {
		return err
	}
	if err := manager.Add(preparation); err != nil {
		return err
	}
	sandboxes, err := sandboxservice.NewService(sandboxservice.Config{Store: store, Kube: manager.GetClient(), Authorizer: authorizer, Actors: actors, Guests: guests,
		DefaultTTL: cfg.GetSandboxes().GetDefaultTtl().AsDuration(), MaxTTL: cfg.GetSandboxes().GetMaxTtl().AsDuration(), ExpirationPollInterval: cfg.GetSandboxes().GetExpirationInterval().AsDuration()})
	if err != nil {
		return err
	}
	if err := manager.Add(sandboxes); err != nil {
		return err
	}
	mcpHandler, err := mcp.New(sessions, checkpoints, gateway, sandboxes, sandboxTemplates)
	if err != nil {
		return err
	}
	policies, err := mergePolicies(grpcserver.DefaultMethodPolicies(), opts.MethodPolicies)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	if pushSigner != nil {
		mux.Handle("GET /.well-known/jwks.json", pushSigner)
	} else {
		mux.HandleFunc("GET /.well-known/jwks.json", http.NotFound)
	}
	mux.Handle("/mcp", otelhttp.NewHandler(auth.AuthnMiddleware(authenticator)(mcpHandler), "/mcp"))
	mux.Handle(a2agateway.HTTPPathPrefix, otelhttp.NewHandler(a2agateway.NewHTTPHandler(gateway, authenticator, store), a2agateway.HTTPPathPrefix))
	server, err := grpcserver.New(grpcserver.Config{
		MethodPolicies:        policies,
		RegisterServices:      opts.GRPCServices,
		BindAddress:           cfg.GetServer().GetBindAddress(),
		Reflection:            cfg.GetServer().GetGrpcReflection(),
		Authenticator:         authenticator,
		RuntimeAuthenticator:  &taskstore.Authenticator{},
		ShareStore:            store,
		ModelService:          models,
		ToolService:           tools,
		PromptTemplateService: prompts,
		SystemService:         system,
		MemoryService:         memory,
		TaskStoreService:      runtimeTasks,
		SessionService:        sessions,
		ScheduledRunService:   schedules,
		// Author Agents and their reusable configuration through the API.
		AgentService:           agents,
		AgentTemplateService:   kubecrud.NewService(manager.GetClient(), authorizer, &kagentv1alpha3.AgentTemplate{}, &kagentv1alpha3.AgentTemplateList{}, "AgentTemplate"),
		HarnessService:         kubecrud.NewService(manager.GetClient(), authorizer, &kagentv1alpha3.Harness{}, &kagentv1alpha3.HarnessList{}, "Harness"),
		SandboxTemplateService: sandboxTemplates,
		SandboxService:         sandboxes,
		CheckpointService:      checkpoints,
		A2AHandler:             gateway,
		HTTPHandler:            mux,
	})
	if err != nil {
		return err
	}

	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error { return runtime.Start(ctx) })
	group.Go(func() error { return manager.Start(ctx) })
	group.Go(func() error { return server.Start(ctx) })
	return group.Wait()
}

// mergePolicies overlays a consumer's method policies onto core's defaults.
//
// A collision is an error rather than an override: core's methods keep the
// access core assigned them, so this cannot be used to make an authenticated
// method public.
func mergePolicies(defaults grpcserver.MethodPolicies, extra map[string]auth.AccessMode) (grpcserver.MethodPolicies, error) {
	merged := make(grpcserver.MethodPolicies, len(defaults)+len(extra))
	maps.Copy(merged, defaults)
	for method, access := range extra {
		if _, taken := defaults[method]; taken {
			return nil, fmt.Errorf("method policy for %s is core's to set", method)
		}
		merged[method] = access
	}
	return merged, nil
}

func postgresConfig(cfg *configv1.DatabaseConfig, pool *pgxpool.Config) *database.PostgresConfig {
	return &database.PostgresConfig{Pool: pool, VectorEnabled: cfg.GetVectorEnabled(),
		Schema: cfg.GetSchema(), VectorSchema: cfg.GetVectorSchema(),
		MaxConns: new(cfg.GetPool().GetMaxConns()), MinConns: new(cfg.GetPool().GetMinConns()),
		MaxConnIdleTime: new(cfg.GetPool().GetMaxConnIdleTime().AsDuration()), MaxConnLifetime: new(cfg.GetPool().GetMaxConnLifetime().AsDuration())}
}

// migrationSources appends extra after the built-in tracks: the built-ins must reach
// their final version before a library consumer's tables, which may reference them.
func migrationSources(cfg *configv1.DatabaseConfig, extra []migrations.Source) []migrations.Source {
	builtins := migrations.BuiltinSourcesInSchema(cfg.GetVectorEnabled(), cfg.GetSchema(), cfg.GetVectorSchema())
	return append(builtins, extra...)
}

// databasePoolConfig reads one login's connection string and builds its pool
// configuration. field names the login in errors.
func databasePoolConfig(field string, login *configv1.DatabaseLoginConfig) (*pgxpool.Config, error) {
	url, err := config.ReadSecret(login.GetUrlFile())
	if err != nil {
		return nil, fmt.Errorf("%s.urlFile: %w", field, err)
	}
	pool, err := postgres.PoolConfig(url, login.GetRole())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", field, err)
	}
	return pool, nil
}

func namespaceCache(names []string) map[string]cache.Config {
	if len(names) == 0 {
		return nil
	}
	result := make(map[string]cache.Config, len(names))
	for _, name := range names {
		result[name] = cache.Config{}
	}
	return result
}
