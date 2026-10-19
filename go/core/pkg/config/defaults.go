package config

import (
	"runtime"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	"github.com/kagent-dev/kagent/go/core/pkg/consts"
)

// DefaultCLI supplies defaults without consulting process environment.
func DefaultCLI(cfg *configv1.CLIConfig) {
	defaults(cfg, &configv1.CLIConfig{
		Connection: &configv1.ConnectionConfig{ApiUrl: new("http://localhost:8083"), GatewayUrl: new("http://localhost:8083"), Namespace: new("kagent"), UserId: new("admin@kagent.dev"), Timeout: durationpb.New(5 * time.Minute)},
		Output:     &configv1.OutputConfig{Format: new("table")},
		Logging:    &configv1.LoggingConfig{Level: new("info")},
		Install:    &configv1.InstallConfig{DefaultModelProvider: new("OpenAI"), BundledPostgresImage: new("postgres:18-alpine@sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15"), Kagent: &configv1.ChartConfig{Repository: new("oci://ghcr.io/kagent-dev/kagent/helm/"), Version: new(version.Version)}, Substrate: &configv1.ChartConfig{Repository: new("oci://ghcr.io/kagent-dev/substrate/helm/"), Version: new(version.SubstrateVersion)}},
		Database:   defaultDatabase(),
	})
	if cfg.Install.PodCertificate == nil {
		cfg.Install.PodCertificate = &configv1.ChartConfig{}
	}
	defaults(cfg.Install.PodCertificate, &configv1.ChartConfig{Repository: new(cfg.Install.Substrate.GetRepository()), Version: new(cfg.Install.Substrate.GetVersion())})
}

// DefaultController fills only absent settings. Secret file paths are required
// inputs, and remain empty until supplied by the deployment.
func DefaultController(cfg *configv1.ControllerConfig) {
	defaults(cfg, &configv1.ControllerConfig{
		Kubernetes:       &configv1.KubernetesConfig{Namespace: new("kagent"), LeaderElection: new(true)},
		Server:           &configv1.ControllerServerConfig{BindAddress: new(":8083"), Auth: &configv1.AuthenticationConfig{Mode: new("insecure")}},
		Database:         defaultDatabase(),
		Substrate:        &configv1.SubstrateConfig{ApiEndpoint: new("dns:///api.ate-system.svc:443"), RouterUrl: new("http://atenet-router.ate-system.svc:80")},
		Sessions:         &configv1.SessionsConfig{IdleTtl: durationpb.New(7 * 24 * time.Hour), ExpirationInterval: durationpb.New(time.Minute), QuiescenceRecoveryInterval: durationpb.New(time.Minute)},
		Gateway:          &configv1.GatewayConfig{PublicUrl: new("http://localhost:8083")},
		Push:             &configv1.PushConfig{RecoveryInterval: durationpb.New(time.Minute)},
		Sandboxes:        &configv1.SandboxConfig{Cpu: new("1"), Memory: new("1Gi"), DefaultTtl: durationpb.New(time.Hour), MaxTtl: durationpb.New(24 * time.Hour), ExpirationInterval: durationpb.New(time.Second)},
		ScheduledRuns:    &configv1.ScheduledRunsConfig{PollInterval: durationpb.New(time.Second), ExecutionInterval: durationpb.New(time.Second)},
		RuntimeRevisions: &configv1.RuntimeRevisionsConfig{GcInterval: durationpb.New(time.Minute)},
		Metrics:          &configv1.MetricsConfig{BindAddress: new("0")},
		Logging:          &configv1.LoggingConfig{Level: new("info")},
		Telemetry:        &configv1.TelemetryConfig{},
		RuntimeDefaults:  &configv1.RuntimeDefaultsConfig{Cpu: new("1"), Memory: new("1Gi"), ControlPlaneUrl: new("http://kagent-controller.kagent:8083"), GatewayUrl: new("http://kagent-controller.kagent:8083"), Logging: &configv1.LoggingConfig{Level: new("info")}, Telemetry: &configv1.TelemetryConfig{}},
	})
	if cfg.Push.Issuer == nil {
		cfg.Push.Issuer = new(cfg.Gateway.GetPublicUrl())
	}
	defaultTelemetry(cfg.Telemetry)
	defaultTelemetry(cfg.RuntimeDefaults.Telemetry)
}

func defaultDatabase() *configv1.DatabaseConfig {
	return &configv1.DatabaseConfig{Schema: new(consts.DefaultPostgresTableSchema), VectorSchema: new(consts.DefaultPgvectorSchema), Pool: &configv1.PostgresPoolConfig{MaxConns: new(int32(max(4, runtime.NumCPU()))), MaxConnIdleTime: durationpb.New(30 * time.Minute), MaxConnLifetime: durationpb.New(time.Hour)}}
}

func defaultTelemetry(cfg *configv1.TelemetryConfig) {
	if cfg.MaxCaptureBytes == nil {
		cfg.MaxCaptureBytes = new(int32(16384))
	}
	for _, signal := range []*configv1.OTLPConfig{cfg.Traces, cfg.Metrics, cfg.Logs} {
		if signal == nil {
			continue
		}
		defaults(signal, &configv1.OTLPConfig{Protocol: new("grpc"), Timeout: durationpb.New(10 * time.Second)})
	}
}

// ResolveControllerPaths makes each local file reference relative to its config.
func ResolveControllerPaths(cfg *configv1.ControllerConfig, path string) {
	resolveLoginPath(cfg.Database.Owner, path)
	resolveLoginPath(cfg.Database.ReadWrite, path)
	cfg.Push.SigningPrivateKeyFile = FilePath(path, cfg.Push.SigningPrivateKeyFile)
	cfg.Substrate.CaFile = FilePath(path, cfg.Substrate.CaFile)
	cfg.Substrate.ClientCertFile = FilePath(path, cfg.Substrate.ClientCertFile)
}

// ResolveCLIPaths does not open referenced files: unrelated commands need no
// database credentials or install values files.
func ResolveCLIPaths(cfg *configv1.CLIConfig, path string) {
	resolveLoginPath(cfg.Database.Owner, path)
	cfg.Connection.CaFile = FilePath(path, cfg.Connection.CaFile)
	for _, chart := range []*configv1.ChartConfig{cfg.Install.Kagent, cfg.Install.Substrate, cfg.Install.PodCertificate} {
		for i, value := range chart.ValuesFiles {
			chart.ValuesFiles[i] = FilePath(path, value)
		}
	}
}

func resolveLoginPath(login *configv1.DatabaseLoginConfig, path string) {
	if login != nil {
		login.UrlFile = FilePath(path, login.UrlFile)
	}
}

// defaults applies application defaults using protobuf field presence. Optional
// scalars retain explicit zero values; ordinary scalars use their protobuf zero.
// Duration messages are whole values, so an explicit 0s is not filled recursively.
func defaults(dst, src proto.Message) {
	fillDefaults(dst.ProtoReflect(), src.ProtoReflect())
}

func fillDefaults(dst, src protoreflect.Message) {
	src.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if !dst.Has(field) {
			if field.Kind() == protoreflect.MessageKind && !field.IsMap() && !field.IsList() {
				dst.Set(field, protoreflect.ValueOfMessage(proto.Clone(value.Message().Interface()).ProtoReflect()))
			} else {
				dst.Set(field, value)
			}
		} else if field.Kind() == protoreflect.MessageKind && !field.IsMap() && !field.IsList() {
			// Well-known values (especially Duration{seconds:0}) are atomic.
			if !strings.HasPrefix(string(field.Message().FullName()), "google.protobuf.") {
				fillDefaults(dst.Get(field).Message(), value.Message())
			}
		}
		return true
	})
}
