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
	"strconv"
	"strings"
	"time"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/a2agateway"
	v2controller "github.com/kagent-dev/kagent/go/core/internal/controller"
	mcpservercontroller "github.com/kagent-dev/kagent/go/core/internal/controller/mcpserver"
	remotemcpcontroller "github.com/kagent-dev/kagent/go/core/internal/controller/remotemcpserver"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/grpcserver"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	v2mcp "github.com/kagent-dev/kagent/go/core/internal/mcp"
	"github.com/kagent-dev/kagent/go/core/internal/service/agentinstance"
	"github.com/kagent-dev/kagent/go/core/internal/service/checkpoint"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	memoryservice "github.com/kagent-dev/kagent/go/core/internal/service/memory"
	modelservice "github.com/kagent-dev/kagent/go/core/internal/service/model"
	prompttemplateservice "github.com/kagent-dev/kagent/go/core/internal/service/prompttemplate"
	systemservice "github.com/kagent-dev/kagent/go/core/internal/service/system"
	toolservice "github.com/kagent-dev/kagent/go/core/internal/service/tool"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/internal/telemetry"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	kmcp "github.com/kagent-dev/kmcp/api/v1alpha1"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// Options are the components a library consumer may supply in place of core's own.
//
// A nil field is not an error: it selects the default, which is what this
// repository's controller runs with. Supplying one does not change how core
// uses it — the authenticator still guards the gRPC server and the /mcp
// endpoint, and the authorizer is still consulted by every service that takes
// one — so a library consumer cannot narrow where its own policy applies.
type Options struct {
	// Authenticator identifies the caller. Nil selects UnsecureAuthenticator,
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
		authenticator = &authimpl.UnsecureAuthenticator{}
	}
	authorizer := o.Authorizer
	if authorizer == nil {
		authorizer = &authimpl.NoopAuthorizer{}
	}
	return authenticator, authorizer
}

// SetupLogger installs the controller-runtime logger, at the level named by
// LOG_LEVEL.
//
// Run calls this itself, so a library consumer needs it only when it logs
// before Run — and it must then call it first, because controller-runtime
// discards everything written through log.Log until the first SetLogger, and
// Run's call comes after the consumer has already built its Options. A startup
// error reported in that window is otherwise lost, which reads as a process
// that died silently.
//
// Calling it twice is harmless. SetLogger fulfils a promise that can only be
// fulfilled once, so the first caller wins and Run's own call does nothing.
func SetupLogger() error {
	logger, err := logging.NewFromEnv(os.Stderr)
	if err != nil {
		return fmt.Errorf("parse LOG_LEVEL: %w", err)
	}
	slog.SetDefault(logger)
	ctrl.SetLogger(logging.AsLogr(logger))
	return nil
}

// Run boots the controller and blocks until ctx is cancelled or a component
// fails. It returns the first error rather than exiting, so a library consumer
// keeps control of how the process ends.
func Run(ctx context.Context, opts Options) error {
	if err := SetupLogger(); err != nil {
		return err
	}
	logger := slog.Default()
	ctx = logging.IntoContext(ctx, logger)
	// otelgrpc snapshots the global TracerProvider and propagator when its handler
	// is constructed, so tracing has to be registered before any server is built.
	shutdownTracing, err := telemetry.InitTracerProvider(ctx, version.Version)
	if err != nil {
		return fmt.Errorf("initialize tracing: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(shutdownCtx); err != nil {
			logger.ErrorContext(shutdownCtx, "failed to shut down tracing", "error", err)
		}
	}()

	dbURL, err := database.ResolveURL(env("POSTGRES_DATABASE_URL", "postgres://postgres:kagent@kagent-postgresql.kagent.svc.cluster.local:5432/postgres"), os.Getenv("POSTGRES_DATABASE_URL_FILE"))
	if err != nil {
		return err
	}
	vectorEnabled := kagentenv.DatabaseVectorEnabled.Get()
	// Appended, not merged: the built-in tracks must reach their final version
	// before a library consumer's tables, which may reference them.
	sources := append(migrations.BuiltinSources(vectorEnabled), opts.ExtraMigrations...)
	if kagentenv.SkipMigrations.Get() {
		if err := migrations.VerifyMigrated(ctx, dbURL, sources); err != nil {
			return fmt.Errorf("verify database migrations: %w", err)
		}
	} else if err := migrations.RunUp(ctx, dbURL, sources); err != nil {
		return fmt.Errorf("run database migrations: %w", err)
	}
	db, err := database.Connect(ctx, &database.PostgresConfig{URL: dbURL, VectorEnabled: vectorEnabled})
	if err != nil {
		return err
	}
	defer db.Close()
	store := database.NewClient(db)

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
	utilruntime.Must(kmcp.AddToScheme(managerScheme))
	watchNamespaces := namespaces(os.Getenv("WATCH_NAMESPACES"))
	managerClientOptions := client.Options{}
	managerCacheOptions := cache.Options{DefaultNamespaces: namespaceCache(watchNamespaces)}
	if len(watchNamespaces) > 0 {
		// A namespaced Role cannot list cluster-scoped Namespace objects. Read them
		// directly so SystemService can fall back to the configured names on a
		// Forbidden response without a failing Namespace informer blocking startup.
		managerClientOptions.Cache = &client.CacheOptions{DisableFor: []client.Object{&corev1.Namespace{}}}
	}
	manager, err := ctrl.NewManager(kubeConfig, ctrl.Options{
		Scheme:                  managerScheme,
		Cache:                   managerCacheOptions,
		Client:                  managerClientOptions,
		Metrics:                 metricsserver.Options{BindAddress: "0"},
		LeaderElection:          envBool("LEADER_ELECT"),
		LeaderElectionID:        "0e9f6799.kagent.dev",
		LeaderElectionNamespace: env("KAGENT_NAMESPACE", "kagent"),
	})
	if err != nil {
		return fmt.Errorf("create controller manager: %w", err)
	}
	runtime, err := v2controller.NewRuntime(kubeConfig, watchNamespaces, ctx.Done())
	if err != nil {
		return err
	}
	actors, err := substrate.Dial(ctx, substrate.Config{
		AteAPIEndpoint: env("SUBSTRATE_ATE_API_ENDPOINT", "dns:///api.ate-system.svc:443"),
		CAFile:         os.Getenv("SUBSTRATE_ATE_API_CA_FILE"),
		ClientCertFile: os.Getenv("SUBSTRATE_ATE_API_CLIENT_CERT_FILE"),
		CallTimeout:    30 * time.Second,
	})
	if err != nil {
		return err
	}
	defer actors.Close()
	reconciler, err := v2controller.NewReconciler(kubeConfig, runtime.Collections, store, actors)
	if err != nil {
		return err
	}
	if err := manager.Add(reconciler); err != nil {
		return fmt.Errorf("add reconciler to controller manager: %w", err)
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
	mcpServerDiscovery := mcpservercontroller.New(manager.GetClient(), mcpClient, store)
	if err := mcpServerDiscovery.SetupWithManager(manager); err != nil {
		return fmt.Errorf("set up MCPServer discovery: %w", err)
	}

	authenticator, authorizer := opts.resolve()
	resourceNamespace := env("KAGENT_NAMESPACE", "kagent")
	models := modelservice.NewService(manager.GetClient(), authorizer, resourceNamespace)
	tools := toolservice.NewService(manager.GetClient(), store, authorizer, resourceNamespace, mcpClient)
	prompts := prompttemplateservice.NewService(manager.GetClient(), authorizer)
	system := systemservice.NewService(manager.GetClient(), watchNamespaces, authorizer, actors, store)
	memory := memoryservice.NewService(store)
	instanceWorkflow := agentinstance.NewActorWorkflow(store, actors)
	instances := agentinstance.NewService(store, authorizer, instanceWorkflow)
	checkpoints := checkpoint.NewService(store, authorizer, actors, instanceWorkflow)
	gatewayDialer, err := a2agateway.NewRuntimeDialer(
		env("SUBSTRATE_ATENET_ROUTER_URL", substrate.DefaultAtenetRouterURL),
		authenticator,
	)
	if err != nil {
		return err
	}
	gateway := a2agateway.New(store, authorizer, gatewayDialer, instanceWorkflow,
		env("A2A_GATEWAY_URL", "http://127.0.0.1:8084"))
	mcpHandler, err := v2mcp.New(instances, checkpoints, gateway)
	if err != nil {
		return err
	}
	policies, err := mergePolicies(grpcserver.DefaultMethodPolicies(), opts.MethodPolicies)
	if err != nil {
		return err
	}
	server, err := grpcserver.New(grpcserver.Config{
		MethodPolicies:        policies,
		RegisterServices:      opts.GRPCServices,
		BindAddress:           env("GRPC_BIND_ADDRESS", ":8084"),
		Reflection:            envBool("GRPC_REFLECTION"),
		Authenticator:         authenticator,
		ShareStore:            store,
		ModelService:          models,
		ToolService:           tools,
		PromptTemplateService: prompts,
		SystemService:         system,
		MemoryService:         memory,
		AgentInstanceService:  instances,
		// Both halves of the pair CreateAgentInstance names. Without these two
		// the only way to author a Harness or an AgentTemplate is kubectl.
		AgentTemplateService: kubecrud.NewService(manager.GetClient(), authorizer, &kagentv1alpha3.AgentTemplate{}, &kagentv1alpha3.AgentTemplateList{}, "AgentTemplate"),
		HarnessService:       kubecrud.NewService(manager.GetClient(), authorizer, &kagentv1alpha3.Harness{}, &kagentv1alpha3.HarnessList{}, "Harness"),
		CheckpointService:    checkpoints,
		A2AHandler:           gateway,
	})
	if err != nil {
		return err
	}

	// The HTTP port serves health *and* gRPC-Web, because a browser cannot speak
	// gRPC and this is the only port a page can reach: the chart's nginx proxies
	// /api here, while :8084 speaks native gRPC that `fetch` has no way to talk to.
	//
	// Worth stating because the previous shape of this looked correct and was not.
	// It answered every path with an empty 200 and ignored the request entirely, so
	// a browser calling an RPC got a success with no body — which reads as a
	// serialisation fault in the client rather than as a server that never had the
	// endpoint. The router below serves MCP and hands other non-gRPC-Web requests
	// to the same health response as before.
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("/mcp", auth.AuthnMiddleware(authenticator)(mcpHandler))
	health := &http.Server{Addr: env("HTTP_BIND_ADDRESS", ":8083"), Handler: server.WebHandlerOr(mux)}
	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error { return runtime.Start(ctx) })
	group.Go(func() error { return manager.Start(ctx) })
	group.Go(func() error { return server.Start(ctx) })
	group.Go(func() error {
		go func() {
			<-ctx.Done()
			_ = health.Shutdown(context.Background())
		}()
		if err := health.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("serve health endpoint: %w", err)
		}
		return nil
	})
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

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envBool(name string) bool {
	value, _ := strconv.ParseBool(os.Getenv(name))
	return value
}

func namespaces(value string) []string {
	var result []string
	for namespace := range strings.SplitSeq(value, ",") {
		if namespace = strings.TrimSpace(namespace); namespace != "" {
			result = append(result, namespace)
		}
	}
	return result
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
