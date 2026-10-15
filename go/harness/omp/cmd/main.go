// Command kagent-omp runs oh-my-pi (omp) as a BYO Harness: it serves Kagent's
// private A2A contract and runs each turn as `omp --mode rpc` on the Actor's
// omp session.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/adk/pkg/app"
	"github.com/kagent-dev/kagent/go/harness/omp/internal/adapter"
	runtimea2a "github.com/kagent-dev/kagent/go/harness/runtime/a2a"
	"github.com/kagent-dev/kagent/go/harness/runtime/continuation"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/kagent-dev/kagent/go/pkg/telemetry"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"golang.org/x/sys/unix"
)

const (
	configEnv    = "KAGENT_CONFIG_JSON"
	agentCardEnv = "KAGENT_AGENT_CARD_JSON"
	dataDir      = "/data"
	privatePort  = "80"
	executable   = "omp"
)

// sessionIDPattern admits omp's session IDs and nothing --resume would read
// as a path.
var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,128}$`)

func main() {
	check := flag.Bool("check", false, "validate configuration and omp version, then exit")
	flag.Parse()
	logger, err := logging.NewFromEnv(os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx := logging.IntoContext(context.Background(), logger)
	// omp and the commands it runs share this process's user. Not dumpable,
	// its /proc entries (the environment with every placeholder and the
	// compiled configuration, its memory) are root's, so they cannot read them.
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		logger.ErrorContext(ctx, "omp harness stopped", "error", fmt.Errorf("make the harness process non-dumpable: %w", err))
		os.Exit(1)
	}
	if err := run(ctx, *check, os.Getenv, os.Environ()); err != nil { //nolint:forbidigo // Reads only controller-generated config and agent-card payloads.
		logger.ErrorContext(ctx, "omp harness stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, check bool, getenv func(string) string, environment []string) error {
	configJSON, err := requiredEnvironment(getenv, configEnv)
	if err != nil {
		return err
	}
	agentCardJSON, err := requiredEnvironment(getenv, agentCardEnv)
	if err != nil {
		return err
	}
	var card a2atype.AgentCard
	if err := json.Unmarshal(agentCardJSON, &card); err != nil {
		return fmt.Errorf("decode agent card: %w", err)
	}
	if strings.TrimSpace(card.Name) == "" {
		return fmt.Errorf("agent card name is required")
	}
	runner, err := adapter.New(ctx, adapter.Input{
		ConfigJSON: configJSON, Executable: executable,
		Workspace: dataDir + "/workspace", DurableDir: dataDir, Environment: environment,
	})
	if err != nil {
		return fmt.Errorf("configure omp Harness: %w", err)
	}
	validateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := runner.Validate(validateCtx); err != nil {
		return err
	}
	if check {
		return nil
	}
	store, err := continuation.New(dataDir+"/adapter", "omp", validateSessionID)
	if err != nil {
		return err
	}
	// A BYO image carries no compiled telemetry identity, so its spans are
	// attributed by the standard OpenTelemetry environment alone.
	var identity tracing.RuntimeTelemetry
	executor, err := runtimea2a.New(runner, store, identity)
	if err != nil {
		return err
	}
	providers, err := telemetry.Init(ctx, telemetry.Options{})
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to initialize harness telemetry", "error", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := providers.Shutdown(shutdownCtx); err != nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to shutdown harness telemetry", "error", err)
		}
	}()
	application, err := app.New(app.AppConfig{
		AgentCard: card, Port: privatePort, AppName: card.Name,
		Logger: logging.FromContext(ctx), Telemetry: identity, Flush: providers.ForceFlush,
	}, executor)
	if err != nil {
		return fmt.Errorf("construct private A2A app: %w", err)
	}
	return application.Run()
}

func validateSessionID(id string) error {
	if !sessionIDPattern.MatchString(id) {
		return fmt.Errorf("invalid omp session ID")
	}
	return nil
}

func requiredEnvironment(getenv func(string) string, name string) ([]byte, error) {
	value := strings.TrimSpace(getenv(name))
	if value == "" {
		return nil, fmt.Errorf("%s is required", name)
	}
	return []byte(value), nil
}
