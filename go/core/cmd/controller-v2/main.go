/*
Copyright 2026.

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

package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	mcpservercontroller "github.com/kagent-dev/kagent/go/core/internal/controller/mcpserver"
	remotemcpcontroller "github.com/kagent-dev/kagent/go/core/internal/controller/remotemcpserver"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/grpcserver"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	feedbackservice "github.com/kagent-dev/kagent/go/core/internal/service/feedback"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	memoryservice "github.com/kagent-dev/kagent/go/core/internal/service/memory"
	modelservice "github.com/kagent-dev/kagent/go/core/internal/service/model"
	prompttemplateservice "github.com/kagent-dev/kagent/go/core/internal/service/prompttemplate"
	systemservice "github.com/kagent-dev/kagent/go/core/internal/service/system"
	toolservice "github.com/kagent-dev/kagent/go/core/internal/service/tool"
	"github.com/kagent-dev/kagent/go/core/internal/telemetry"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
	"github.com/kagent-dev/kagent/go/core/v2/a2agateway"
	"github.com/kagent-dev/kagent/go/core/v2/agentinstance"
	"github.com/kagent-dev/kagent/go/core/v2/checkpoint"
	v2controller "github.com/kagent-dev/kagent/go/core/v2/controller"
	v2mcp "github.com/kagent-dev/kagent/go/core/v2/mcp"
	"github.com/kagent-dev/kagent/go/core/v2/substrate"
	kmcp "github.com/kagent-dev/kmcp/api/v1alpha1"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logLevel := zapcore.InfoLevel
	if value := os.Getenv("ZAP_LOG_LEVEL"); value != "" {
		if err := logLevel.Set(value); err != nil {
			log.Fatalf("parse ZAP_LOG_LEVEL: %v", err)
		}
	}
	ctrl.SetLogger(zap.New(zap.Level(logLevel)))
	// otelgrpc snapshots the global TracerProvider and propagator when its handler
	// is constructed, so tracing has to be registered before any server is built.
	shutdownTracing, err := telemetry.InitTracerProvider(ctx, version.Version)
	if err != nil {
		log.Fatalf("initialize tracing: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(shutdownCtx); err != nil {
			log.Printf("shutdown tracing: %v", err)
		}
	}()

	dbURL, err := database.ResolveURL(env("POSTGRES_DATABASE_URL", "postgres://postgres:kagent@kagent-postgresql.kagent.svc.cluster.local:5432/postgres"), os.Getenv("POSTGRES_DATABASE_URL_FILE"))
	if err != nil {
		log.Fatal(err)
	}
	if err := migrations.RunUp(ctx, dbURL, migrations.BuiltinSources(false)); err != nil {
		log.Fatalf("run database migrations: %v", err)
	}
	db, err := database.Connect(ctx, &database.PostgresConfig{URL: dbURL})
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	store := database.NewClient(db)

	kubeConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		log.Fatalf("load Kubernetes config: %v", err)
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
		log.Fatalf("create controller manager: %v", err)
	}
	runtime, err := v2controller.NewRuntime(kubeConfig, watchNamespaces, ctx.Done())
	if err != nil {
		log.Fatal(err)
	}
	actors, err := substrate.Dial(ctx, substrate.Config{
		AteAPIEndpoint: env("SUBSTRATE_ATE_API_ENDPOINT", "dns:///api.ate-system.svc:443"),
		CAFile:         os.Getenv("SUBSTRATE_ATE_API_CA_FILE"),
		ClientCertFile: os.Getenv("SUBSTRATE_ATE_API_CLIENT_CERT_FILE"),
		CallTimeout:    30 * time.Second,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer actors.Close()
	reconciler, err := v2controller.NewReconciler(kubeConfig, runtime.Collections, store, actors)
	if err != nil {
		log.Fatal(err)
	}
	if err := manager.Add(reconciler); err != nil {
		log.Fatalf("add reconciler to controller manager: %v", err)
	}
	mcpClient := toolservice.NewRuntimeMCPClient(manager.GetClient())
	remoteMCPDiscovery := remotemcpcontroller.New(manager.GetClient(), mcpClient, store)
	if err := remoteMCPDiscovery.SetupWithManager(manager); err != nil {
		log.Fatalf("set up RemoteMCPServer discovery: %v", err)
	}
	mcpServerDiscovery := mcpservercontroller.New(manager.GetClient(), mcpClient, store)
	if err := mcpServerDiscovery.SetupWithManager(manager); err != nil {
		log.Fatalf("set up MCPServer discovery: %v", err)
	}

	authenticator := &authimpl.UnsecureAuthenticator{}
	authorizer := &authimpl.NoopAuthorizer{}
	resourceNamespace := env("KAGENT_NAMESPACE", "kagent")
	models := modelservice.NewService(manager.GetClient(), authorizer, resourceNamespace)
	tools := toolservice.NewService(manager.GetClient(), store, authorizer, resourceNamespace, mcpClient)
	prompts := prompttemplateservice.NewService(manager.GetClient(), authorizer)
	system := systemservice.NewService(
		systemservice.WithInventory(manager.GetClient(), watchNamespaces, authorizer, actors),
		systemservice.WithRuntimeRevisions(store),
	)
	feedback := feedbackservice.NewService(store)
	memory := memoryservice.NewService(store)
	instanceWorkflow := agentinstance.NewActorWorkflow(store, actors)
	instances := agentinstance.NewService(store, authorizer, instanceWorkflow)
	checkpoints := checkpoint.NewService(store, authorizer, actors, instanceWorkflow)
	gatewayDialer, err := a2agateway.NewRuntimeDialer(
		env("SUBSTRATE_ATENET_ROUTER_URL", substrate.DefaultAtenetRouterURL),
		authenticator,
	)
	if err != nil {
		log.Fatal(err)
	}
	gateway := a2agateway.New(store, authorizer, gatewayDialer, instanceWorkflow,
		env("A2A_GATEWAY_URL", "http://127.0.0.1:8084"))
	mcpHandler, err := v2mcp.New(instances, checkpoints, gateway)
	if err != nil {
		log.Fatal(err)
	}
	server, err := grpcserver.New(grpcserver.Config{
		BindAddress:           env("GRPC_BIND_ADDRESS", ":8084"),
		Reflection:            envBool("GRPC_REFLECTION"),
		Authenticator:         authenticator,
		ShareStore:            store,
		ModelService:          models,
		ToolService:           tools,
		PromptTemplateService: prompts,
		SystemService:         system,
		FeedbackService:       feedback,
		MemoryService:         memory,
		AgentInstanceService:  instances,
		// Both halves of the pair CreateAgentInstance names. Without these two
		// the only way to author a Harness or an AgentTemplate is kubectl.
		AgentTemplateService: kubecrud.NewScopedService(manager.GetClient(), authorizer, &kagentv1alpha3.AgentTemplate{}, &kagentv1alpha3.AgentTemplateList{}, "AgentTemplate"),
		HarnessService:       kubecrud.NewScopedService(manager.GetClient(), authorizer, &kagentv1alpha3.Harness{}, &kagentv1alpha3.HarnessList{}, "Harness"),
		CheckpointService:    checkpoints,
		A2AHandler:           gateway,
	})
	if err != nil {
		log.Fatal(err)
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
	if err := group.Wait(); err != nil {
		log.Fatal(err)
	}
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
