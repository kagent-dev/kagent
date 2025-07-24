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

package main

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

	"github.com/kagent-dev/kagent/go/internal/version"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"

	autogen "github.com/kagent-dev/kagent/go/controller/internal/autogen"
	"github.com/kagent-dev/kagent/go/controller/translator"
	"github.com/kagent-dev/kagent/go/internal/a2a"
	a2a_manager "github.com/kagent-dev/kagent/go/internal/a2a/manager"
	autogen_client "github.com/kagent-dev/kagent/go/internal/autogen/client"
	"github.com/kagent-dev/kagent/go/internal/database"

	a2a_reconciler "github.com/kagent-dev/kagent/go/controller/internal/a2a"
	"github.com/kagent-dev/kagent/go/internal/httpserver"
	common "github.com/kagent-dev/kagent/go/internal/utils"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

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
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	agentv1alpha1 "github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/controller"
	"github.com/kagent-dev/kagent/go/internal/goruntime"
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

	utilruntime.Must(agentv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
}

// nolint:gocyclo
func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var autogenStudioBaseURL string
	var defaultModelConfig types.NamespacedName
	var tlsOpts []func(*tls.Config)
	var httpServerAddr string
	var watchNamespaces string
	var a2aBaseUrl string
	var databasePath string
	var databaseType string
	var databaseURL string

	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8082", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")

	flag.StringVar(&autogenStudioBaseURL, "autogen-base-url", "http://127.0.0.1:8081/api", "The base url of the Autogen Studio server.")

	flag.StringVar(&defaultModelConfig.Name, "default-model-config-name", "default-model-config", "The name of the default model config.")
	flag.StringVar(&defaultModelConfig.Namespace, "default-model-config-namespace", kagentNamespace, "The namespace of the default model config.")
	flag.StringVar(&httpServerAddr, "http-server-address", ":8083", "The address the HTTP server binds to.")
	flag.StringVar(&a2aBaseUrl, "a2a-base-url", "http://127.0.0.1:8083", "The base URL of the A2A Server endpoint, as advertised to clients.")
	flag.StringVar(&databaseType, "database-type", "sqlite", "The type of the database to use. Supported values: sqlite, postgres.")
	flag.StringVar(&databasePath, "sqlite-database-path", "./kagent.db", "The path to the SQLite database file.")
	flag.StringVar(&databaseURL, "postgres-database-url", "postgres://postgres:kagent@db.kagent.svc.cluster.local:5432/crud", "The URL of the PostgreSQL database.")

	flag.StringVar(&watchNamespaces, "watch-namespaces", "", "The namespaces to watch for .")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	logger := zap.New(zap.UseFlagOptions(&opts))

	ctrl.SetLogger(logger)

	goruntime.SetMaxProcs(logger)

	setupLog.Info("Starting KAgent Controller", "version", Version, "git_commit", GitCommit, "build_date", BuildDate)

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

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Create watchers for metrics and webhooks certificates
	var metricsCertWatcher, webhookCertWatcher *certwatcher.CertWatcher

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		var err error
		webhookCertWatcher, err = certwatcher.New(
			filepath.Join(webhookCertPath, webhookCertName),
			filepath.Join(webhookCertPath, webhookCertKey),
		)
		if err != nil {
			setupLog.Error(err, "Failed to initialize webhook certificate watcher")
			os.Exit(1)
		}

		webhookTLSOpts = append(webhookTLSOpts, func(config *tls.Config) {
			config.GetCertificate = webhookCertWatcher.GetCertificate
		})
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: webhookTLSOpts,
	})

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.0/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
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
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		var err error
		metricsCertWatcher, err = certwatcher.New(
			filepath.Join(metricsCertPath, metricsCertName),
			filepath.Join(metricsCertPath, metricsCertKey),
		)
		if err != nil {
			setupLog.Error(err, "to initialize metrics certificate watcher", "error", err)
			os.Exit(1)
		}

		metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, func(config *tls.Config) {
			config.GetCertificate = metricsCertWatcher.GetCertificate
		})
	}

	// filter out invalid namespaces from the watchNamespaces flag (comma separated list)
	watchNamespacesList := filterValidNamespaces(strings.Split(watchNamespaces, ","))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "0e9f6799.kagent.dev",
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

	// Initialize database
	dbManager, err := database.NewManager(&database.Config{
		DatabaseType: database.DatabaseType(databaseType),
		SqliteConfig: &database.SqliteConfig{
			DatabasePath: databasePath,
		},
		PostgresConfig: &database.PostgresConfig{
			URL: databaseURL,
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to initialize database")
		os.Exit(1)
	}

	// Initialize database tables
	if err := dbManager.Initialize(); err != nil {
		setupLog.Error(err, "unable to initialize database")
		os.Exit(1)
	}

	dbClient := database.NewClient(dbManager)

	autogenClient := autogen_client.New(
		autogenStudioBaseURL,
	)

	// wait for autogen to become ready on port 8081 before starting the manager
	if err := waitForAutogenReady(context.Background(), setupLog, autogenClient, time.Minute*5, time.Second*15); err != nil {
		setupLog.Error(err, "failed to wait for autogen to become ready")
		os.Exit(1)
	}

	kubeClient := mgr.GetClient()

	apiTranslator := translator.NewAutogenApiTranslator(
		kubeClient,
		defaultModelConfig,
	)

	a2aStorage := a2a_manager.NewStorage(dbClient)

	a2aHandler := a2a.NewA2AHttpMux(httpserver.APIPathA2A, a2aStorage)

	a2aReconciler := a2a_reconciler.NewAutogenReconciler(
		autogenClient,
		a2aHandler,
		a2aBaseUrl+httpserver.APIPathA2A,
		dbClient,
	)

	autogenReconciler := autogen.NewAutogenReconciler(
		apiTranslator,
		kubeClient,
		autogenClient,
		dbClient,
		defaultModelConfig,
		a2aReconciler,
	)

	if err = (&controller.AutogenTeamReconciler{
		Client:     kubeClient,
		Scheme:     mgr.GetScheme(),
		Reconciler: autogenReconciler,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AutogenTeam")
		os.Exit(1)
	}
	if err = (&controller.AutogenAgentReconciler{
		Client:     kubeClient,
		Scheme:     mgr.GetScheme(),
		Reconciler: autogenReconciler,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AutogenAgent")
		os.Exit(1)
	}
	if err = (&controller.AutogenModelConfigReconciler{
		Client:     kubeClient,
		Scheme:     mgr.GetScheme(),
		Reconciler: autogenReconciler,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AutogenModelConfig")
		os.Exit(1)
	}
	if err = (&controller.AutogenSecretReconciler{
		Client:     kubeClient,
		Scheme:     mgr.GetScheme(),
		Reconciler: autogenReconciler,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AutogenSecret")
		os.Exit(1)
	}
	if err = (&controller.ToolServerReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		Reconciler: autogenReconciler,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ToolServer")
		os.Exit(1)
	}
	if err = (&controller.AutogenMemoryReconciler{
		Client:     kubeClient,
		Scheme:     mgr.GetScheme(),
		Reconciler: autogenReconciler,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Memory")
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

	httpServer, err := httpserver.NewHTTPServer(httpserver.ServerConfig{
		BindAddr:          httpServerAddr,
		AutogenClient:     autogenClient,
		KubeClient:        kubeClient,
		A2AHandler:        a2aHandler,
		WatchedNamespaces: watchNamespacesList,
		DbClient:          dbClient,
	})
	if err := mgr.Add(httpServer); err != nil {
		setupLog.Error(err, "unable to set up HTTP server")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func waitForAutogenReady(
	ctx context.Context,
	log logr.Logger,
	client autogen_client.Client,
	timeout, interval time.Duration,
) error {
	log.Info("waiting for autogen to become ready")
	return waitForReady(func() error {
		version, err := client.GetVersion(ctx)
		if err != nil {
			log.Error(err, "autogen is not ready")
			return err
		}
		log.Info("autogen is ready", "version", version)
		return nil
	}, timeout, interval)
}

func waitForReady(f func() error, timeout, interval time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %v", timeout)
		}
		if err := f(); err == nil {
			return nil
		}

		time.Sleep(interval)
	}
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
