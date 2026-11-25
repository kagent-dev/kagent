package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"gopkg.in/yaml.v3"

	"github.com/kagent-dev/kagent/go/api/client"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/anthropicselfhostedsandboxworker"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
	"github.com/kagent-dev/kagent/go/core/pkg/postgres"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/kagent-dev/kagent/go/pkg/telemetry"
)

var schemaPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

type environmentConfig struct {
	EnvironmentSecretRef     string `yaml:"environmentSecretRef"`
	SandboxTemplateName      string `yaml:"sandboxTemplateName"`
	SandboxTemplateNamespace string `yaml:"sandboxTemplateNamespace"`
}

func main() {
	migrate := flag.Bool("migrate", false, "apply the adapter's PostgreSQL migrations and exit")
	config := flag.String("config", "/config/config.yaml", "environment-to-sandbox-template mapping file")
	credentials := flag.String("credentials", "/credentials/environments", "directory of mounted environment secrets")
	idle := flag.Duration("sandbox-idle", 5*time.Second, "suspend compute after this much inactivity")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx = logging.IntoContext(ctx, logger)
	if err := run(ctx, *migrate, *config, *credentials, *idle); err != nil {
		logger.ErrorContext(ctx, "anthropic sandbox worker", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, migrate bool, configFile, credentials string, idle time.Duration) error {
	schema := env.AnthropicSandboxDatabaseSchema.Get()
	if !schemaPattern.MatchString(schema) {
		return fmt.Errorf("%s must specify a dedicated PostgreSQL schema", env.AnthropicSandboxDatabaseSchema.Name())
	}
	dsn := env.AnthropicSandboxDatabaseURL.Get()
	if dsn == "" {
		return fmt.Errorf("%s is required", env.AnthropicSandboxDatabaseURL.Name())
	}
	db, err := postgres.PoolConfig(dsn, env.AnthropicSandboxDatabaseRole.Get())
	if err != nil {
		return err
	}
	source := anthropicselfhostedsandboxworker.MigrationSource(schema)
	if migrate {
		return migrations.RunUp(ctx, db, []migrations.Source{source})
	}
	db.ConnConfig.RuntimeParams["search_path"] = pgx.Identifier{schema}.Sanitize()
	pool, err := pgxpool.NewWithConfig(ctx, db)
	if err != nil {
		return err
	}
	defer pool.Close()
	key, err := base64.StdEncoding.DecodeString(env.AnthropicSandboxEncryptionKey.Get())
	if err != nil {
		return fmt.Errorf("invalid base64 %s", env.AnthropicSandboxEncryptionKey.Name())
	}
	environments, err := loadEnvironments(configFile, credentials, env.AnthropicSandboxNamespace.Get())
	if err != nil {
		return err
	}
	kagent, err := client.NewAPI(env.AnthropicSandboxKagentURL.Get(), client.WithGRPCTimeout(0))
	if err != nil {
		return err
	}
	defer func() {
		if err := kagent.Close(); err != nil {
			logging.FromContext(ctx).WarnContext(ctx, "close Kagent client", "error", err)
		}
	}()
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(startup); err != nil {
		return errors.New("could not connect to adapter PostgreSQL")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 15 * time.Second
	defer transport.CloseIdleConnections()
	provider := anthropic.NewClient(option.WithoutEnvironmentDefaults(),
		option.WithHTTPClient(&http.Client{Transport: transport}), option.WithMaxRetries(0))
	const serviceName = "kagent-anthropic-sandbox-worker"
	logger := logging.FromContext(ctx)
	providers, err := telemetry.Init(ctx, telemetry.Options{Defaults: []attribute.KeyValue{
		semconv.ServiceName(serviceName), semconv.ServiceNamespace("kagent"), semconv.ServiceInstanceID(uuid.NewString()),
	}})
	if err != nil {
		logger.ErrorContext(ctx, "failed to initialize telemetry", "error", err)
	}
	defer func() {
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := providers.Shutdown(shutdown); err != nil {
			logger.ErrorContext(shutdown, "failed to shut down telemetry", "error", err)
		}
	}()
	runner, err := anthropicselfhostedsandboxworker.New(pool, provider, kagent.Sandbox, environments, key, idle,
		otel.Meter(serviceName))
	if err != nil {
		return err
	}
	runner.Run(ctx)
	return nil
}

func loadEnvironments(configFile, credentials, defaultNamespace string) ([]anthropicselfhostedsandboxworker.Environment, error) {
	config, err := os.Open(configFile)
	if err != nil {
		return nil, err
	}
	defer config.Close()
	var raw struct {
		Environments []environmentConfig `yaml:"environments"`
	}
	decoder := yaml.NewDecoder(config)
	decoder.KnownFields(true)
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}
	result := make([]anthropicselfhostedsandboxworker.Environment, 0, len(raw.Environments))
	for i, env := range raw.Environments {
		if env.EnvironmentSecretRef == "" {
			return nil, fmt.Errorf("environments[%d].environmentSecretRef is required", i)
		}
		template := templateReference(env.SandboxTemplateName, env.SandboxTemplateNamespace, defaultNamespace)
		if template == nil {
			return nil, fmt.Errorf("sandbox template name and namespace are required in environments[%d]", i)
		}
		dir := filepath.Join(credentials, strconv.Itoa(i))
		id, err := os.ReadFile(filepath.Join(dir, "environment-id"))
		if err != nil {
			return nil, err
		}
		key, err := os.ReadFile(filepath.Join(dir, "environment-key"))
		if err != nil {
			return nil, err
		}
		result = append(result, anthropicselfhostedsandboxworker.Environment{
			ID: strings.TrimSpace(string(id)), Key: strings.TrimSpace(string(key)), Template: template,
		})
	}
	return result, nil
}

func templateReference(name, namespace, defaultNamespace string) *apiv1.ResourceReference {
	if namespace == "" {
		namespace = defaultNamespace
	}
	if name == "" || namespace == "" {
		return nil
	}
	return &apiv1.ResourceReference{Namespace: namespace, Name: name}
}
