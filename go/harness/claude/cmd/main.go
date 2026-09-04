// Command kagent-claude runs the Claude Harness runtime adapter.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go/adk/pkg/app"
	"github.com/kagent-dev/kagent/go/harness/claude/internal/adapter"
	runtimea2a "github.com/kagent-dev/kagent/go/harness/runtime/a2a"
	"github.com/kagent-dev/kagent/go/harness/runtime/continuation"
	"github.com/kagent-dev/kagent/go/internal/logging"
)

const (
	configEnv    = "KAGENT_CONFIG_JSON"
	agentCardEnv = "KAGENT_AGENT_CARD_JSON"
	dataDir      = "/data"
	privatePort  = "80"
)

func main() {
	check := flag.Bool("check", false, "validate configuration and Claude version, then exit")
	flag.Parse()
	logger, err := logging.NewFromEnv(os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx := logging.IntoContext(context.Background(), logger)
	if err := run(ctx, *check, os.Getenv, os.Environ()); err != nil {
		logger.ErrorContext(ctx, "claude harness stopped", "error", err)
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
		ConfigJSON: configJSON,
		Workspace:  dataDir + "/workspace", DurableDir: dataDir,
		EphemeralDir: "/tmp/kagent-claude",
		Environment:  environment,
	})
	if err != nil {
		return fmt.Errorf("configure Claude Harness: %w", err)
	}
	validateCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := runner.Validate(validateCtx); err != nil {
		return err
	}
	if check {
		return nil
	}
	store, err := continuation.New(dataDir+"/adapter", "claude", validateSessionID)
	if err != nil {
		return err
	}
	executor, err := runtimea2a.New(runner, store)
	if err != nil {
		return err
	}
	application, err := app.New(app.AppConfig{AgentCard: card, Port: privatePort, AppName: card.Name, Logger: logging.FromContext(ctx)}, executor)
	if err != nil {
		return fmt.Errorf("construct private A2A app: %w", err)
	}
	return application.Run()
}

func validateSessionID(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return fmt.Errorf("invalid Claude session ID: %w", err)
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
