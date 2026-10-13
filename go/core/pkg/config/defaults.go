package config

import (
	"runtime"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	"github.com/kagent-dev/kagent/go/core/pkg/consts"
)

// DefaultCLI supplies defaults without consulting process environment.
func DefaultCLI(cfg *apiv1.CLIConfig) {
	defaults(cfg, &apiv1.CLIConfig{
		Connection: &apiv1.ConnectionConfig{ApiUrl: new("http://localhost:8083"), GatewayUrl: new("http://localhost:8083"), Namespace: new("kagent"), UserId: new("admin@kagent.dev"), Timeout: durationpb.New(5 * time.Minute)},
		Output:     &apiv1.OutputConfig{Format: new("table")},
		Logging:    &apiv1.LoggingConfig{Level: new("info")},
		Install:    &apiv1.InstallConfig{DefaultModelProvider: new("OpenAI"), BundledPostgresImage: new("postgres:18-alpine@sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15"), Kagent: &apiv1.ChartConfig{Repository: new("oci://ghcr.io/kagent-dev/kagent/helm/"), Version: new(version.Version)}, Substrate: &apiv1.ChartConfig{Repository: new("oci://ghcr.io/kagent-dev/substrate/helm/"), Version: new(version.SubstrateVersion)}},
		Database:   defaultDatabase(),
	})
	if cfg.Install.PodCertificate == nil {
		cfg.Install.PodCertificate = &apiv1.ChartConfig{}
	}
	defaults(cfg.Install.PodCertificate, &apiv1.ChartConfig{Repository: new(cfg.Install.Substrate.GetRepository()), Version: new(cfg.Install.Substrate.GetVersion())})
}

// DefaultController fills only absent settings. Secret file paths are required
// inputs, and remain empty until supplied by the deployment.
func DefaultController(cfg *apiv1.ControllerConfig) {
	defaults(cfg, &apiv1.ControllerConfig{
		Kubernetes:       &apiv1.KubernetesConfig{Namespace: new("kagent"), LeaderElection: new(true)},
		Server:           &apiv1.ControllerServerConfig{BindAddress: new(":8083"), Auth: &apiv1.AuthenticationConfig{Mode: new("insecure")}},
		Database:         defaultDatabase(),
		Substrate:        &apiv1.SubstrateConfig{ApiEndpoint: new("dns:///api.ate-system.svc:443"), RouterUrl: new("http://atenet-router.ate-system.svc:80")},
		Sessions:         &apiv1.SessionsConfig{IdleTtl: durationpb.New(7 * 24 * time.Hour), ShareMaxTtl: durationpb.New(0), ExpirationInterval: durationpb.New(time.Minute), QuiescenceRecoveryInterval: durationpb.New(time.Minute)},
		Gateway:          &apiv1.GatewayConfig{PublicUrl: new("http://localhost:8083"), Push: &apiv1.PushConfig{}},
		Sandboxes:        &apiv1.SandboxConfig{Cpu: new("1"), Memory: new("1Gi"), DefaultTtl: durationpb.New(time.Hour), MaxTtl: durationpb.New(24 * time.Hour), ExpirationInterval: durationpb.New(time.Second)},
		ScheduledRuns:    &apiv1.ScheduledRunsConfig{PollInterval: durationpb.New(time.Second), ExecutionInterval: durationpb.New(time.Second)},
		RuntimeRevisions: &apiv1.RuntimeRevisionsConfig{GcInterval: durationpb.New(time.Minute)},
		Metrics:          &apiv1.MetricsConfig{BindAddress: new("0")},
		Logging:          &apiv1.LoggingConfig{Level: new("info")},
		Telemetry:        &apiv1.TelemetryConfig{},
		RuntimeDefaults:  &apiv1.RuntimeDefaultsConfig{ControlPlaneUrl: new("http://kagent-controller.kagent:8083"), GatewayUrl: new("http://kagent-controller.kagent:8083"), Logging: &apiv1.LoggingConfig{Level: new("info")}, Telemetry: &apiv1.TelemetryConfig{}},
	})
	if cfg.Gateway.Push.Issuer == nil {
		cfg.Gateway.Push.Issuer = new(cfg.Gateway.GetPublicUrl())
	}
	defaultTelemetry(cfg.Telemetry)
	defaultTelemetry(cfg.RuntimeDefaults.Telemetry)
}

func defaultDatabase() *apiv1.DatabaseConfig {
	return &apiv1.DatabaseConfig{Schema: new(consts.DefaultPostgresTableSchema), VectorSchema: new(consts.DefaultPgvectorSchema), Pool: &apiv1.PostgresPoolConfig{MinConns: new(int32(0)), MaxConns: new(int32(max(4, runtime.NumCPU()))), MaxConnIdleTime: durationpb.New(30 * time.Minute), MaxConnLifetime: durationpb.New(time.Hour)}}
}

func defaultTelemetry(cfg *apiv1.TelemetryConfig) {
	if cfg.MaxCaptureBytes == nil {
		cfg.MaxCaptureBytes = new(int32(16384))
	}
	for _, signal := range []*apiv1.OTLPConfig{cfg.Traces, cfg.Metrics, cfg.Logs} {
		if signal == nil {
			continue
		}
		defaults(signal, &apiv1.OTLPConfig{Protocol: new("grpc"), Timeout: durationpb.New(10 * time.Second)})
	}
}

// ResolveControllerPaths makes each local file reference relative to its config.
func ResolveControllerPaths(cfg *apiv1.ControllerConfig, path string) {
	cfg.Database.UrlFile = FilePath(path, cfg.Database.UrlFile)
	cfg.Gateway.Push.SigningPrivateKeyFile = FilePath(path, cfg.Gateway.Push.SigningPrivateKeyFile)
	cfg.Substrate.CaFile = FilePath(path, cfg.Substrate.CaFile)
	cfg.Substrate.ClientCertFile = FilePath(path, cfg.Substrate.ClientCertFile)
}

// ResolveCLIPaths does not open referenced files: unrelated commands need no
// database credentials or install values files.
func ResolveCLIPaths(cfg *apiv1.CLIConfig, path string) {
	cfg.Database.UrlFile = FilePath(path, cfg.Database.UrlFile)
	cfg.Connection.CaFile = FilePath(path, cfg.Connection.CaFile)
	for _, chart := range []*apiv1.ChartConfig{cfg.Install.Kagent, cfg.Install.Substrate, cfg.Install.PodCertificate} {
		for i, value := range chart.ValuesFiles {
			chart.ValuesFiles[i] = FilePath(path, value)
		}
	}
}
