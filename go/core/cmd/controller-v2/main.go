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

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/grpcserver"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	sessionservice "github.com/kagent-dev/kagent/go/core/internal/service/session"
	taskservice "github.com/kagent-dev/kagent/go/core/internal/service/task"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
	legacysubstrate "github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
	"github.com/kagent-dev/kagent/go/core/v2/a2agateway"
	"github.com/kagent-dev/kagent/go/core/v2/agentinstance"
	"github.com/kagent-dev/kagent/go/core/v2/checkpoint"
	v2controller "github.com/kagent-dev/kagent/go/core/v2/controller"
	"golang.org/x/sync/errgroup"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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
	manager, err := ctrl.NewManager(kubeConfig, ctrl.Options{
		Scheme:                  managerScheme,
		Metrics:                 metricsserver.Options{BindAddress: "0"},
		LeaderElection:          envBool("LEADER_ELECT"),
		LeaderElectionID:        "0e9f6799.kagent.dev",
		LeaderElectionNamespace: env("KAGENT_NAMESPACE", "kagent"),
	})
	if err != nil {
		log.Fatalf("create controller manager: %v", err)
	}
	runtime, err := v2controller.NewRuntime(kubeConfig, namespaces(os.Getenv("WATCH_NAMESPACES")), ctx.Done())
	if err != nil {
		log.Fatal(err)
	}
	reconciler, err := v2controller.NewReconciler(kubeConfig, runtime.Collections, store)
	if err != nil {
		log.Fatal(err)
	}
	if err := manager.Add(reconciler); err != nil {
		log.Fatalf("add reconciler to controller manager: %v", err)
	}

	actors, err := legacysubstrate.Dial(ctx, legacysubstrate.Config{
		AteAPIEndpoint: env("SUBSTRATE_ATE_API_ENDPOINT", "dns:///api.ate-system.svc:443"),
		CAFile:         os.Getenv("SUBSTRATE_ATE_API_CA_FILE"),
		ClientCertFile: os.Getenv("SUBSTRATE_ATE_API_CLIENT_CERT_FILE"),
		CallTimeout:    30 * time.Second,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer actors.Close()

	authenticator := &authimpl.UnsecureAuthenticator{}
	authorizer := &authimpl.NoopAuthorizer{}
	instanceWorkflow := agentinstance.NewActorWorkflow(store, actors)
	instances := agentinstance.NewService(store, authorizer, instanceWorkflow)
	checkpoints := checkpoint.NewService(store, authorizer, actors, instanceWorkflow)
	gatewayDialer, err := a2agateway.NewRuntimeDialer(
		env("SUBSTRATE_ATENET_ROUTER_URL", legacysubstrate.DefaultAtenetRouterURL),
		authenticator,
	)
	if err != nil {
		log.Fatal(err)
	}
	server, err := grpcserver.New(grpcserver.Config{
		BindAddress:          env("GRPC_BIND_ADDRESS", ":8084"),
		Reflection:           envBool("GRPC_REFLECTION"),
		Authenticator:        authenticator,
		ShareStore:           store,
		SessionService:       sessionservice.NewService(store),
		TaskService:          taskservice.NewService(store),
		AgentInstanceService: instances,
		// Both halves of the pair CreateAgentInstance names. Without these two
		// the only way to author a Harness or an AgentTemplate is kubectl.
		AgentTemplateService: kubecrud.NewService(manager.GetClient(), authorizer, &kagentv1alpha3.AgentTemplate{}, &kagentv1alpha3.AgentTemplateList{}, "AgentTemplate"),
		HarnessService:       kubecrud.NewService(manager.GetClient(), authorizer, &kagentv1alpha3.Harness{}, &kagentv1alpha3.HarnessList{}, "Harness"),
		CheckpointService:    checkpoints,
		// `instanceWorkflow` is what upstream added: the gateway needs it to suspend an
		// instance once a turn reaches a quiescent boundary.
		A2AHandler: a2agateway.New(store, authorizer, gatewayDialer, instanceWorkflow,
			env("A2A_GATEWAY_URL", "http://127.0.0.1:8084")),
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
	// endpoint. The router below hands anything that is not gRPC-Web to the same
	// health response as before.
	httpHandler := server.WebHandlerOr(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	health := &http.Server{Addr: env("HTTP_BIND_ADDRESS", ":8083"), Handler: httpHandler}
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
